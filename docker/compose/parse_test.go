package compose

import (
	"testing"
)

// TestParseDependsOnShortForm verifies the list form of depends_on:
//
//	depends_on:
//	  - postgres
//	  - vault-init
//
// Short-form entries must produce ServiceDependency entries with empty
// Condition (the deploy engine treats that as service_started).
func TestParseDependsOnShortForm(t *testing.T) {
	yaml := `version: "3.8"
services:
  app:
    image: myapp:latest
    depends_on:
      - postgres
      - vault-init
`
	cfg, err := Parse("test", yaml)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(cfg.Services) != 1 {
		t.Fatalf("expected 1 service, got %d", len(cfg.Services))
	}
	got := cfg.Services[0].DependsOn
	if len(got) != 2 {
		t.Fatalf("expected 2 deps, got %d: %+v", len(got), got)
	}
	found := map[string]string{}
	for _, d := range got {
		found[d.Service] = d.Condition
	}
	c, ok := found["postgres"]
	if !ok {
		t.Fatalf("missing postgres dep, got %+v", got)
	}
	if c != "" {
		t.Fatalf("expected empty condition for short form, got %q", c)
	}
	if _, ok := found["vault-init"]; !ok {
		t.Fatalf("missing vault-init dep, got %+v", got)
	}
}

// TestParseDependsOnLongForm verifies the map form of depends_on keeps
// the per-service condition so the deploy engine can honour it.
func TestParseDependsOnLongForm(t *testing.T) {
	yaml := `version: "3.8"
services:
  vault-init:
    image: hashicorp/vault:latest
    depends_on:
      vault:
        condition: service_healthy
      postgres:
        condition: service_started
`
	cfg, err := Parse("test", yaml)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(cfg.Services) != 1 {
		t.Fatalf("expected 1 service, got %d", len(cfg.Services))
	}
	got := cfg.Services[0].DependsOn
	if len(got) != 2 {
		t.Fatalf("expected 2 deps, got %d: %+v", len(got), got)
	}
	// Map form is sorted alphabetically for determinism.
	if got[0].Service != "postgres" || got[0].Condition != "service_started" {
		t.Fatalf("expected postgres/service_started first, got %+v", got[0])
	}
	if got[1].Service != "vault" || got[1].Condition != "service_healthy" {
		t.Fatalf("expected vault/service_healthy second, got %+v", got[1])
	}
}

// TestParseDependsOnEmpty verifies that a service without depends_on
// still parses cleanly.
func TestParseDependsOnEmpty(t *testing.T) {
	yaml := `version: "3.8"
services:
  app:
    image: myapp:latest
`
	cfg, err := Parse("test", yaml)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(cfg.Services[0].DependsOn) != 0 {
		t.Fatalf("expected no deps, got %+v", cfg.Services[0].DependsOn)
	}
}

// TestParseDependsOnUserRegression reproduces a compose file using the
// long form with service_healthy — the original motivating scenario.
func TestParseDependsOnUserRegression(t *testing.T) {
	yaml := `version: "3.8"
services:
  vault:
    image: hashicorp/vault:1.15
  vault-init:
    image: hashicorp/vault:1.15
    depends_on:
      vault:
        condition: service_healthy
`
	cfg, err := Parse("test", yaml)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	var initDeps []string
	var initConds []string
	for _, s := range cfg.Services {
		if s.Name == "vault-init" {
			for _, d := range s.DependsOn {
				initDeps = append(initDeps, d.Service)
				initConds = append(initConds, d.Condition)
			}
			break
		}
	}
	if len(initDeps) != 1 || initDeps[0] != "vault" {
		t.Fatalf("expected [vault], got %v", initDeps)
	}
	if len(initConds) != 1 || initConds[0] != "service_healthy" {
		t.Fatalf("expected condition service_healthy, got %v", initConds)
	}
}
