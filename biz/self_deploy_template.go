package biz

import (
	"bytes"
	_ "embed"
	"fmt"
	"strconv"
	"strings"
	"text/template"

	"github.com/cuigh/swirl/misc"
)

// seedSelfDeployTemplate is the built-in compose template rendered when
// the operator has not saved a custom one. Using //go:embed with a
// package-local path keeps the release binary self-contained.
//
// Note on directory choice: Go's //go:embed cannot traverse parent
// directories, so the seed file lives inside the biz package at
// biz/templates/. This is a deviation from the planning document
// (which placed it at the repo root) — the operator-facing example
// shipped in Fase 7 (`compose.self-stack.yml.example`) will be the
// copy visible to humans; the embedded copy is the one the renderer
// actually consumes.
//
//go:embed templates/self-stack.yml
var seedSelfDeployTemplate string

// SelfDeployPlaceholders is re-exported from the misc package so existing
// biz-level callers keep working without an import-site rewrite. The
// authoritative definition lives in misc (see misc/self_deploy_defaults.go)
// so that misc.Setting can embed it without forming an import cycle
// (misc → biz → misc). The JSON tags and field semantics are documented
// on the source type; refer to misc.SelfDeployPlaceholders.
type SelfDeployPlaceholders = misc.SelfDeployPlaceholders

// DefaultPlaceholders returns a SelfDeployPlaceholders populated with
// safe defaults. Used by Preview when the operator hasn't saved a
// configuration yet, and by LoadConfig/SaveConfig as the fallback
// when a field is empty in the persisted record.
func DefaultPlaceholders() SelfDeployPlaceholders {
	return SelfDeployPlaceholders{
		ImageTag:      misc.SelfDeployImageTag,
		ExposePort:    misc.SelfDeployExposePort,
		RecoveryPort:  misc.SelfDeployRecoveryPort,
		RecoveryAllow: []string{misc.SelfDeployDefaultRecoveryCIDR},
		TraefikLabels: nil,
		VolumeData:    misc.SelfDeployVolumeData,
		NetworkName:   misc.SelfDeployNetworkName,
		ContainerName: misc.SelfDeployContainerName,
		ExtraEnv:      nil,
	}
}

// mergeWithDefaults fills every zero-valued field of p with the
// corresponding default. Non-zero fields are preserved verbatim.
// Not exported: the biz layer is the only caller (Preview, SaveConfig).
func mergeWithDefaults(p SelfDeployPlaceholders) SelfDeployPlaceholders {
	d := DefaultPlaceholders()
	if strings.TrimSpace(p.ImageTag) == "" {
		p.ImageTag = d.ImageTag
	}
	if p.ExposePort == 0 {
		p.ExposePort = d.ExposePort
	}
	if p.RecoveryPort == 0 {
		p.RecoveryPort = d.RecoveryPort
	}
	if len(p.RecoveryAllow) == 0 {
		p.RecoveryAllow = d.RecoveryAllow
	}
	if strings.TrimSpace(p.VolumeData) == "" {
		p.VolumeData = d.VolumeData
	}
	if strings.TrimSpace(p.NetworkName) == "" {
		p.NetworkName = d.NetworkName
	}
	if strings.TrimSpace(p.ContainerName) == "" {
		p.ContainerName = d.ContainerName
	}
	return p
}

// selfDeployFuncMap is the template FuncMap shared by RenderTemplate.
// Kept separate so unit tests (and a future YAML linter) can reuse it.
var selfDeployFuncMap = template.FuncMap{
	// join concatenates a slice with a separator. Useful when the
	// template wants a single comma-separated line of allow-list
	// CIDRs, for example.
	"join": func(sep string, items []string) string {
		return strings.Join(items, sep)
	},
	// quote wraps the argument in double quotes and escapes inner
	// quotes — safe for YAML scalar emission of arbitrary labels.
	"quote": func(v any) string {
		return strconv.Quote(fmt.Sprint(v))
	},
}

// RenderTemplate parses tmpl as a Go text/template, executes it with
// the placeholder struct (after merging with defaults), and returns
// the rendered string. A parse error or an execute error is wrapped
// with enough context that the Preview endpoint surfaces an
// actionable message to the operator.
func RenderTemplate(tmpl string, p SelfDeployPlaceholders) (string, error) {
	if strings.TrimSpace(tmpl) == "" {
		return "", fmt.Errorf("self-deploy template is empty")
	}
	t, err := template.New("self-deploy").Funcs(selfDeployFuncMap).Parse(tmpl)
	if err != nil {
		return "", fmt.Errorf("self-deploy template: parse failed: %w", err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, mergeWithDefaults(p)); err != nil {
		return "", fmt.Errorf("self-deploy template: render failed: %w", err)
	}
	return buf.String(), nil
}

// LoadSeedTemplate returns the embedded seed compose template. Callers
// that want to customise should Render the seed, edit the output, and
// save that as the persisted template via biz.SelfDeployBiz.SaveConfig.
func LoadSeedTemplate() string {
	return seedSelfDeployTemplate
}
