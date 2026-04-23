package biz

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// AddonsConfig carries the wizard state for every enabled addon, keyed by
// service name. The tab UI emits exactly the shape persisted here; the
// backend turns it into labels + YAML mutations.
//
// Merge model (post marker-abandonment): every addon owns its label
// namespace entirely. For each service present in cfg.<Addon> the backend
// REMOVES every label carrying the addon's prefix (e.g. `traefik.*`) and
// rewrites it with the labels computed from the form. Services not present
// in cfg.<Addon> are left untouched. Resources follow the same model at
// scalar level: a service listed in cfg.Resources has its resource fields
// overwritten; services not listed are ignored.
type AddonsConfig struct {
	Traefik    map[string]TraefikServiceCfg    `json:"traefik,omitempty"`
	Sablier    map[string]SablierServiceCfg    `json:"sablier,omitempty"`
	Watchtower map[string]WatchtowerServiceCfg `json:"watchtower,omitempty"`
	Backup     map[string]BackupServiceCfg     `json:"backup,omitempty"`
	Resources  map[string]ResourcesServiceCfg  `json:"resources,omitempty"`
}

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
	// ExtraLabels is the passthrough for every Traefik-prefixed label the
	// wizard doesn't model natively — e.g. extra routers for the same
	// service, tls.options, provider-qualified middlewares (`@file`,
	// `@kubernetes`), service-level loadBalancer tweaks, plugins, etc.
	// The reverse parser fills this with anything NOT mapped into the
	// structured fields above; the builder re-emits the map verbatim on
	// save, alongside the structured labels. This lets operators use the
	// wizard for common cases without losing hand-written advanced
	// configuration.
	ExtraLabels map[string]string `json:"extraLabels,omitempty"`
}

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
	Enabled  bool              `json:"enabled"`
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

// addonPrefixes lists the label key prefixes each addon claims. When a
// service appears in cfg.<Addon>, every existing label with one of these
// prefixes is stripped from the service before the new label set is
// written. Keeping the list in one place makes the replace-all policy
// auditable at a glance.
var addonPrefixes = map[string][]string{
	"traefik":    {"traefik."},
	"sablier":    {"sablier.", "traefik.http.middlewares."},
	"watchtower": {"com.centurylinklabs.watchtower."},
	"backup":     {"backup."},
}

// injectAddonLabels rewrites the compose YAML in-place, replacing every
// addon-owned label set across the services listed in cfg. The mode decides
// WHERE the labels land:
//
//	standalone → services.<svc>.labels
//	swarm      → services.<svc>.deploy.labels
//
// Labels carrying a prefix that belongs to a wizard-touched addon on a
// service are dropped and re-emitted from cfg; labels of addons NOT touched
// by the operator on that service are preserved as-is. Resources work the
// same way at scalar-field level.
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
		return content, nil
	}

	// Compute, per service, (a) which addon prefixes must be purged and
	// (b) the new label set to write. A service appears in the map if
	// AT LEAST ONE addon has an entry for it.
	purgePerService := map[string][]string{}
	labelsPerService := map[string]map[string]string{}

	for svc, t := range cfg.Traefik {
		purgePerService[svc] = append(purgePerService[svc], addonPrefixes["traefik"]...)
		for k, v := range buildTraefikLabels(svc, t) {
			if labelsPerService[svc] == nil {
				labelsPerService[svc] = map[string]string{}
			}
			labelsPerService[svc][k] = v
		}
	}
	// Future phases wire Sablier/Watchtower/Backup the same way — the
	// prefix-based purge makes their addition a one-liner.

	for svc, prefixes := range purgePerService {
		svcNode := mappingFieldNode(services, svc)
		if svcNode == nil || svcNode.Kind != yaml.MappingNode {
			continue
		}
		// Purge owned prefixes from every labels location (standalone +
		// swarm), then write to the mode-correct target. Cross-location
		// purge avoids orphan labels when a stack migrates between modes.
		for _, loc := range labelLocations(svcNode) {
			stripPrefixes(loc, prefixes)
		}
		target := resolveLabelsNode(svcNode, mode)
		if labels := labelsPerService[svc]; labels != nil {
			writeLabels(target, labels)
		}
	}

	// Resources — mode-specific path, not labels.
	if len(cfg.Resources) > 0 {
		applyResources(services, cfg.Resources, mode)
	}

	buf, err := marshalYAMLNode(&root)
	if err != nil {
		return content, fmt.Errorf("addon labels: serialize YAML: %w", err)
	}
	return buf, nil
}

// extractAddonConfig is the reverse parser: walks every service's labels and
// reconstructs the AddonsConfig for the wizard tabs. All labels carrying a
// recognised addon prefix are considered wizard-gestibili — the marker is
// gone, so user-authored entries and wizard-authored entries are treated
// identically. That matches the new save semantics: the wizard owns the
// entire addon namespace on a given service.
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

		labels := map[string]string{}
		for _, target := range labelLocations(svcNode) {
			collectLabels(target, labels)
		}
		cfg := traefikCfgFromLabelsFor(labels, svc)
		// Surface the cfg when ANY traefik.* label exists on the
		// service — the UI shows the wizard state for known keys and
		// a passthrough box for the rest. Services with no traefik
		// footprint stay out of the map.
		if cfg.Enabled || len(cfg.ExtraLabels) > 0 {
			out.Traefik[svc] = cfg
		}

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
// parent, returning it for further mutation.
func ensureMappingChild(parent *yaml.Node, key string) *yaml.Node {
	if parent == nil || parent.Kind != yaml.MappingNode {
		return nil
	}
	if existing := mappingFieldNode(parent, key); existing != nil {
		if existing.Kind == yaml.MappingNode {
			return existing
		}
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

// removeMappingChild removes a child by key from a mapping node. No-op when
// the parent isn't a mapping or the key is absent. Used to delete empty
// labels / deploy / resources containers after pruning.
func removeMappingChild(parent *yaml.Node, key string) {
	if parent == nil || parent.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i < len(parent.Content); i += 2 {
		k := parent.Content[i]
		if k != nil && k.Value == key {
			parent.Content = append(parent.Content[:i], parent.Content[i+2:]...)
			return
		}
	}
}

// resolveLabelsNode returns the labels node for a service (mapping OR
// sequence — see Docker Compose spec: both forms are valid), creating an
// empty mapping if the field is absent. Swarm mode lands under
// deploy.labels; standalone under top-level labels. Callers must treat
// the returned node as-is: sequence nodes keep their `- "k=v"` style,
// mapping nodes keep their `k: v` style.
func resolveLabelsNode(svcNode *yaml.Node, mode string) *yaml.Node {
	if mode == "swarm" {
		deploy := ensureMappingChild(svcNode, "deploy")
		return ensureLabelsChild(deploy)
	}
	return ensureLabelsChild(svcNode)
}

// ensureLabelsChild returns the `labels` child verbatim when it exists
// (preserving mapping vs sequence form), or creates an empty mapping when
// absent. Unlike ensureMappingChild, this NEVER converts a sequence to a
// mapping — that conversion would drop every hand-written label in the
// common `- "k=v"` form used by most compose files.
func ensureLabelsChild(parent *yaml.Node) *yaml.Node {
	if parent == nil || parent.Kind != yaml.MappingNode {
		return nil
	}
	if existing := mappingFieldNode(parent, "labels"); existing != nil {
		return existing
	}
	k := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "labels"}
	v := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	parent.Content = append(parent.Content, k, v)
	return v
}

// labelLocations returns the set of existing label nodes on a service —
// mapping OR sequence — across both mode-specific placements. Reverse
// parser and prefix purger walk every location.
func labelLocations(svcNode *yaml.Node) []*yaml.Node {
	var out []*yaml.Node
	if lbl := mappingFieldNode(svcNode, "labels"); isLabelsNode(lbl) {
		out = append(out, lbl)
	}
	if dep := mappingFieldNode(svcNode, "deploy"); dep != nil {
		if lbl := mappingFieldNode(dep, "labels"); isLabelsNode(lbl) {
			out = append(out, lbl)
		}
	}
	return out
}

// isLabelsNode classifies a YAML node as a valid labels container:
// either a mapping (`k: v` entries) or a sequence (`- "k=v"` entries).
func isLabelsNode(n *yaml.Node) bool {
	if n == nil {
		return false
	}
	return n.Kind == yaml.MappingNode || n.Kind == yaml.SequenceNode
}

// stripPrefixes drops every label whose key starts with any of the given
// prefixes, handling BOTH mapping-form and sequence-form nodes. Called
// before writeLabels so the wizard always emits a clean addon namespace.
func stripPrefixes(labelsNode *yaml.Node, prefixes []string) {
	if !isLabelsNode(labelsNode) {
		return
	}
	if labelsNode.Kind == yaml.MappingNode {
		kept := make([]*yaml.Node, 0, len(labelsNode.Content))
		for i := 0; i < len(labelsNode.Content); i += 2 {
			k := labelsNode.Content[i]
			if !hasAnyPrefix(k.Value, prefixes) {
				kept = append(kept, labelsNode.Content[i], labelsNode.Content[i+1])
			}
		}
		labelsNode.Content = kept
		return
	}
	// SequenceNode: each Content item is a scalar `"key=value"` or
	// sometimes `"key"` (flag-style). We match on the key half.
	kept := make([]*yaml.Node, 0, len(labelsNode.Content))
	for _, item := range labelsNode.Content {
		k, _ := splitSeqLabel(item)
		if !hasAnyPrefix(k, prefixes) {
			kept = append(kept, item)
		}
	}
	labelsNode.Content = kept
}

func hasAnyPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// splitSeqLabel pulls the "key" / "value" out of a single sequence-form
// label node. Accepts `"k=v"`, `"k"` (value defaults to ""), and quoted
// variants. Non-scalar items return ("", "").
func splitSeqLabel(item *yaml.Node) (string, string) {
	if item == nil || item.Kind != yaml.ScalarNode {
		return "", ""
	}
	s := item.Value
	if eq := strings.Index(s, "="); eq >= 0 {
		return s[:eq], s[eq+1:]
	}
	return s, ""
}

// writeLabels appends key/value pairs to a labels node in deterministic
// order, preserving the node's form (mapping vs sequence). Callers are
// expected to have purged the addon prefix already via stripPrefixes.
func writeLabels(labelsNode *yaml.Node, pairs map[string]string) {
	if !isLabelsNode(labelsNode) {
		return
	}
	keys := make([]string, 0, len(pairs))
	for k := range pairs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if labelsNode.Kind == yaml.MappingNode {
		for _, k := range keys {
			keyNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: k}
			valNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: pairs[k]}
			labelsNode.Content = append(labelsNode.Content, keyNode, valNode)
		}
		return
	}
	// SequenceNode: emit `- "key=value"`. The DoubleQuotedStyle gives
	// visual parity with the rules operators hand-write (which
	// typically quote the whole entry because values contain backticks,
	// dots, etc.).
	for _, k := range keys {
		entry := &yaml.Node{
			Kind:  yaml.ScalarNode,
			Tag:   "!!str",
			Value: k + "=" + pairs[k],
			Style: yaml.DoubleQuotedStyle,
		}
		labelsNode.Content = append(labelsNode.Content, entry)
	}
}

// collectLabels copies every key/value pair of a labels node (mapping OR
// sequence) into `into`. Used by the reverse parser — all entries are
// surfaced regardless of origin.
func collectLabels(labelsNode *yaml.Node, into map[string]string) {
	if !isLabelsNode(labelsNode) {
		return
	}
	if labelsNode.Kind == yaml.MappingNode {
		for i := 0; i < len(labelsNode.Content); i += 2 {
			k := labelsNode.Content[i]
			v := labelsNode.Content[i+1]
			if v == nil {
				continue
			}
			into[k.Value] = v.Value
		}
		return
	}
	for _, item := range labelsNode.Content {
		k, v := splitSeqLabel(item)
		if k != "" {
			into[k] = v
		}
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

// buildTraefikLabels emits the label set that makes the service routable
// through a Traefik v3 instance. The structured wizard fields produce the
// minimum-viable routing; ExtraLabels (passthrough) is merged on top so
// advanced operator-authored entries round-trip verbatim.
//
// Return value semantics:
//   - Wizard disabled AND no extras → nil, caller purges the namespace.
//   - Wizard disabled BUT extras present → only the extras (preserving
//     multi-router / tls.options / middleware@file on a service the
//     operator doesn't want fully wizard-managed).
//   - Wizard enabled but missing rule/port → extras only (the wizard
//     row is incomplete, we don't emit a half-broken router).
func buildTraefikLabels(svc string, cfg TraefikServiceCfg) map[string]string {
	// Start with passthrough. Structured fields below override on key
	// collision — if the operator typed `traefik.enable` in the extras
	// list, the wizard toggle still wins.
	out := map[string]string{}
	for k, v := range cfg.ExtraLabels {
		if strings.HasPrefix(k, "traefik.") {
			out[k] = v
		}
	}
	if !cfg.Enabled {
		if len(out) == 0 {
			return nil
		}
		return out
	}
	router := strings.TrimSpace(cfg.Router)
	if router == "" {
		router = svc
	}
	rule := buildTraefikRule(cfg)
	if rule == "" || cfg.Port <= 0 {
		if len(out) == 0 {
			return nil
		}
		return out
	}
	out["traefik.enable"] = "true"
	out[fmt.Sprintf("traefik.http.routers.%s.rule", router)] = rule
	out[fmt.Sprintf("traefik.http.services.%s.loadbalancer.server.port", router)] = fmt.Sprintf("%d", cfg.Port)
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
		if domain != "" {
			return fmt.Sprintf("Host(`%s`)", domain)
		}
		return ""
	}
}

// traefikCfgFromLabels rebuilds the wizard state from every traefik.*
// label. Recognised keys of a single "primary" router fill the
// structured fields; everything else — extra routers, tls.options,
// middlewares with provider qualifiers (`@file`, `@kubernetes`),
// service-level loadBalancer tweaks, plugins, etc. — lands verbatim in
// ExtraLabels so the next save preserves it.
//
// Router selection (preserveDeterministic):
//   - prefer `<svcName>` when a router with that name exists (matches
//     the Docker Compose convention);
//   - fallback to the lexicographically-first router name (comparing
//     just the NAME, not the full label key — avoids `-` < `.` ASCII
//     surprises that made keycloak-int beat keycloak);
//   - only actually CONSUME the router's labels into structured fields
//     if the router has BOTH `rule` AND a loadbalancer port. Otherwise
//     we leave everything in ExtraLabels and the builder re-emits it
//     verbatim — round-trip safe for hand-authored configs the wizard
//     can't fully reconstruct.
func traefikCfgFromLabels(labels map[string]string) TraefikServiceCfg {
	return traefikCfgFromLabelsFor(labels, "")
}

// traefikCfgFromLabelsFor is the service-aware variant used when we know
// the compose service name — lets us prefer `<svcName>` as the primary
// router when both exist.
func traefikCfgFromLabelsFor(labels map[string]string, svcName string) TraefikServiceCfg {
	cfg := TraefikServiceCfg{ExtraLabels: map[string]string{}}
	for k, v := range labels {
		if strings.HasPrefix(k, "traefik.") {
			cfg.ExtraLabels[k] = v
		}
	}
	enable, hasEnable := labels["traefik.enable"]
	if !hasEnable || enable != "true" {
		return cfg
	}
	cfg.Enabled = true
	delete(cfg.ExtraLabels, "traefik.enable")

	routers := collectRouterNames(labels)
	if len(routers) == 0 {
		if len(cfg.ExtraLabels) == 0 {
			cfg.ExtraLabels = nil
		}
		return cfg
	}
	// Prefer a router whose name matches the compose service.
	router := routers[0]
	if svcName != "" {
		for _, r := range routers {
			if r == svcName {
				router = r
				break
			}
		}
	}
	ruleKey := fmt.Sprintf("traefik.http.routers.%s.rule", router)
	portKey := fmt.Sprintf("traefik.http.services.%s.loadbalancer.server.port", router)
	rule, hasRule := labels[ruleKey]
	_, hasPort := labels[portKey]
	kind, domain, path := parseTraefikRule(rule)

	// Consume the router into structured fields only when we can rebuild
	// it end-to-end (needs rule + port). Otherwise leave everything in
	// ExtraLabels so the builder doesn't drop anything.
	if hasRule && hasPort && kind != "" {
		cfg.Router = router
		cfg.RuleType, cfg.Domain, cfg.Path = kind, domain, path
		delete(cfg.ExtraLabels, ruleKey)
		consume := func(key string, apply func(string)) {
			if v, ok := labels[key]; ok {
				apply(v)
				delete(cfg.ExtraLabels, key)
			}
		}
		consume(fmt.Sprintf("traefik.http.routers.%s.entrypoints", router), func(v string) { cfg.Entrypoint = v })
		consume(portKey, func(v string) { fmt.Sscanf(v, "%d", &cfg.Port) })
		consume(fmt.Sprintf("traefik.http.routers.%s.tls", router), func(v string) {
			if v == "true" {
				cfg.TLS = true
			}
		})
		consume(fmt.Sprintf("traefik.http.routers.%s.tls.certresolver", router), func(v string) { cfg.CertResolver = v })
		consume(fmt.Sprintf("traefik.http.routers.%s.middlewares", router), func(v string) {
			if v != "" {
				cfg.Middlewares = strings.Split(v, ",")
			}
		})
	}

	if len(cfg.ExtraLabels) == 0 {
		cfg.ExtraLabels = nil
	}
	return cfg
}

// collectRouterNames returns the set of router names present in the label
// map (`traefik.http.routers.<name>.*`), sorted alphabetically by NAME.
// Used for deterministic primary-router selection.
func collectRouterNames(labels map[string]string) []string {
	seen := map[string]struct{}{}
	for k := range labels {
		const prefix = "traefik.http.routers."
		if !strings.HasPrefix(k, prefix) {
			continue
		}
		rest := k[len(prefix):]
		if dot := strings.Index(rest, "."); dot > 0 {
			seen[rest[:dot]] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

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

// applyResources writes the wizard-captured resource fields under
// `services.<svc>.deploy.resources.{limits,reservations}` for every
// service listed in cfg. The layout is identical in both standalone
// and swarm mode — the parser (types.ForbiddenProperties) refuses
// the legacy top-level `mem_limit` / `cpus` / `cpu_shares` fields, so
// using `deploy.resources.*` is the only path that round-trips cleanly
// through Parse + Deploy.
//
// The standalone engine now maps these fields onto
// container.HostConfig.Resources (see createAndStart) so the wizard
// picks up runtime enforcement regardless of mode. `mode` is kept in
// the signature for forward-compatibility in case swarm-only fields
// (e.g. generic resources) get added later.
//
// Empty strings in the cfg delete the corresponding field — a service
// listed in cfg.Resources owns its resource fields for the duration
// of the save.
func applyResources(services *yaml.Node, cfg map[string]ResourcesServiceCfg, mode string) {
	_ = mode // reserved for future swarm-only extensions
	if services == nil || services.Kind != yaml.MappingNode {
		return
	}
	for svc, r := range cfg {
		svcNode := mappingFieldNode(services, svc)
		if svcNode == nil || svcNode.Kind != yaml.MappingNode {
			continue
		}
		// Always purge the forbidden legacy top-level fields —
		// legacy stacks written before the wizard unification may
		// still carry them, and leaving them alone would trip the
		// parser on the next deploy.
		removeMappingChild(svcNode, "cpus")
		removeMappingChild(svcNode, "mem_limit")
		removeMappingChild(svcNode, "mem_reservation")
		removeMappingChild(svcNode, "cpu_shares")
		removeMappingChild(svcNode, "cpu_quota")
		removeMappingChild(svcNode, "cpuset")
		removeMappingChild(svcNode, "memswap_limit")
		applyDeployResources(svcNode, r)
	}
}

func applyDeployResources(svcNode *yaml.Node, r ResourcesServiceCfg) {
	if r.CPUsLimit == "" && r.MemoryLimit == "" && r.CPUsReservation == "" && r.MemoryReservation == "" {
		// All-empty: purge the whole resources block so the wizard
		// tab can clear a service back to "no limit".
		if deploy := mappingFieldNode(svcNode, "deploy"); deploy != nil {
			removeMappingChild(deploy, "resources")
			if len(deploy.Content) == 0 {
				removeMappingChild(svcNode, "deploy")
			}
		}
		return
	}
	deploy := ensureMappingChild(svcNode, "deploy")
	resources := ensureMappingChild(deploy, "resources")
	if r.CPUsLimit != "" || r.MemoryLimit != "" {
		limits := ensureMappingChild(resources, "limits")
		setOrRemoveScalar(limits, "cpus", r.CPUsLimit)
		setOrRemoveScalar(limits, "memory", r.MemoryLimit)
	} else {
		removeMappingChild(resources, "limits")
	}
	if r.CPUsReservation != "" || r.MemoryReservation != "" {
		res := ensureMappingChild(resources, "reservations")
		setOrRemoveScalar(res, "cpus", r.CPUsReservation)
		setOrRemoveScalar(res, "memory", r.MemoryReservation)
	} else {
		removeMappingChild(resources, "reservations")
	}
}

// setOrRemoveScalar writes `value` at `key` under `parent`, or removes the
// key entirely when value is empty. No marker tracking: the wizard owns
// the namespace once it's in the form.
func setOrRemoveScalar(parent *yaml.Node, key, value string) {
	if parent == nil || parent.Kind != yaml.MappingNode {
		return
	}
	if value == "" {
		removeMappingChild(parent, key)
		return
	}
	if existing := mappingFieldNode(parent, key); existing != nil {
		existing.Kind = yaml.ScalarNode
		existing.Tag = "!!str"
		existing.Value = value
		existing.LineComment = ""
		existing.Content = nil
		return
	}
	k := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
	v := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
	parent.Content = append(parent.Content, k, v)
}

// resourcesCfgFromService reads resource fields from
// `services.<svc>.deploy.resources.*` into a ResourcesServiceCfg.
// Legacy top-level fields (`cpus`, `mem_limit`, `mem_reservation`) are
// read ONLY as fallback — the parser refuses to load them at deploy
// time, but a legacy authored YAML may still carry them; surface them
// to the wizard so the operator sees + migrates the state rather than
// silently losing values. `deploy.resources.*` takes precedence when
// both paths are present.
func resourcesCfgFromService(svcNode *yaml.Node) (ResourcesServiceCfg, bool) {
	out := ResourcesServiceCfg{}
	seen := false
	// Legacy fallback first — overridden below by deploy.resources.*
	// if also present.
	if v := scalarValue(svcNode, "cpus"); v != "" {
		out.CPUsLimit = v
		seen = true
	}
	if v := scalarValue(svcNode, "mem_limit"); v != "" {
		out.MemoryLimit = v
		seen = true
	}
	if v := scalarValue(svcNode, "mem_reservation"); v != "" {
		out.MemoryReservation = v
		seen = true
	}
	if deploy := mappingFieldNode(svcNode, "deploy"); deploy != nil {
		if resources := mappingFieldNode(deploy, "resources"); resources != nil {
			if limits := mappingFieldNode(resources, "limits"); limits != nil {
				if v := scalarValue(limits, "cpus"); v != "" {
					out.CPUsLimit = v
					seen = true
				}
				if v := scalarValue(limits, "memory"); v != "" {
					out.MemoryLimit = v
					seen = true
				}
			}
			if res := mappingFieldNode(resources, "reservations"); res != nil {
				if v := scalarValue(res, "cpus"); v != "" {
					out.CPUsReservation = v
					seen = true
				}
				if v := scalarValue(res, "memory"); v != "" {
					out.MemoryReservation = v
					seen = true
				}
			}
		}
	}
	return out, seen
}

// scalarValue returns the string form of a mapping's scalar child, or ""
// when the child is absent or not a scalar.
func scalarValue(parent *yaml.Node, key string) string {
	if parent == nil || parent.Kind != yaml.MappingNode {
		return ""
	}
	for i := 0; i < len(parent.Content); i += 2 {
		k := parent.Content[i]
		v := parent.Content[i+1]
		if k == nil || k.Value != key || v == nil {
			continue
		}
		if v.Kind != yaml.ScalarNode {
			return ""
		}
		return v.Value
	}
	return ""
}
