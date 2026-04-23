package biz

import (
	"strings"
	"testing"
)

const traefikMinimalStack = `services:
  web:
    image: nginx:alpine
`

// The new TraefikServiceCfg is a flat label map. These tests verify:
//   - label emission (mapping + sequence form, mode-aware placement);
//   - namespace ownership (full purge-and-replace for wizard-touched services);
//   - round-trip fidelity (reverse parse → inject → re-parse with the exact
//     original label set);
//   - format compatibility (sequence-form compose files).

func TestInjectEmitsWizardLabelsStandalone(t *testing.T) {
	cfg := &AddonsConfig{
		Traefik: map[string]TraefikServiceCfg{
			"web": {
				Enabled: true,
				Labels: map[string]string{
					"traefik.http.routers.web.rule":                               "Host(`demo.local`)",
					"traefik.http.routers.web.entrypoints":                        "websecure",
					"traefik.http.services.web.loadbalancer.server.port":          "80",
					"traefik.http.routers.web.tls":                                "true",
					"traefik.http.routers.web.tls.certresolver":                   "letsencrypt",
				},
			},
		},
	}
	out, err := injectAddonLabels(traefikMinimalStack, cfg, "standalone")
	if err != nil {
		t.Fatalf("inject failed: %v", err)
	}
	for _, needle := range []string{
		"traefik.enable: \"true\"",
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
	if !strings.Contains(out, "  web:\n    image: nginx:alpine\n    labels:") {
		t.Errorf("expected labels: under web service, got:\n%s", out)
	}
}

func TestInjectEmitsWizardLabelsSwarm(t *testing.T) {
	cfg := &AddonsConfig{
		Traefik: map[string]TraefikServiceCfg{
			"web": {
				Enabled: true,
				Labels: map[string]string{
					"traefik.http.routers.web.rule": "Host(`demo.local`)",
				},
			},
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

// Namespace ownership: a wizard-touched service gets its entire traefik.*
// set replaced. User's non-traefik labels (my.custom) survive.
func TestTraefikNamespaceFullReplace(t *testing.T) {
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
			"web": {
				Enabled: true,
				Labels: map[string]string{
					"traefik.http.routers.web.rule": "Host(`new.local`)",
				},
			},
		},
	}
	out, err := injectAddonLabels(input, cfg, "standalone")
	if err != nil {
		t.Fatalf("inject failed: %v", err)
	}
	if strings.Contains(out, "traefik.http.routers.OLD") {
		t.Errorf("expected OLD router purged, got:\n%s", out)
	}
	if strings.Contains(out, "traefik.enable: \"false\"") {
		t.Errorf("expected traefik.enable overwritten to true, got:\n%s", out)
	}
	if !strings.Contains(out, "traefik.enable: \"true\"") {
		t.Errorf("expected traefik.enable=true, got:\n%s", out)
	}
	if !strings.Contains(out, "Host(`new.local`)") {
		t.Errorf("expected new Host rule, got:\n%s", out)
	}
	if !strings.Contains(out, "my.custom: value") {
		t.Errorf("expected my.custom user label to survive, got:\n%s", out)
	}
}

// Disabled + no labels → every traefik.* on the service is purged.
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

// Roundtrip: extract -> re-inject reproduces the exact label set.
func TestTraefikRoundTripPreservesEverything(t *testing.T) {
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
	parsed, err := extractAddonConfig(input)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	tc := parsed.Traefik["keycloak"]
	if !tc.Enabled {
		t.Fatalf("expected Enabled=true, got %+v", tc)
	}
	for _, k := range []string{
		"traefik.http.routers.keycloak.rule",
		"traefik.http.routers.keycloak.tls.options",
		"traefik.http.routers.keycloak-int.rule",
		"traefik.http.routers.keycloak-int.middlewares",
	} {
		if _, ok := tc.Labels[k]; !ok {
			t.Errorf("expected %q in Labels, got keys: %v", k, keysOf(tc.Labels))
		}
	}
	out, err := injectAddonLabels(input, &AddonsConfig{Traefik: map[string]TraefikServiceCfg{"keycloak": tc}}, "standalone")
	if err != nil {
		t.Fatalf("inject: %v", err)
	}
	for _, needle := range []string{
		"traefik.enable=true",
		"Host(`security.example.com`)",
		"cross@file",
		"tls.options=modern@file",
		"keycloak-int.rule",
		"keycloak-int.entrypoints=https",
		"internalnet@file",
	} {
		if !strings.Contains(out, needle) {
			t.Errorf("round-trip lost %q, got:\n%s", needle, out)
		}
	}
}

func TestSequenceLabelsRoundTripReverseParse(t *testing.T) {
	input := `services:
  web:
    image: nginx:alpine
    labels:
      - "traefik.enable=true"
      - "traefik.http.routers.web.rule=Host(` + "`foo.example.com`" + `)"
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
	if tc.Labels["traefik.http.routers.web.rule"] != "Host(`foo.example.com`)" {
		t.Errorf("rule not captured in Labels: %v", tc.Labels)
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
			"web": {
				Enabled: true,
				Labels: map[string]string{
					"traefik.http.routers.web.rule": "Host(`new.local`)",
				},
			},
		},
	}
	out, err := injectAddonLabels(input, cfg, "standalone")
	if err != nil {
		t.Fatalf("inject: %v", err)
	}
	if !strings.Contains(out, "- traefik.enable=true") &&
		!strings.Contains(out, "- \"traefik.enable=true\"") {
		t.Errorf("expected sequence-form labels on output, got:\n%s", out)
	}
	for _, needle := range []string{
		"com.centurylinklabs.watchtower.enable=true",
		"my.custom=value",
		"new.local",
	} {
		if !strings.Contains(out, needle) {
			t.Errorf("expected %q in output, got:\n%s", needle, out)
		}
	}
	if strings.Contains(out, "old.local") {
		t.Errorf("expected old Traefik rule purged, got:\n%s", out)
	}
}

// applyResources always writes under deploy.resources (compose-spec
// unified form) and purges legacy top-level cpus/mem_limit/mem_reservation
// so a stack edited via the wizard never carries both forms.
func TestResourcesRoundtripUnified(t *testing.T) {
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
		"deploy:",
		"resources:",
		"limits:",
		"cpus: \"0.5\"",
		"memory: 256M",
		"reservations:",
		"memory: 128M",
	} {
		if !strings.Contains(out, needle) {
			t.Errorf("expected %q in output, got:\n%s", needle, out)
		}
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

// Legacy top-level resource fields (cpus, mem_limit) are ALWAYS purged
// when the wizard writes the unified deploy.resources form, even on a
// service that previously carried both.
func TestResourcesEmptyPurgesLegacy(t *testing.T) {
	input := `services:
  web:
    image: nginx:alpine
    cpus: "2"
    mem_limit: 1G
`
	cfg := &AddonsConfig{
		Resources: map[string]ResourcesServiceCfg{"web": {}},
	}
	out, err := injectAddonLabels(input, cfg, "standalone")
	if err != nil {
		t.Fatalf("inject: %v", err)
	}
	if strings.Contains(out, "cpus:") || strings.Contains(out, "mem_limit:") {
		t.Errorf("expected legacy resources purged, got:\n%s", out)
	}
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

func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
