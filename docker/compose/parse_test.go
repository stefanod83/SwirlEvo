package compose

import (
	"testing"
)

// TestParseDependsOnShortForm verifies the list form of depends_on:
//
//	depends_on:
//	  - postgres
//	  - vault-init
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
		t.Fatalf("expected 2 deps, got %d: %v", len(got), got)
	}
	found := map[string]bool{}
	for _, s := range got {
		found[s] = true
	}
	if !found["postgres"] || !found["vault-init"] {
		t.Fatalf("expected [postgres vault-init], got %v", got)
	}
}

// TestParseDependsOnLongForm verifies the map form of depends_on:
//
//	depends_on:
//	  vault:
//	    condition: service_healthy
//	  postgres:
//	    condition: service_started
//
// Only the service names should survive — condition/restart/required are
// discarded because Swirl's standalone engine does not enforce readiness.
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
		t.Fatalf("expected 2 deps, got %d: %v", len(got), got)
	}
	// Map form is sorted alphabetically for determinism.
	if got[0] != "postgres" || got[1] != "vault" {
		t.Fatalf("expected sorted [postgres vault], got %v", got)
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
		t.Fatalf("expected no deps, got %v", cfg.Services[0].DependsOn)
	}
}

// TestParseDependsOnUserRegression reproduces the original user report:
// a compose file using the long form for vault-init's depends_on on vault.
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
	var init *struct {
		Deps []string
	}
	for _, s := range cfg.Services {
		if s.Name == "vault-init" {
			init = &struct {
				Deps []string
			}{Deps: []string(s.DependsOn)}
			break
		}
	}
	if init == nil {
		t.Fatalf("vault-init service not found")
	}
	if len(init.Deps) != 1 || init.Deps[0] != "vault" {
		t.Fatalf("expected [vault], got %v", init.Deps)
	}
}
