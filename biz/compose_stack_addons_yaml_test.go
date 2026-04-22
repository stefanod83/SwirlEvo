package biz

import (
	"strings"
	"testing"
)

const traefikMinimalStack = `services:
  web:
    image: nginx:alpine
`

func TestInjectTraefikLabelsStandalone(t *testing.T) {
	cfg := &AddonsConfig{
		Traefik: map[string]TraefikServiceCfg{
			"web": {
				Enabled:      true,
				Router:       "web",
				RuleType:     "Host",
				Domain:       "demo.local",
				Entrypoint:   "websecure",
				Port:         80,
				TLS:          true,
				CertResolver: "letsencrypt",
			},
		},
	}
	out, err := injectAddonLabels(traefikMinimalStack, cfg, "standalone")
	if err != nil {
		t.Fatalf("inject failed: %v", err)
	}
	for _, needle := range []string{
		"traefik.enable: \"true\"",
		"traefik.http.routers.web.rule:",
		"Host(`demo.local`)",
		"traefik.http.routers.web.entrypoints: websecure",
		"traefik.http.services.web.loadbalancer.server.port: \"80\"",
		"traefik.http.routers.web.tls: \"true\"",
		"traefik.http.routers.web.tls.certresolver: letsencrypt",
	} {
		if !strings.Contains(out, needle) {
			t.Errorf("expected %q in output, got:\n%s", needle, out)
		}
	}
	// Marker is gone — no output must mention it.
	if strings.Contains(out, "# swirl-managed") {
		t.Errorf("marker should no longer be emitted, got:\n%s", out)
	}
	if !strings.Contains(out, "  web:\n    image: nginx:alpine\n    labels:") {
		t.Errorf("expected labels: under web service, got:\n%s", out)
	}
}

func TestInjectTraefikLabelsSwarm(t *testing.T) {
	cfg := &AddonsConfig{
		Traefik: map[string]TraefikServiceCfg{
			"web": {Enabled: true, Router: "web", RuleType: "Host", Domain: "demo.local", Port: 8080},
		},
	}
	out, err := injectAddonLabels(traefikMinimalStack, cfg, "swarm")
	if err != nil {
		t.Fatalf("inject failed: %v", err)
	}
	if !strings.Contains(out, "deploy:") || !strings.Contains(out, "      labels:") {
		t.Errorf("expected deploy.labels placement in swarm mode, got:\n%s", out)
	}
	if strings.Contains(out, "\n    labels:") {
		t.Errorf("swarm mode must not emit top-level labels, got:\n%s", out)
	}
}

// Replace semantics: when a service is present in cfg.Traefik, all existing
// traefik.* labels on that service are wiped — regardless of who wrote them.
// The wizard now owns the whole Traefik namespace of a touched service.
func TestTraefikReplacesAllExistingLabels(t *testing.T) {
	input := `services:
  web:
    image: nginx:alpine
    labels:
      traefik.enable: "false"
      traefik.http.routers.OLD.rule: Host(` + "`old.local`" + `)
      my.custom: value
`
	cfg := &AddonsConfig{
		Traefik: map[string]TraefikServiceCfg{
			"web": {Enabled: true, Router: "web", RuleType: "Host", Domain: "new.local", Port: 80},
		},
	}
	out, err := injectAddonLabels(input, cfg, "standalone")
	if err != nil {
		t.Fatalf("inject failed: %v", err)
	}
	// Old router name must be purged.
	if strings.Contains(out, "traefik.http.routers.OLD") {
		t.Errorf("expected OLD router purged, got:\n%s", out)
	}
	// User-managed traefik.enable=false must be overwritten by the form.
	if strings.Contains(out, "traefik.enable: \"false\"") {
		t.Errorf("expected traefik.enable overwritten by the form, got:\n%s", out)
	}
	if !strings.Contains(out, "traefik.enable: \"true\"") {
		t.Errorf("expected traefik.enable=true, got:\n%s", out)
	}
	if !strings.Contains(out, "Host(`new.local`)") {
		t.Errorf("expected new Host rule, got:\n%s", out)
	}
	// Non-Traefik user label must survive — the wizard only owns the
	// traefik.* namespace.
	if !strings.Contains(out, "my.custom: value") {
		t.Errorf("expected my.custom user label to survive, got:\n%s", out)
	}
}

// Disabled service clears its Traefik namespace entirely.
func TestTraefikDisabledPurgesNamespace(t *testing.T) {
	input := `services:
  web:
    image: nginx:alpine
    labels:
      traefik.enable: "true"
      traefik.http.routers.web.rule: Host(` + "`foo.local`" + `)
      my.custom: value
`
	cfg := &AddonsConfig{
		Traefik: map[string]TraefikServiceCfg{
			"web": {Enabled: false},
		},
	}
	out, err := injectAddonLabels(input, cfg, "standalone")
	if err != nil {
		t.Fatalf("inject failed: %v", err)
	}
	if strings.Contains(out, "traefik.") {
		t.Errorf("expected every traefik.* label purged, got:\n%s", out)
	}
	if !strings.Contains(out, "my.custom: value") {
		t.Errorf("expected my.custom user label to survive, got:\n%s", out)
	}
}

func TestRoundTripTraefikCfg(t *testing.T) {
	original := TraefikServiceCfg{
		Enabled:      true,
		Router:       "api",
		RuleType:     "Host+PathPrefix",
		Domain:       "demo.local",
		Path:         "/v1",
		Entrypoint:   "websecure",
		Port:         9000,
		TLS:          true,
		CertResolver: "letsencrypt",
		Middlewares:  []string{"auth@docker", "ratelimit@docker"},
	}
	cfg := &AddonsConfig{Traefik: map[string]TraefikServiceCfg{"web": original}}
	out, err := injectAddonLabels(traefikMinimalStack, cfg, "standalone")
	if err != nil {
		t.Fatalf("inject: %v", err)
	}
	reversed, err := extractAddonConfig(out)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	got, ok := reversed.Traefik["web"]
	if !ok {
		t.Fatalf("expected Traefik['web'] to be reconstructed, got %+v", reversed)
	}
	if got.RuleType != original.RuleType || got.Domain != original.Domain || got.Path != original.Path {
		t.Errorf("rule mismatch: got %+v, want %+v", got, original)
	}
	if got.Port != original.Port {
		t.Errorf("port mismatch: got %d, want %d", got.Port, original.Port)
	}
	if got.CertResolver != original.CertResolver || !got.TLS {
		t.Errorf("tls/certresolver mismatch: got %+v", got)
	}
	if strings.Join(got.Middlewares, ",") != strings.Join(original.Middlewares, ",") {
		t.Errorf("middlewares mismatch: got %v, want %v", got.Middlewares, original.Middlewares)
	}
}

// Reverse parse picks up labels written by hand too — no marker required.
func TestReverseParsePicksUpManualLabels(t *testing.T) {
	input := `services:
  web:
    image: nginx:alpine
    labels:
      traefik.enable: "true"
      traefik.http.routers.web.rule: Host(` + "`manual.local`" + `)
      traefik.http.services.web.loadbalancer.server.port: "3000"
`
	cfg, err := extractAddonConfig(input)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	got, ok := cfg.Traefik["web"]
	if !ok {
		t.Fatalf("expected Traefik['web'], got %+v", cfg)
	}
	if got.Domain != "manual.local" || got.Port != 3000 {
		t.Errorf("reverse parse mismatch: got %+v", got)
	}
}

func TestResourcesStandaloneRoundtrip(t *testing.T) {
	cfg := &AddonsConfig{
		Resources: map[string]ResourcesServiceCfg{
			"web": {CPUsLimit: "0.5", MemoryLimit: "256M", MemoryReservation: "128M"},
		},
	}
	out, err := injectAddonLabels(traefikMinimalStack, cfg, "standalone")
	if err != nil {
		t.Fatalf("inject: %v", err)
	}
	for _, needle := range []string{
		"cpus: \"0.5\"",
		"mem_limit: 256M",
		"mem_reservation: 128M",
	} {
		if !strings.Contains(out, needle) {
			t.Errorf("expected %q in output, got:\n%s", needle, out)
		}
	}
	if strings.Contains(out, "# swirl-managed") {
		t.Errorf("marker should no longer be emitted, got:\n%s", out)
	}
	reversed, err := extractAddonConfig(out)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	got := reversed.Resources["web"]
	if got.CPUsLimit != "0.5" || got.MemoryLimit != "256M" || got.MemoryReservation != "128M" {
		t.Errorf("roundtrip mismatch: got %+v", got)
	}
}

// Empty resource cfg for a service purges the resource block entirely.
func TestResourcesEmptyPurges(t *testing.T) {
	input := `services:
  web:
    image: nginx:alpine
    cpus: "2"
    mem_limit: 1G
`
	cfg := &AddonsConfig{
		Resources: map[string]ResourcesServiceCfg{
			"web": {},
		},
	}
	out, err := injectAddonLabels(input, cfg, "standalone")
	if err != nil {
		t.Fatalf("inject: %v", err)
	}
	if strings.Contains(out, "cpus:") || strings.Contains(out, "mem_limit:") {
		t.Errorf("expected resources purged, got:\n%s", out)
	}
}

// --- sequence-form label support ---------------------------------------

func TestSequenceLabelsRoundTripReverseParse(t *testing.T) {
	// Real-world compose: labels as a sequence of "k=v" strings.
	input := `services:
  web:
    image: nginx:alpine
    labels:
      - "traefik.enable=true"
      - "traefik.http.routers.web.rule=Host(` + "`foo.example.com`" + `)"
      - "traefik.http.services.web.loadbalancer.server.port=80"
      - "com.centurylinklabs.watchtower.enable=true"
`
	cfg, err := extractAddonConfig(input)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	tc, ok := cfg.Traefik["web"]
	if !ok || !tc.Enabled {
		t.Fatalf("expected Traefik['web'].Enabled=true, got %+v", cfg.Traefik)
	}
	if tc.Domain != "foo.example.com" || tc.Port != 80 {
		t.Errorf("structured fields mismatch: got %+v", tc)
	}
}

func TestSequenceLabelsPreservedOnSave(t *testing.T) {
	input := `services:
  web:
    image: nginx:alpine
    labels:
      - "traefik.enable=true"
      - "traefik.http.routers.web.rule=Host(` + "`old.local`" + `)"
      - "com.centurylinklabs.watchtower.enable=true"
      - "my.custom=value"
`
	cfg := &AddonsConfig{
		Traefik: map[string]TraefikServiceCfg{
			"web": {Enabled: true, Router: "web", RuleType: "Host", Domain: "new.local", Port: 80},
		},
	}
	out, err := injectAddonLabels(input, cfg, "standalone")
	if err != nil {
		t.Fatalf("inject: %v", err)
	}
	// Must stay sequence-form: the writer preserves the existing node type.
	if !strings.Contains(out, "- traefik.enable=true") &&
		!strings.Contains(out, "- \"traefik.enable=true\"") {
		t.Errorf("expected sequence-form labels on output, got:\n%s", out)
	}
	// Unrelated addon labels (Watchtower) + user's own my.custom must survive.
	for _, needle := range []string{
		"com.centurylinklabs.watchtower.enable=true",
		"my.custom=value",
		"traefik.http.routers.web.rule",
		"new.local",
	} {
		if !strings.Contains(out, needle) {
			t.Errorf("expected %q in output, got:\n%s", needle, out)
		}
	}
	// The old Host rule must be gone (Traefik namespace fully replaced
	// with the wizard output).
	if strings.Contains(out, "old.local") {
		t.Errorf("expected old Traefik rule purged, got:\n%s", out)
	}
}

// --- passthrough (ExtraLabels) -----------------------------------------

func TestExtraLabelsPreserveMultiRouterAndTlsOptions(t *testing.T) {
	// Mirrors the user-reported real-world YAML: two routers on the same
	// service + tls.options + middleware with `@file` provider. None of
	// these are modeled by the wizard's structured fields; all must
	// survive a round-trip via ExtraLabels.
	input := `services:
  keycloak:
    image: quay.io/keycloak/keycloak:latest
    labels:
      - "traefik.enable=true"
      - "traefik.http.routers.keycloak.rule=Host(` + "`security.example.com`" + `)"
      - "traefik.http.routers.keycloak.entrypoints=https"
      - "traefik.http.routers.keycloak.middlewares=cross@file"
      - "traefik.http.routers.keycloak.tls=true"
      - "traefik.http.routers.keycloak.tls.options=modern@file"
      - "traefik.http.routers.keycloak.tls.certresolver=letsencrypt"
      - "traefik.http.routers.keycloak-int.rule=Host(` + "`kc.internal`" + `)"
      - "traefik.http.routers.keycloak-int.entrypoints=https"
      - "traefik.http.routers.keycloak-int.tls=true"
      - "traefik.http.routers.keycloak-int.middlewares=internalnet@file"
`
	cfg, err := extractAddonConfig(input)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	tc := cfg.Traefik["keycloak"]
	if !tc.Enabled {
		t.Errorf("expected Traefik.Enabled=true, got %+v", tc)
	}
	// Port is in `expose`, not in a loadbalancer label — the reverse
	// parser intentionally does NOT consume into structured fields when
	// it can't fully reconstruct the router (rule + port). Everything
	// must land in ExtraLabels instead so the round-trip is loss-free.
	wantInExtras := []string{
		"traefik.http.routers.keycloak.rule",
		"traefik.http.routers.keycloak.entrypoints",
		"traefik.http.routers.keycloak.middlewares",
		"traefik.http.routers.keycloak.tls",
		"traefik.http.routers.keycloak.tls.options",
		"traefik.http.routers.keycloak.tls.certresolver",
		"traefik.http.routers.keycloak-int.rule",
		"traefik.http.routers.keycloak-int.entrypoints",
		"traefik.http.routers.keycloak-int.tls",
		"traefik.http.routers.keycloak-int.middlewares",
	}
	for _, k := range wantInExtras {
		if _, ok := tc.ExtraLabels[k]; !ok {
			t.Errorf("expected %q in ExtraLabels, got keys: %v", k, keysOf(tc.ExtraLabels))
		}
	}

	// Round-trip: re-emit and verify EVERY original label is still in
	// the output. This is the contract operators rely on — the wizard
	// must never drop a hand-written Traefik label.
	re := &AddonsConfig{Traefik: map[string]TraefikServiceCfg{"keycloak": tc}}
	out, err := injectAddonLabels(input, re, "standalone")
	if err != nil {
		t.Fatalf("re-inject: %v", err)
	}
	for _, needle := range []string{
		"traefik.http.routers.keycloak.tls.options=modern@file",
		"traefik.http.routers.keycloak-int.rule",
		"keycloak-int.entrypoints=https",
		"keycloak-int.middlewares=internalnet@file",
		"cross@file",
	} {
		if !strings.Contains(out, needle) {
			t.Errorf("round-trip lost %q, got:\n%s", needle, out)
		}
	}
}

// Wizard disabled with no structured fields but ExtraLabels present:
// re-emit only the extras. Covers the case of a service that has
// passthrough-only Traefik config the operator hasn't activated in the
// wizard yet.
func TestExtraLabelsEmitWhenWizardDisabled(t *testing.T) {
	cfg := &AddonsConfig{
		Traefik: map[string]TraefikServiceCfg{
			"web": {
				Enabled: false,
				ExtraLabels: map[string]string{
					"traefik.http.routers.special.rule":        "Host(`custom.local`)",
					"traefik.http.routers.special.entrypoints": "web",
				},
			},
		},
	}
	out, err := injectAddonLabels(traefikMinimalStack, cfg, "standalone")
	if err != nil {
		t.Fatalf("inject: %v", err)
	}
	if !strings.Contains(out, "traefik.http.routers.special.rule") {
		t.Errorf("expected passthrough emitted even when wizard disabled, got:\n%s", out)
	}
	if strings.Contains(out, "traefik.enable") {
		t.Errorf("wizard is disabled, traefik.enable must not be emitted, got:\n%s", out)
	}
}

func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestNoOpWhenCfgEmpty(t *testing.T) {
	out, err := injectAddonLabels(traefikMinimalStack, nil, "standalone")
	if err != nil {
		t.Fatalf("inject(nil) failed: %v", err)
	}
	if out != traefikMinimalStack {
		t.Errorf("expected verbatim YAML when cfg is nil, got:\n%s", out)
	}
}
