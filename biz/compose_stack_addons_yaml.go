package biz

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// swirlManagedMarker is the trailing line-comment stamped on every label the
// addon wizard writes. On re-save we overwrite keys carrying the marker and
// preserve keys without it (user-managed). Anchored to a stable prefix so a
// later revision of the marker text stays detectable.
const swirlManagedMarker = "swirl-managed"

// AddonsConfig carries the wizard state for every enabled addon, keyed by
// service name. The tab UI emits exactly the shape persisted here; the
// backend turns it into labels + YAML mutations.
type AddonsConfig struct {
	Traefik    map[string]TraefikServiceCfg    `json:"traefik,omitempty"`
	Sablier    map[string]SablierServiceCfg    `json:"sablier,omitempty"`
	Watchtower map[string]WatchtowerServiceCfg `json:"watchtower,omitempty"`
	Backup     map[string]BackupServiceCfg     `json:"backup,omitempty"`
	Resources  map[string]ResourcesServiceCfg  `json:"resources,omitempty"`
}

// TraefikServiceCfg holds the wizard state for a single compose service.
// Empty / zero-valued fields mean "don't emit that label". Required fields
// (Domain/Path/Port) are validated at injection time — a toggled-off
// service emits no labels at all.
type TraefikServiceCfg struct {
	Enabled      bool     `json:"enabled"`
	Router       string   `json:"router"`
	RuleType     string   `json:"ruleType"` // "Host" | "PathPrefix" | "Host+PathPrefix"
	Domain       string   `json:"domain"`
	Path         string   `json:"path"`
	Entrypoint   string   `json:"entrypoint"`
	Port         int      `json:"port"`
	TLS          bool     `json:"tls"`
	CertResolver string   `json:"certResolver"`
	Middlewares  []string `json:"middlewares,omitempty"`
}

// Placeholders for later phases — declared here so the backend + frontend
// types stay aligned from Phase 3 onwards.
type SablierServiceCfg struct {
	Enabled         bool   `json:"enabled"`
	Group           string `json:"group"`
	SessionDuration string `json:"sessionDuration"`
	Strategy        string `json:"strategy"`
	DisplayName     string `json:"displayName"`
	Theme           string `json:"theme"`
}

type WatchtowerServiceCfg struct {
	Enabled     bool `json:"enabled"`
	MonitorOnly bool `json:"monitorOnly"`
}

type BackupServiceCfg struct {
	Enabled bool `json:"enabled"`
	// Coarse placeholders for Phase 5. Kept as strings/maps so the JSON
	// stays stable even as we flesh out the backup-tab form.
	Schedule string            `json:"schedule,omitempty"`
	Plugin   string            `json:"plugin,omitempty"`
	Extra    map[string]string `json:"extra,omitempty"`
}

type ResourcesServiceCfg struct {
	CPUsLimit         string `json:"cpusLimit,omitempty"`
	CPUsReservation   string `json:"cpusReservation,omitempty"`
	MemoryLimit       string `json:"memoryLimit,omitempty"`
	MemoryReservation string `json:"memoryReservation,omitempty"`
}

// injectAddonLabels rewrites the compose YAML in-place, upserting every
// wizard-managed label across the services listed in cfg. The mode decides
// WHERE the labels land:
//
//	standalone → services.<svc>.labels
//	swarm      → services.<svc>.deploy.labels
//
// Labels carrying swirlManagedMarker are overwritten; user-managed labels
// (same key, no marker) are preserved as-is. Returns the re-serialised YAML.
// A nil cfg (or one with only empty addon maps) is a no-op — the function
// returns content verbatim.
func injectAddonLabels(content string, cfg *AddonsConfig, mode string) (string, error) {
	if cfg == nil || !hasAnyAddon(cfg) {
		return content, nil
	}

	var root yaml.Node
	if err := yaml.Unmarshal([]byte(content), &root); err != nil {
		return content, fmt.Errorf("addon labels: parse YAML: %w", err)
	}
	doc := documentNode(&root)
	if doc == nil {
		return content, errors.New("addon labels: empty YAML document")
	}

	services := mappingFieldNode(doc, "services")
	if services == nil {
		// Nothing to label — return the content unchanged. The validator
		// upstream (Parse) already rejected malformed YAML.
		return content, nil
	}

	// Pre-compute labels per service so we only walk the service list once.
	labelsPerService := buildLabelsPerService(cfg)

	for svcName, labels := range labelsPerService {
		svcNode := mappingFieldNode(services, svcName)
		if svcNode == nil || svcNode.Kind != yaml.MappingNode {
			// Service not in the YAML (may have been renamed between
			// wizard edits and save). Skip silently — emitting an error
			// would break the save for an otherwise valid compose.
			continue
		}
		target := resolveLabelsNode(svcNode, mode)
		upsertLabels(target, labels)
	}

	// Apply resources — different path, same pre-serialize moment so the
	// whole doc round-trips once.
	if len(cfg.Resources) > 0 {
		applyResources(services, cfg.Resources, mode)
	}

	buf, err := marshalYAMLNode(&root)
	if err != nil {
		return content, fmt.Errorf("addon labels: serialize YAML: %w", err)
	}
	return buf, nil
}

// extractAddonConfig is the reverse parser: reads a YAML document, looks at
// every service's labels (both standalone and swarm-style placements) and
// reconstructs the AddonsConfig for the wizard tabs. Only labels carrying
// swirlManagedMarker are considered wizard-owned — everything else is left
// alone and the tab starts blank on those services.
//
// Phase 3 wires Traefik only. Other addons return empty maps and are
// filled in by later phases.
func extractAddonConfig(content string) (*AddonsConfig, error) {
	out := &AddonsConfig{
		Traefik:   map[string]TraefikServiceCfg{},
		Resources: map[string]ResourcesServiceCfg{},
	}
	if strings.TrimSpace(content) == "" {
		return out, nil
	}
	var root yaml.Node
	if err := yaml.Unmarshal([]byte(content), &root); err != nil {
		return out, err
	}
	doc := documentNode(&root)
	if doc == nil {
		return out, nil
	}
	services := mappingFieldNode(doc, "services")
	if services == nil || services.Kind != yaml.MappingNode {
		return out, nil
	}
	for i := 0; i < len(services.Content); i += 2 {
		keyNode := services.Content[i]
		svcNode := services.Content[i+1]
		if keyNode == nil || svcNode == nil || svcNode.Kind != yaml.MappingNode {
			continue
		}
		svc := keyNode.Value

		// Labels could live either under labels (standalone) or
		// deploy.labels (swarm). Scan both.
		managedLabels := map[string]string{}
		for _, target := range labelLocations(svcNode) {
			collectSwirlManagedLabels(target, managedLabels)
		}
		if cfg := traefikCfgFromLabels(managedLabels); cfg.Enabled {
			out.Traefik[svc] = cfg
		}

		// Resources: swarm and standalone have different paths.
		if rc, ok := resourcesCfgFromService(svcNode); ok {
			out.Resources[svc] = rc
		}
	}
	return out, nil
}

// ---------- helpers ----------

func hasAnyAddon(cfg *AddonsConfig) bool {
	return len(cfg.Traefik) > 0 ||
		len(cfg.Sablier) > 0 ||
		len(cfg.Watchtower) > 0 ||
		len(cfg.Backup) > 0 ||
		len(cfg.Resources) > 0
}

// documentNode peels off the top DocumentNode wrapper that yaml.v3 hands back
// from Unmarshal, returning the root mapping/sequence. Handles empty docs.
func documentNode(root *yaml.Node) *yaml.Node {
	if root == nil {
		return nil
	}
	if root.Kind == yaml.DocumentNode {
		if len(root.Content) == 0 {
			return nil
		}
		return root.Content[0]
	}
	return root
}

// mappingFieldNode finds a child by key in a MappingNode. Returns nil when
// the parent isn't a mapping or the key is absent.
func mappingFieldNode(parent *yaml.Node, key string) *yaml.Node {
	if parent == nil || parent.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i < len(parent.Content); i += 2 {
		k := parent.Content[i]
		if k != nil && k.Value == key {
			return parent.Content[i+1]
		}
	}
	return nil
}

// ensureMappingChild fetches-or-creates a MappingNode child under a mapping
// parent, returning it for further mutation. Used to materialise `deploy:`
// and `deploy.labels:` when absent.
func ensureMappingChild(parent *yaml.Node, key string) *yaml.Node {
	if parent == nil || parent.Kind != yaml.MappingNode {
		return nil
	}
	if existing := mappingFieldNode(parent, key); existing != nil {
		if existing.Kind == yaml.MappingNode {
			return existing
		}
		// Replace non-mapping scalar with an empty map — an uncommon
		// mid-edit state but safer than refusing to write.
		existing.Kind = yaml.MappingNode
		existing.Tag = "!!map"
		existing.Value = ""
		existing.Content = nil
		return existing
	}
	k := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
	v := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	parent.Content = append(parent.Content, k, v)
	return v
}

// resolveLabelsNode returns the labels MappingNode for a service, creating
// intermediate parents where needed. Swarm mode lands under deploy.labels;
// standalone under top-level labels.
func resolveLabelsNode(svcNode *yaml.Node, mode string) *yaml.Node {
	if mode == "swarm" {
		deploy := ensureMappingChild(svcNode, "deploy")
		return ensureMappingChild(deploy, "labels")
	}
	return ensureMappingChild(svcNode, "labels")
}

// labelLocations returns the set of existing label MappingNodes on a
// service — used by the reverse parser which has to look at both mode-
// specific placements regardless of which mode the host is in today.
func labelLocations(svcNode *yaml.Node) []*yaml.Node {
	var out []*yaml.Node
	if lbl := mappingFieldNode(svcNode, "labels"); lbl != nil && lbl.Kind == yaml.MappingNode {
		out = append(out, lbl)
	}
	if dep := mappingFieldNode(svcNode, "deploy"); dep != nil {
		if lbl := mappingFieldNode(dep, "labels"); lbl != nil && lbl.Kind == yaml.MappingNode {
			out = append(out, lbl)
		}
	}
	return out
}

// upsertLabels writes the key/value pairs under a labels MappingNode.
// Policy:
//   - key exists AND has swirl-managed marker → overwrite value.
//   - key exists AND no marker              → leave untouched (user-managed).
//   - key absent                            → insert with marker.
//
// Keys are written in deterministic (sorted) order when appended so the
// resulting YAML diff stays reviewable between saves.
func upsertLabels(labelsNode *yaml.Node, pairs map[string]string) {
	if labelsNode == nil || labelsNode.Kind != yaml.MappingNode {
		return
	}
	existing := map[string]int{} // key -> index of its value node
	for i := 0; i < len(labelsNode.Content); i += 2 {
		existing[labelsNode.Content[i].Value] = i
	}

	// Deterministic order for new keys keeps diffs reviewable.
	newKeys := make([]string, 0, len(pairs))
	for k := range pairs {
		newKeys = append(newKeys, k)
	}
	sort.Strings(newKeys)

	for _, k := range newKeys {
		v := pairs[k]
		if idx, ok := existing[k]; ok {
			valNode := labelsNode.Content[idx+1]
			if !strings.Contains(valNode.LineComment, swirlManagedMarker) {
				// user-managed — leave alone.
				continue
			}
			valNode.Value = v
			valNode.Tag = "!!str"
			valNode.Style = 0
			valNode.LineComment = swirlManagedMarker
			continue
		}
		keyNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: k}
		valNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v, LineComment: swirlManagedMarker}
		labelsNode.Content = append(labelsNode.Content, keyNode, valNode)
	}
}

// collectSwirlManagedLabels picks out only labels carrying the marker from a
// labels MappingNode — the reverse parser never touches user-managed entries.
func collectSwirlManagedLabels(labelsNode *yaml.Node, into map[string]string) {
	if labelsNode == nil || labelsNode.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i < len(labelsNode.Content); i += 2 {
		k := labelsNode.Content[i]
		v := labelsNode.Content[i+1]
		if v == nil || !strings.Contains(v.LineComment, swirlManagedMarker) {
			continue
		}
		into[k.Value] = v.Value
	}
}

func marshalYAMLNode(root *yaml.Node) (string, error) {
	var sb strings.Builder
	enc := yaml.NewEncoder(&sb)
	enc.SetIndent(2)
	if err := enc.Encode(root); err != nil {
		return "", err
	}
	_ = enc.Close()
	return sb.String(), nil
}

// ---------- Traefik-specific builder/reverse ----------

// buildLabelsPerService flattens cfg into service → label map, merging the
// output of every per-addon builder. When a service is listed in multiple
// addons (e.g. Traefik + Sablier) their label maps are merged — Sablier
// emits traefik.http.middlewares entries that must coexist with Traefik's.
func buildLabelsPerService(cfg *AddonsConfig) map[string]map[string]string {
	out := map[string]map[string]string{}
	for svc, t := range cfg.Traefik {
		for k, v := range buildTraefikLabels(svc, t) {
			if out[svc] == nil {
				out[svc] = map[string]string{}
			}
			out[svc][k] = v
		}
	}
	return out
}

// buildTraefikLabels emits the minimum label set that makes the service
// routable through a Traefik v3 instance. Returns an empty map when the
// wizard entry is disabled or missing required fields — nothing is written
// to the YAML in that case.
func buildTraefikLabels(svc string, cfg TraefikServiceCfg) map[string]string {
	if !cfg.Enabled {
		return nil
	}
	router := strings.TrimSpace(cfg.Router)
	if router == "" {
		router = svc
	}
	rule := buildTraefikRule(cfg)
	if rule == "" || cfg.Port <= 0 {
		// Incomplete form — skip the service. The UI enforces
		// required-field validation; this is a safety net for API
		// callers that bypass it.
		return nil
	}
	out := map[string]string{
		"traefik.enable": "true",
		fmt.Sprintf("traefik.http.routers.%s.rule", router):                                rule,
		fmt.Sprintf("traefik.http.services.%s.loadbalancer.server.port", router):            fmt.Sprintf("%d", cfg.Port),
	}
	if cfg.Entrypoint != "" {
		out[fmt.Sprintf("traefik.http.routers.%s.entrypoints", router)] = cfg.Entrypoint
	}
	if cfg.TLS {
		out[fmt.Sprintf("traefik.http.routers.%s.tls", router)] = "true"
		if cfg.CertResolver != "" {
			out[fmt.Sprintf("traefik.http.routers.%s.tls.certresolver", router)] = cfg.CertResolver
		}
	}
	if len(cfg.Middlewares) > 0 {
		out[fmt.Sprintf("traefik.http.routers.%s.middlewares", router)] = strings.Join(cfg.Middlewares, ",")
	}
	return out
}

// buildTraefikRule assembles the router.rule value out of the UI builder
// (Host / PathPrefix / both). Returns "" when the chosen combination has
// unfilled required fields.
func buildTraefikRule(cfg TraefikServiceCfg) string {
	domain := strings.TrimSpace(cfg.Domain)
	path := strings.TrimSpace(cfg.Path)
	switch strings.ToLower(cfg.RuleType) {
	case "host":
		if domain == "" {
			return ""
		}
		return fmt.Sprintf("Host(`%s`)", domain)
	case "pathprefix":
		if path == "" {
			return ""
		}
		return fmt.Sprintf("PathPrefix(`%s`)", path)
	case "host+pathprefix":
		if domain == "" || path == "" {
			return ""
		}
		return fmt.Sprintf("Host(`%s`) && PathPrefix(`%s`)", domain, path)
	default:
		// Empty/unknown — default to Host when a domain is given.
		if domain != "" {
			return fmt.Sprintf("Host(`%s`)", domain)
		}
		return ""
	}
}

// traefikCfgFromLabels is the reverse of buildTraefikLabels: walks a
// map of marker-tagged labels and rebuilds the wizard state. Router name
// is inferred from the first `traefik.http.routers.<router>.*` key
// encountered.
func traefikCfgFromLabels(labels map[string]string) TraefikServiceCfg {
	cfg := TraefikServiceCfg{}
	if labels["traefik.enable"] != "true" {
		return cfg
	}
	cfg.Enabled = true
	router := ""
	for k := range labels {
		if strings.HasPrefix(k, "traefik.http.routers.") {
			parts := strings.SplitN(k[len("traefik.http.routers."):], ".", 2)
			if len(parts) > 0 && parts[0] != "" {
				router = parts[0]
				break
			}
		}
	}
	cfg.Router = router
	if rule, ok := labels[fmt.Sprintf("traefik.http.routers.%s.rule", router)]; ok {
		cfg.RuleType, cfg.Domain, cfg.Path = parseTraefikRule(rule)
	}
	if ep, ok := labels[fmt.Sprintf("traefik.http.routers.%s.entrypoints", router)]; ok {
		cfg.Entrypoint = ep
	}
	if portStr, ok := labels[fmt.Sprintf("traefik.http.services.%s.loadbalancer.server.port", router)]; ok {
		fmt.Sscanf(portStr, "%d", &cfg.Port)
	}
	if tls, ok := labels[fmt.Sprintf("traefik.http.routers.%s.tls", router)]; ok && tls == "true" {
		cfg.TLS = true
	}
	if cr, ok := labels[fmt.Sprintf("traefik.http.routers.%s.tls.certresolver", router)]; ok {
		cfg.CertResolver = cr
	}
	if mws, ok := labels[fmt.Sprintf("traefik.http.routers.%s.middlewares", router)]; ok && mws != "" {
		cfg.Middlewares = strings.Split(mws, ",")
	}
	return cfg
}

// parseTraefikRule is a minimal reverse of the Host/PathPrefix builder.
// Anything it cannot recognise returns empty strings so the UI falls back
// to treating the service as "custom rule" (editor shows a warning).
var (
	hostRe        = regexp.MustCompile("^Host\\(`([^`]+)`\\)$")
	pathRe        = regexp.MustCompile("^PathPrefix\\(`([^`]+)`\\)$")
	hostAndPathRe = regexp.MustCompile("^Host\\(`([^`]+)`\\)\\s*&&\\s*PathPrefix\\(`([^`]+)`\\)$")
)

func parseTraefikRule(rule string) (kind, domain, path string) {
	rule = strings.TrimSpace(rule)
	if m := hostAndPathRe.FindStringSubmatch(rule); len(m) == 3 {
		return "Host+PathPrefix", m[1], m[2]
	}
	if m := hostRe.FindStringSubmatch(rule); len(m) == 2 {
		return "Host", m[1], ""
	}
	if m := pathRe.FindStringSubmatch(rule); len(m) == 2 {
		return "PathPrefix", "", m[1]
	}
	return "", "", ""
}

// ---------- resources (non-label path) ----------

// applyResources mutates services.<svc>.deploy.resources (swarm) or
// services.<svc>.{cpus,mem_limit,mem_reservation} (standalone) for every
// service listed in cfg. Keeps the label-injection semantics symmetrical
// even though resources are not labels.
func applyResources(services *yaml.Node, cfg map[string]ResourcesServiceCfg, mode string) {
	if services == nil || services.Kind != yaml.MappingNode {
		return
	}
	for svc, r := range cfg {
		svcNode := mappingFieldNode(services, svc)
		if svcNode == nil || svcNode.Kind != yaml.MappingNode {
			continue
		}
		if mode == "swarm" {
			applySwarmResources(svcNode, r)
		} else {
			applyStandaloneResources(svcNode, r)
		}
	}
}

func applySwarmResources(svcNode *yaml.Node, r ResourcesServiceCfg) {
	deploy := ensureMappingChild(svcNode, "deploy")
	resources := ensureMappingChild(deploy, "resources")
	if r.CPUsLimit != "" || r.MemoryLimit != "" {
		limits := ensureMappingChild(resources, "limits")
		setScalarField(limits, "cpus", r.CPUsLimit)
		setScalarField(limits, "memory", r.MemoryLimit)
	}
	if r.CPUsReservation != "" || r.MemoryReservation != "" {
		res := ensureMappingChild(resources, "reservations")
		setScalarField(res, "cpus", r.CPUsReservation)
		setScalarField(res, "memory", r.MemoryReservation)
	}
}

func applyStandaloneResources(svcNode *yaml.Node, r ResourcesServiceCfg) {
	setScalarField(svcNode, "cpus", r.CPUsLimit)
	setScalarField(svcNode, "mem_limit", r.MemoryLimit)
	setScalarField(svcNode, "mem_reservation", r.MemoryReservation)
	// cpu_shares maps to reservation on standalone Compose v2 spec; skip
	// unless the wizard eventually exposes it explicitly.
}

func setScalarField(parent *yaml.Node, key, value string) {
	if parent == nil || parent.Kind != yaml.MappingNode {
		return
	}
	if value == "" {
		return
	}
	if existing := mappingFieldNode(parent, key); existing != nil {
		existing.Kind = yaml.ScalarNode
		existing.Tag = "!!str"
		existing.Value = value
		existing.LineComment = swirlManagedMarker
		existing.Content = nil
		return
	}
	k := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
	v := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value, LineComment: swirlManagedMarker}
	parent.Content = append(parent.Content, k, v)
}

// resourcesCfgFromService walks the YAML to reconstruct the ResourcesServiceCfg
// from either deploy.resources (swarm) or top-level cpus/mem_limit keys
// (standalone). Only values stamped with the swirl-managed marker are
// included — operator-managed resource declarations are left out of the
// wizard state so the tab stays empty for them.
func resourcesCfgFromService(svcNode *yaml.Node) (ResourcesServiceCfg, bool) {
	out := ResourcesServiceCfg{}
	seen := false
	// Standalone: direct cpus/mem_limit at service level.
	if v := scalarIfManaged(svcNode, "cpus"); v != "" {
		out.CPUsLimit = v
		seen = true
	}
	if v := scalarIfManaged(svcNode, "mem_limit"); v != "" {
		out.MemoryLimit = v
		seen = true
	}
	if v := scalarIfManaged(svcNode, "mem_reservation"); v != "" {
		out.MemoryReservation = v
		seen = true
	}
	// Swarm: deploy.resources.{limits,reservations}.{cpus,memory}
	if deploy := mappingFieldNode(svcNode, "deploy"); deploy != nil {
		if resources := mappingFieldNode(deploy, "resources"); resources != nil {
			if limits := mappingFieldNode(resources, "limits"); limits != nil {
				if v := scalarIfManaged(limits, "cpus"); v != "" {
					out.CPUsLimit = v
					seen = true
				}
				if v := scalarIfManaged(limits, "memory"); v != "" {
					out.MemoryLimit = v
					seen = true
				}
			}
			if res := mappingFieldNode(resources, "reservations"); res != nil {
				if v := scalarIfManaged(res, "cpus"); v != "" {
					out.CPUsReservation = v
					seen = true
				}
				if v := scalarIfManaged(res, "memory"); v != "" {
					out.MemoryReservation = v
					seen = true
				}
			}
		}
	}
	return out, seen
}

// scalarIfManaged returns the scalar value of a child field only when the
// value node carries the swirl-managed marker. Used by the reverse parsers
// so operator-authored entries stay invisible to the wizard state.
func scalarIfManaged(parent *yaml.Node, key string) string {
	if parent == nil || parent.Kind != yaml.MappingNode {
		return ""
	}
	for i := 0; i < len(parent.Content); i += 2 {
		k := parent.Content[i]
		v := parent.Content[i+1]
		if k == nil || k.Value != key || v == nil {
			continue
		}
		if v.Kind != yaml.ScalarNode || !strings.Contains(v.LineComment, swirlManagedMarker) {
			return ""
		}
		return v.Value
	}
	return ""
}

