package biz

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/cuigh/swirl/dao"
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

// TraefikServiceCfg is a flat label map for a compose service. The UI
// composes labels from a structured "section · name · key · value" form
// and ships the result back as a {key: value} map. The backend does NOT
// model routers / middlewares / services — it just owns the traefik.*
// namespace on wizard-touched services: purge, then rewrite with
// cfg.Labels (plus `traefik.enable` when cfg.Enabled).
type TraefikServiceCfg struct {
	// Enabled flips traefik.enable=true on the service. Decoupled from
	// the label map so the UI can keep the master toggle separate from
	// the detailed labels.
	Enabled bool `json:"enabled"`
	// Labels carries every traefik.* label (except traefik.enable which
	// is derived from Enabled). The wizard tab builds these from its
	// structured-row editor plus any raw passthrough the operator adds.
	// Unknown / provider-qualified / multi-router entries coexist
	// uniformly here — there's no privileged "structured" subset.
	Labels map[string]string `json:"labels,omitempty"`
}

// Sablier / Watchtower / Backup use the same flat shape as Traefik:
// `Enabled` drives the master switch label, `Labels` carries every
// other addon-prefixed entry verbatim. The backend doesn't model the
// individual keys — it owns the prefix namespace on touched services.
type SablierServiceCfg struct {
	Enabled bool              `json:"enabled"`
	Labels  map[string]string `json:"labels,omitempty"`
}

type WatchtowerServiceCfg struct {
	Enabled bool              `json:"enabled"`
	Labels  map[string]string `json:"labels,omitempty"`
}

type BackupServiceCfg struct {
	Enabled bool              `json:"enabled"`
	Labels  map[string]string `json:"labels,omitempty"`
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
// written.
//
// Sablier intentionally owns only `sablier.*` (native container labels
// the Sablier daemon reads via the docker provider). The Traefik
// plugin form (`traefik.http.middlewares.<name>.plugin.sablier.*`)
// lives under the Traefik namespace so the Traefik tab's passthrough
// keeps it round-tripping — purging `traefik.http.middlewares.*`
// would nuke every hand-written Traefik middleware on the service.
var addonPrefixes = map[string][]string{
	"traefik":    {"traefik."},
	"sablier":    {"sablier."},
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

	addString := func(svc string, m map[string]string) {
		if labelsPerService[svc] == nil {
			labelsPerService[svc] = map[string]string{}
		}
		for k, v := range m {
			labelsPerService[svc][k] = v
		}
	}
	for svc, t := range cfg.Traefik {
		purgePerService[svc] = append(purgePerService[svc], addonPrefixes["traefik"]...)
		addString(svc, buildTraefikLabels(svc, t))
	}
	for svc, s := range cfg.Sablier {
		purgePerService[svc] = append(purgePerService[svc], addonPrefixes["sablier"]...)
		addString(svc, buildSablierLabels(svc, s))
	}
	for svc, w := range cfg.Watchtower {
		purgePerService[svc] = append(purgePerService[svc], addonPrefixes["watchtower"]...)
		addString(svc, buildWatchtowerLabels(svc, w))
	}
	for svc, b := range cfg.Backup {
		purgePerService[svc] = append(purgePerService[svc], addonPrefixes["backup"]...)
		addString(svc, buildBackupLabels(svc, b))
	}

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

// detectActiveAddons returns the ordered list of addon tags currently
// emitted by a persisted stack. Cheap — reuses extractAddonConfig's
// reverse parse. The tags populate ComposeStackSummary.ActiveAddons
// so the list UI can render at-a-glance chips ("traefik", "backup",
// "resources", etc.). Order matches the wizard tab order.
//
// `registry-cache` surfaces when the stack is NOT opted out and its
// YAML either references the mirror hostname already or carries the
// rewriter marker comment — in practice: any stack that would be
// rewritten on the next deploy, which is what the operator wants to
// see in the list.
func detectActiveAddons(s *dao.ComposeStack) []string {
	if s == nil || strings.TrimSpace(s.Content) == "" {
		return nil
	}
	cfg, err := extractAddonConfig(s.Content)
	if err != nil || cfg == nil {
		return nil
	}
	var tags []string
	if len(cfg.Traefik) > 0 {
		tags = append(tags, "traefik")
	}
	if len(cfg.Sablier) > 0 {
		tags = append(tags, "sablier")
	}
	if len(cfg.Watchtower) > 0 {
		tags = append(tags, "watchtower")
	}
	if len(cfg.Backup) > 0 {
		tags = append(tags, "backup")
	}
	if len(cfg.Resources) > 0 {
		tags = append(tags, "resources")
	}
	// Registry Cache: opted-out stacks never surface the chip — they
	// deliberately bypass the rewriter. Otherwise include the chip
	// when the YAML shows evidence of a past rewrite (marker comment
	// on a service image) — cheap to detect at list-time without
	// touching the live Settings.RegistryCache.
	if !s.DisableRegistryCache && strings.Contains(s.Content, "swirl-managed-registry-cache") {
		tags = append(tags, "registry-cache")
	}
	return tags
}

// extractAddonConfig is the reverse parser: walks every service's labels and
// reconstructs the AddonsConfig for the wizard tabs. All labels carrying a
// recognised addon prefix are considered wizard-gestibili — the marker is
// gone, so user-authored entries and wizard-authored entries are treated
// identically. That matches the new save semantics: the wizard owns the
// entire addon namespace on a given service.
func extractAddonConfig(content string) (*AddonsConfig, error) {
	out := &AddonsConfig{
		Traefik:    map[string]TraefikServiceCfg{},
		Sablier:    map[string]SablierServiceCfg{},
		Watchtower: map[string]WatchtowerServiceCfg{},
		Backup:     map[string]BackupServiceCfg{},
		Resources:  map[string]ResourcesServiceCfg{},
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
		tcfg := traefikCfgFromLabels(labels)
		if tcfg.Enabled || len(tcfg.Labels) > 0 {
			out.Traefik[svc] = tcfg
		}
		scfg := sablierCfgFromLabels(labels)
		if scfg.Enabled || len(scfg.Labels) > 0 {
			out.Sablier[svc] = scfg
		}
		wcfg := watchtowerCfgFromLabels(labels)
		if wcfg.Enabled || len(wcfg.Labels) > 0 {
			out.Watchtower[svc] = wcfg
		}
		bcfg := backupCfgFromLabels(labels)
		if bcfg.Enabled || len(bcfg.Labels) > 0 {
			out.Backup[svc] = bcfg
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

// buildTraefikLabels flattens the wizard cfg into the final label set.
// The service "owns" its traefik.* namespace: wizard-disabled + no
// labels → nil (caller purges); otherwise emits traefik.enable (when
// Enabled) + every entry in cfg.Labels that carries a traefik. prefix.
// Any unknown/provider-qualified/multi-router entry flows through
// transparently because the backend doesn't model routers at all.
func buildTraefikLabels(svc string, cfg TraefikServiceCfg) map[string]string {
	_ = svc
	out := map[string]string{}
	for k, v := range cfg.Labels {
		if strings.HasPrefix(k, "traefik.") {
			out[k] = v
		}
	}
	if cfg.Enabled {
		out["traefik.enable"] = "true"
	}
	if !cfg.Enabled && len(out) == 0 {
		return nil
	}
	return out
}

// buildSablierLabels / buildWatchtowerLabels / buildBackupLabels mirror
// the Traefik builder: emit every label in cfg.Labels that carries the
// addon's prefix, plus the canonical "enable" label when cfg.Enabled.
// Pre-filtering on the prefix prevents a config that accidentally
// smuggled a cross-addon key from leaking across namespaces.

func buildSablierLabels(_ string, cfg SablierServiceCfg) map[string]string {
	out := filterPrefix(cfg.Labels, "sablier.")
	if cfg.Enabled {
		out["sablier.enable"] = "true"
	}
	if !cfg.Enabled && len(out) == 0 {
		return nil
	}
	return out
}

func buildWatchtowerLabels(_ string, cfg WatchtowerServiceCfg) map[string]string {
	out := filterPrefix(cfg.Labels, "com.centurylinklabs.watchtower.")
	if cfg.Enabled {
		out["com.centurylinklabs.watchtower.enable"] = "true"
	}
	if !cfg.Enabled && len(out) == 0 {
		return nil
	}
	return out
}

func buildBackupLabels(_ string, cfg BackupServiceCfg) map[string]string {
	out := filterPrefix(cfg.Labels, "backup.")
	if cfg.Enabled {
		out["backup.enable"] = "true"
	}
	if !cfg.Enabled && len(out) == 0 {
		return nil
	}
	return out
}

// filterPrefix returns a fresh map with only the entries whose key
// starts with `prefix`. Safe on nil input.
func filterPrefix(in map[string]string, prefix string) map[string]string {
	out := map[string]string{}
	for k, v := range in {
		if strings.HasPrefix(k, prefix) {
			out[k] = v
		}
	}
	return out
}

// traefikCfgFromLabels rebuilds the wizard cfg from every traefik.*
// label of a service. traefik.enable drives cfg.Enabled; everything
// else lands in cfg.Labels verbatim. Multi-router / tls.options /
// provider-qualified middlewares round-trip loss-free because there
// is no "structured subset" competing with the raw label set.
func traefikCfgFromLabels(labels map[string]string) TraefikServiceCfg {
	cfg := TraefikServiceCfg{Labels: map[string]string{}}
	for k, v := range labels {
		if !strings.HasPrefix(k, "traefik.") {
			continue
		}
		if k == "traefik.enable" && v == "true" {
			cfg.Enabled = true
			continue
		}
		cfg.Labels[k] = v
	}
	if len(cfg.Labels) == 0 {
		cfg.Labels = nil
	}
	return cfg
}

// traefikCfgFromLabelsFor is kept as an alias for call-site readability:
// earlier versions preferred a router matching the compose service name.
// The new flat model doesn't need the hint — the service name is ignored.
func traefikCfgFromLabelsFor(labels map[string]string, _ string) TraefikServiceCfg {
	return traefikCfgFromLabels(labels)
}

// sablierCfgFromLabels / watchtowerCfgFromLabels / backupCfgFromLabels
// mirror the Traefik reverse parser. `<addon>.enable=true` (canonical
// signal of "opt in on this service") drives cfg.Enabled; everything
// else lands in cfg.Labels verbatim.

func sablierCfgFromLabels(labels map[string]string) SablierServiceCfg {
	cfg := SablierServiceCfg{Labels: map[string]string{}}
	for k, v := range labels {
		if !strings.HasPrefix(k, "sablier.") {
			continue
		}
		if k == "sablier.enable" && v == "true" {
			cfg.Enabled = true
			continue
		}
		cfg.Labels[k] = v
	}
	if len(cfg.Labels) == 0 {
		cfg.Labels = nil
	}
	return cfg
}

func watchtowerCfgFromLabels(labels map[string]string) WatchtowerServiceCfg {
	const prefix = "com.centurylinklabs.watchtower."
	cfg := WatchtowerServiceCfg{Labels: map[string]string{}}
	for k, v := range labels {
		if !strings.HasPrefix(k, prefix) {
			continue
		}
		if k == prefix+"enable" && v == "true" {
			cfg.Enabled = true
			continue
		}
		cfg.Labels[k] = v
	}
	if len(cfg.Labels) == 0 {
		cfg.Labels = nil
	}
	return cfg
}

func backupCfgFromLabels(labels map[string]string) BackupServiceCfg {
	cfg := BackupServiceCfg{Labels: map[string]string{}}
	for k, v := range labels {
		if !strings.HasPrefix(k, "backup.") {
			continue
		}
		if k == "backup.enable" && v == "true" {
			cfg.Enabled = true
			continue
		}
		cfg.Labels[k] = v
	}
	if len(cfg.Labels) == 0 {
		cfg.Labels = nil
	}
	return cfg
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
