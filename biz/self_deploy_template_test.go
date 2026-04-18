package biz

import (
	"strings"
	"testing"

	"github.com/cuigh/swirl/docker/compose"
)

// TestRenderTemplateSeedWithDefaults is the baseline smoke test: the
// embedded seed template, rendered with DefaultPlaceholders, must
// produce a compose file the standalone engine's own parser accepts.
// If this ever regresses, the seed template is broken and Preview
// would mislead operators.
//
// Post-v1.1: the seed no longer marks volume/network as `external: true`.
// They are now managed by the compose engine (created on first deploy,
// preserved across redeploys by Docker's default named-volume lifecycle).
// Operators who need external resources can still add `external: true`
// in their customised template — the engine and the validator handle
// both shapes.
func TestRenderTemplateSeedWithDefaults(t *testing.T) {
	yaml, err := RenderTemplate(LoadSeedTemplate(), DefaultPlaceholders())
	if err != nil {
		t.Fatalf("render seed: %v", err)
	}
	if !strings.Contains(yaml, "cuigh/swirl:latest") {
		t.Fatalf("rendered YAML should contain default image tag, got:\n%s", yaml)
	}
	if strings.Contains(yaml, "external: true") {
		t.Fatalf("seed must NOT mark volume/network as external (they are compose-managed), got:\n%s", yaml)
	}
	if _, err := compose.Parse("self-deploy-seed", yaml); err != nil {
		t.Fatalf("compose.Parse refused seed render: %v\n---\n%s", err, yaml)
	}
}

// TestRenderTemplateWithFullPlaceholders exercises every struct field:
// a populated input should render a compose file that still parses
// (no stray template markers, correct quoting on labels).
func TestRenderTemplateWithFullPlaceholders(t *testing.T) {
	p := SelfDeployPlaceholders{
		ImageTag:      "custom/swirl:v1.2.3",
		ExposePort:    9001,
		RecoveryPort:  9002,
		RecoveryAllow: []string{"10.0.0.0/24", "127.0.0.1/32"},
		TraefikLabels: []string{
			"traefik.enable=true",
			"traefik.http.routers.swirl.rule=Host(`swirl.example.com`)",
		},
		VolumeData:    "custom_data",
		NetworkName:   "custom_net",
		ContainerName: "swirl-custom",
		ExtraEnv: map[string]string{
			"TZ":        "Europe/Rome",
			"LOG_LEVEL": "info",
		},
	}
	yaml, err := RenderTemplate(LoadSeedTemplate(), p)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, want := range []string{
		"custom/swirl:v1.2.3",
		"9001:9001",
		"custom_data",
		"custom_net",
		"swirl-custom",
		"TZ=Europe/Rome",
		"LOG_LEVEL=info",
		"\"traefik.enable=true\"",
	} {
		if !strings.Contains(yaml, want) {
			t.Fatalf("expected %q in rendered YAML, got:\n%s", want, yaml)
		}
	}
	if _, err := compose.Parse("self-deploy-custom", yaml); err != nil {
		t.Fatalf("compose.Parse refused custom render: %v\n---\n%s", err, yaml)
	}
}

// TestRenderTemplateEmptyPlaceholdersFallToDefaults: operator submits
// a zero-valued struct (happens on first save). mergeWithDefaults
// inside RenderTemplate must kick in, the render must still succeed,
// and the result must be identical to DefaultPlaceholders.
func TestRenderTemplateEmptyPlaceholdersFallToDefaults(t *testing.T) {
	empty, err := RenderTemplate(LoadSeedTemplate(), SelfDeployPlaceholders{})
	if err != nil {
		t.Fatalf("render empty: %v", err)
	}
	defaults, err := RenderTemplate(LoadSeedTemplate(), DefaultPlaceholders())
	if err != nil {
		t.Fatalf("render defaults: %v", err)
	}
	if empty != defaults {
		t.Fatalf("empty render should equal defaults render\nempty:\n%s\n---\ndefaults:\n%s", empty, defaults)
	}
}

// TestRenderTemplateMalformed exercises the parse-error path: a
// template with an unclosed action marker must surface a clear error
// at parse time, not silently render an empty or truncated YAML.
func TestRenderTemplateMalformed(t *testing.T) {
	broken := "services:\n  x:\n    image: {{.ImageTag" // missing closing braces
	_, err := RenderTemplate(broken, DefaultPlaceholders())
	if err == nil {
		t.Fatalf("expected parse error, got nil")
	}
	if !strings.Contains(err.Error(), "parse failed") {
		t.Fatalf("expected 'parse failed' in error, got %v", err)
	}
}

// TestRenderTemplateEmpty verifies the renderer rejects an empty input
// string explicitly instead of silently producing an empty YAML that
// would later fail inside compose.Parse with a less actionable error.
func TestRenderTemplateEmpty(t *testing.T) {
	_, err := RenderTemplate("   ", DefaultPlaceholders())
	if err == nil {
		t.Fatalf("expected error on empty template, got nil")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Fatalf("expected 'empty' in error, got %v", err)
	}
}

// TestRenderTemplateUnknownField guards the template-contract: if the
// seed template references a placeholder field that does not exist on
// the struct, the execute step surfaces the error. Regression safety
// for future renames.
func TestRenderTemplateUnknownField(t *testing.T) {
	tmpl := "image: {{.NonExistentField}}"
	_, err := RenderTemplate(tmpl, DefaultPlaceholders())
	if err == nil {
		t.Fatalf("expected error on unknown field, got nil")
	}
}

// TestDefaultPlaceholdersStable ensures the default struct carries
// every sane value we expect the rest of the pipeline to rely on.
// Plain Go assertions — no Docker, no network.
func TestDefaultPlaceholdersStable(t *testing.T) {
	d := DefaultPlaceholders()
	if d.ImageTag == "" || d.ExposePort == 0 || d.RecoveryPort == 0 {
		t.Fatalf("defaults must populate scalar fields, got %+v", d)
	}
	if len(d.RecoveryAllow) == 0 {
		t.Fatalf("defaults must ship at least one CIDR in RecoveryAllow")
	}
}
