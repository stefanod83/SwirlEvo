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
				Enabled:    true,
				Router:     "web",
				RuleType:   "Host",
				Domain:     "demo.local",
				Entrypoint: "websecure",
				Port:       80,
				TLS:        true,
				CertResolver: "letsencrypt",
			},
		},
	}
	out, err := injectAddonLabels(traefikMinimalStack, cfg, "standalone")
	if err != nil {
		t.Fatalf("inject failed: %v", err)
	}
	// Every wizard-emitted label must carry the marker so the reverse
	// parser recognises it on the next re-open.
	for _, needle := range []string{
		"traefik.enable: \"true\" # swirl-managed",
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
	// Standalone mode must land under the service's top-level labels key.
	if !strings.Contains(out, "  web:\n    image: nginx:alpine\n    labels:") {
		t.Errorf("expected labels: under web service, got:\n%s", out)
	}
}

func TestInjectTraefikLabelsSwarm(t *testing.T) {
	cfg := &AddonsConfig{
		Traefik: map[string]TraefikServiceCfg{
			"web": {
				Enabled:  true,
				Router:   "web",
				RuleType: "Host",
				Domain:   "demo.local",
				Port:     8080,
			},
		},
	}
	out, err := injectAddonLabels(traefikMinimalStack, cfg, "swarm")
	if err != nil {
		t.Fatalf("inject failed: %v", err)
	}
	// Swarm placement: labels sit under deploy.labels, never top-level.
	if !strings.Contains(out, "deploy:") || !strings.Contains(out, "      labels:") {
		t.Errorf("expected deploy.labels placement in swarm mode, got:\n%s", out)
	}
	if strings.Contains(out, "\n    labels:") {
		t.Errorf("swarm mode must not emit top-level labels, got:\n%s", out)
	}
}

func TestInjectPreservesUserManagedLabels(t *testing.T) {
	input := `services:
  web:
    image: nginx:alpine
    labels:
      traefik.enable: "false"
      my.custom: value
`
	cfg := &AddonsConfig{
		Traefik: map[string]TraefikServiceCfg{
			"web": {
				Enabled: true, Router: "web", RuleType: "Host", Domain: "demo.local", Port: 80,
			},
		},
	}
	out, err := injectAddonLabels(input, cfg, "standalone")
	if err != nil {
		t.Fatalf("inject failed: %v", err)
	}
	// The user's unmarked traefik.enable=false must stay intact — we
	// must NOT overwrite a label the operator hand-wrote.
	if !strings.Contains(out, "traefik.enable: \"false\"") {
		t.Errorf("expected user-managed traefik.enable=false to survive, got:\n%s", out)
	}
	// ...and the user's my.custom label must not get the marker.
	if strings.Contains(out, "my.custom: value # swirl-managed") {
		t.Errorf("user-managed label should not receive marker, got:\n%s", out)
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
	// Router name ("api") is independent from the service name ("web");
	// the YAML must list the same compose service the wizard configured.
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
	// yaml.v3 wraps numeric-looking scalars in quotes — accept both forms.
	for _, needle := range []string{
		"cpus: \"0.5\" # swirl-managed",
		"mem_limit: 256M # swirl-managed",
		"mem_reservation: 128M # swirl-managed",
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

func TestResourcesSwarmPlacement(t *testing.T) {
	cfg := &AddonsConfig{
		Resources: map[string]ResourcesServiceCfg{
			"web": {CPUsLimit: "1.0", MemoryLimit: "512M", CPUsReservation: "0.25"},
		},
	}
	out, err := injectAddonLabels(traefikMinimalStack, cfg, "swarm")
	if err != nil {
		t.Fatalf("inject: %v", err)
	}
	// Swarm placement: deploy.resources.{limits,reservations}
	if !strings.Contains(out, "limits:") || !strings.Contains(out, "cpus: \"1.0\"") {
		t.Errorf("expected deploy.resources.limits on swarm, got:\n%s", out)
	}
	if !strings.Contains(out, "reservations:") {
		t.Errorf("expected deploy.resources.reservations on swarm, got:\n%s", out)
	}
	// No top-level cpus on swarm mode.
	if strings.Contains(out, "\n    cpus: ") {
		t.Errorf("swarm mode must not emit top-level cpus, got:\n%s", out)
	}
}

func TestResourcesPreserveUserManaged(t *testing.T) {
	input := `services:
  web:
    image: nginx:alpine
    cpus: 2
    mem_limit: 1G
`
	cfg := &AddonsConfig{
		Resources: map[string]ResourcesServiceCfg{
			"web": {CPUsLimit: "0.5", MemoryLimit: "256M"},
		},
	}
	out, err := injectAddonLabels(input, cfg, "standalone")
	if err != nil {
		t.Fatalf("inject: %v", err)
	}
	// User wrote cpus: 2 without marker — must survive.
	if !strings.Contains(out, "cpus: 2") {
		t.Errorf("expected user-managed cpus: 2 to survive, got:\n%s", out)
	}
	if !strings.Contains(out, "mem_limit: 1G") {
		t.Errorf("expected user-managed mem_limit: 1G to survive, got:\n%s", out)
	}
	// And wizard cpus: 0.5 must NOT replace the user value.
	if strings.Contains(out, "cpus: 0.5 # swirl-managed") {
		t.Errorf("wizard should not overwrite user-managed cpus, got:\n%s", out)
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
