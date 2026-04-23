package biz

import (
	"fmt"
	"strings"

	"github.com/cuigh/swirl/dao"
	"github.com/cuigh/swirl/misc"
	"github.com/distribution/reference"
	"gopkg.in/yaml.v3"
)

// Registry Cache image rewriter.
//
// Runs at deploy time ONLY — never mutates the persisted YAML. The
// engine receives a rewritten copy of `content` where every
// `services.<svc>.image` ref that matches an enabled upstream mapping
// is replaced with `<mirror-host>:<port>/<prefix>/<repo>[:<tag>]`.
// The original ref survives as a leading comment
// (`# swirl-managed-registry-cache: original=<ref>`) so operators can
// audit what happened and a future Phase can reverse-parse it.
//
// Scope resolution (highest precedence first):
//
//  1. stack.DisableRegistryCache → no rewrite (opt-out per stack)
//  2. setting.RegistryCache.Enabled=false → no rewrite
//  3. setting.RegistryCache.RewriteMode:
//       "off"      → no rewrite
//       "per-host" → rewrite only when the host addon extract has
//                    RegistryCache.Enabled=true
//       "always"   → rewrite unconditionally (operators who pre-
//                    bootstrapped every host manually)
//  4. Digest-pinned refs (`image@sha256:…`) → preserved when
//     setting.RegistryCache.PreserveDigests=true.

// RewriteAction describes a single image rewrite performed by the
// rewriter. Callers surface these to the UI preview and to the audit
// trail; they are NOT persisted on the stack.
type RewriteAction struct {
	Service  string `json:"service"`
	Original string `json:"original"`
	Rewritten string `json:"rewritten"`
	Upstream string `json:"upstream"`
	Prefix   string `json:"prefix"`
	// Reason is populated when a service was evaluated but NOT
	// rewritten: "no-match", "digest-preserved", "invalid-ref". Empty
	// when the rewrite succeeded. Lets the UI render the full decision
	// table in the preview without extra round-trips.
	Reason string `json:"reason,omitempty"`
}

// RegistryCacheRewriteInput collapses the three inputs the rewriter
// needs into one struct so callers do not juggle parameters. Any nil
// field is treated as absent (no rewrite).
type RegistryCacheRewriteInput struct {
	Setting       *misc.Setting
	HostExtract   *RegistryCacheExtract
	StackDisabled bool
}

// WillRewrite reports whether the rewriter would perform any
// mutations given the input. Public variant of shouldRun, used by the
// preview API handler to surface a "this configuration will no-op"
// hint distinct from "no images matched".
func WillRewrite(in RegistryCacheRewriteInput) bool {
	run, requireHostOptIn := in.shouldRun()
	if !run {
		return false
	}
	if requireHostOptIn {
		if in.HostExtract == nil || !in.HostExtract.Enabled {
			return false
		}
	}
	return true
}

// rewriteDecision decides whether rewriting runs, given the input.
// Returns (enabled, requiresHostOptIn) where requiresHostOptIn=true
// means we will only rewrite when HostExtract.Enabled is also true.
func (in RegistryCacheRewriteInput) shouldRun() (bool, bool) {
	if in.StackDisabled {
		return false, false
	}
	if in.Setting == nil || !in.Setting.RegistryCache.Enabled {
		return false, false
	}
	if in.Setting.RegistryCache.Hostname == "" {
		return false, false
	}
	switch in.Setting.RegistryCache.RewriteMode {
	case "off":
		return false, false
	case "always":
		return true, false
	case "per-host", "":
		// Default = per-host when the mode is empty (backward compat
		// for legacy blobs written before this field existed).
		return true, true
	default:
		return false, false
	}
}

// RewriteImages parses `content` as a compose YAML document, rewrites
// every `services.<svc>.image` reference that matches an enabled
// upstream, and returns the mutated YAML plus the list of actions.
// Unknown / no-match images are left untouched. Parse errors fall
// back to returning the original content + nil actions so the engine
// can surface its own richer error message.
func RewriteImages(content string, in RegistryCacheRewriteInput) (string, []RewriteAction, error) {
	run, requireHostOptIn := in.shouldRun()
	if !run {
		return content, nil, nil
	}
	if requireHostOptIn {
		if in.HostExtract == nil || !in.HostExtract.Enabled {
			return content, nil, nil
		}
	}

	var root yaml.Node
	if err := yaml.Unmarshal([]byte(content), &root); err != nil {
		// Engine will re-parse and report a better error — do not
		// mask it here.
		return content, nil, nil
	}
	doc := documentNode(&root)
	if doc == nil {
		return content, nil, nil
	}
	services := mappingFieldNode(doc, "services")
	if services == nil || services.Kind != yaml.MappingNode {
		return content, nil, nil
	}

	rc := &in.Setting.RegistryCache
	mirror := fmt.Sprintf("%s:%d", rc.Hostname, portOrDefault(rc.Port))
	useUpstreamPrefix := rc.UseUpstreamPrefix
	preserveDigests := rc.PreserveDigests

	actions := make([]RewriteAction, 0, len(services.Content)/2)

	// services.Content is a flat [key1, value1, key2, value2, …]
	// list of mapping entries. Walk pairs.
	for i := 0; i+1 < len(services.Content); i += 2 {
		svcName := services.Content[i].Value
		svcNode := services.Content[i+1]
		if svcNode.Kind != yaml.MappingNode {
			continue
		}
		imgNode := scalarField(svcNode, "image")
		if imgNode == nil || imgNode.Kind != yaml.ScalarNode {
			continue
		}
		original := imgNode.Value
		if original == "" {
			continue
		}
		action := RewriteAction{Service: svcName, Original: original}

		// Digest pin pass-through.
		if preserveDigests && strings.Contains(original, "@sha256:") {
			action.Reason = "digest-preserved"
			actions = append(actions, action)
			continue
		}

		ref, err := reference.ParseNormalizedNamed(original)
		if err != nil {
			action.Reason = "invalid-ref"
			actions = append(actions, action)
			continue
		}
		domain := reference.Domain(ref)
		path := reference.Path(ref)

		// Re-emit based on UseUpstreamPrefix:
		//   true  → <mirror>/<domain>/<path>[:tag]
		//   false → <mirror>/<path>[:tag]
		var tagPart string
		if tagged, ok := ref.(reference.Tagged); ok {
			tagPart = ":" + tagged.Tag()
		} else {
			// Default tag when missing (reference.TagNameOnly yields
			// `:latest` for unqualified refs).
			tagPart = ":latest"
		}
		var rewritten string
		if useUpstreamPrefix {
			rewritten = fmt.Sprintf("%s/%s/%s%s", mirror, domain, path, tagPart)
		} else {
			rewritten = fmt.Sprintf("%s/%s%s", mirror, path, tagPart)
		}
		action.Rewritten = rewritten
		action.Upstream = domain
		// Prefix is kept in the action for audit continuity — equals
		// the domain when UseUpstreamPrefix is true, empty otherwise.
		if useUpstreamPrefix {
			action.Prefix = domain
		}
		actions = append(actions, action)

		imgNode.Value = rewritten
		// Mark the rewrite in a head comment so operators who dump the
		// effective YAML can see what happened without re-running the
		// rewriter. HeadComment attaches above the scalar; set only
		// when empty to avoid stacking markers on repeated deploys.
		markerLine := fmt.Sprintf("swirl-managed-registry-cache: original=%s", original)
		if imgNode.HeadComment == "" {
			imgNode.HeadComment = markerLine
		} else if !strings.Contains(imgNode.HeadComment, "swirl-managed-registry-cache:") {
			imgNode.HeadComment = imgNode.HeadComment + "\n" + markerLine
		}
	}

	// Nothing actually rewritten → return original to avoid spurious
	// whitespace churn from the YAML re-encoder.
	if !hasRewrite(actions) {
		return content, actions, nil
	}

	out, err := yaml.Marshal(&root)
	if err != nil {
		return content, actions, fmt.Errorf("registry cache rewrite: serialize YAML: %w", err)
	}
	return string(out), actions, nil
}

// BuildRewriteInput builds the rewriter input from the stack, its host
// and the live settings. Centralises the boilerplate so callers (Deploy
// hook + preview API) produce identical scope resolution.
func BuildRewriteInput(stack *dao.ComposeStack, hostExtract *AddonConfigExtract, setting *misc.Setting) RegistryCacheRewriteInput {
	in := RegistryCacheRewriteInput{
		Setting: setting,
	}
	if stack != nil {
		in.StackDisabled = stack.DisableRegistryCache
	}
	if hostExtract != nil {
		in.HostExtract = hostExtract.RegistryCache
	}
	return in
}

// scalarField walks a mapping node looking for a direct child with
// key `name`. Returns nil if absent or the parent is not a mapping.
// Helpers in compose_stack_addons_yaml.go offer a richer surface for
// mappings (mappingFieldNode) but here we need the RAW scalar node
// pointer so we can mutate its Value and HeadComment in place.
func scalarField(node *yaml.Node, name string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == name {
			return node.Content[i+1]
		}
	}
	return nil
}

func hasRewrite(actions []RewriteAction) bool {
	for _, a := range actions {
		if a.Rewritten != "" {
			return true
		}
	}
	return false
}
