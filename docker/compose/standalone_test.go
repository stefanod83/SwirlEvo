package compose

import (
	"strings"
	"testing"

	composetypes "github.com/cuigh/swirl/docker/compose/types"
)

// TestValidateServicesOnlyImage is the baseline: a compose file with plain
// `image:` references must validate cleanly — this is the case that has
// always worked and MUST stay working (retro-compatibility).
func TestValidateServicesOnlyImage(t *testing.T) {
	cfg := &composetypes.Config{
		Services: []composetypes.ServiceConfig{
			{Name: "web", Image: "nginx:latest"},
			{Name: "db", Image: "postgres:16"},
		},
	}
	if err := validateServices(cfg); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

// TestValidateServicesBuildOnly verifies Opzione A1 strict: a service
// declaring only `build.context` (no `image:`) is rejected up-front rather
// than reaching ContainerCreate with an empty image reference.
func TestValidateServicesBuildOnly(t *testing.T) {
	cfg := &composetypes.Config{
		Services: []composetypes.ServiceConfig{
			{Name: "app", Build: composetypes.BuildConfig{Context: "./app"}},
		},
	}
	err := validateServices(cfg)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "service app") {
		t.Fatalf("expected error to mention service name, got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "'build:' is not supported") {
		t.Fatalf("expected error to mention unsupported build, got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "standalone mode") {
		t.Fatalf("expected error to mention standalone mode, got %q", err.Error())
	}
}

// TestValidateServicesBuildPlusImage verifies the strict behaviour: even
// when `image:` is also set, the presence of `build.context` triggers the
// same error. The compose spec would treat `image:` as the tag for the
// built image in this case — Swirl's standalone engine does NOT build, so
// the YAML is ambiguous and rejected to avoid the silent "no command
// specified" daemon error.
func TestValidateServicesBuildPlusImage(t *testing.T) {
	cfg := &composetypes.Config{
		Services: []composetypes.ServiceConfig{
			{
				Name:  "app",
				Image: "myorg/app:dev",
				Build: composetypes.BuildConfig{Context: "./app"},
			},
		},
	}
	err := validateServices(cfg)
	if err == nil {
		t.Fatalf("expected error (build overrides image in strict mode), got nil")
	}
	if !strings.Contains(err.Error(), "'build:' is not supported") {
		t.Fatalf("expected unsupported-build error, got %q", err.Error())
	}
}

// TestValidateServicesNeitherImageNorBuild verifies the second safety net:
// a malformed compose file with a service that has no image AND no build
// is rejected with a clear, actionable message.
func TestValidateServicesNeitherImageNorBuild(t *testing.T) {
	cfg := &composetypes.Config{
		Services: []composetypes.ServiceConfig{
			{Name: "broken"},
		},
	}
	err := validateServices(cfg)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "service broken") {
		t.Fatalf("expected error to mention service name, got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "neither 'image:' nor 'build:' is set") {
		t.Fatalf("expected error to mention missing image+build, got %q", err.Error())
	}
}

// TestValidateServicesBuildFirstServiceStops checks that validation fails
// on the first offending service (useful guarantee for error clarity: the
// operator gets one name to fix at a time).
func TestValidateServicesBuildFirstServiceStops(t *testing.T) {
	cfg := &composetypes.Config{
		Services: []composetypes.ServiceConfig{
			{Name: "web", Image: "nginx:latest"},
			{Name: "app", Build: composetypes.BuildConfig{Context: "./app"}},
			{Name: "worker", Image: "busybox"},
		},
	}
	err := validateServices(cfg)
	if err == nil {
		t.Fatalf("expected error on service 'app', got nil")
	}
	if !strings.Contains(err.Error(), "service app") {
		t.Fatalf("expected error to mention 'app', got %q", err.Error())
	}
}

// TestValidateServicesNilConfig is defensive: validateServices is called
// right after Parse, but a nil guard keeps the function pure and safe to
// reuse from other call sites.
func TestValidateServicesNilConfig(t *testing.T) {
	if err := validateServices(nil); err != nil {
		t.Fatalf("expected nil on nil config, got %v", err)
	}
}

// TestValidateServicesEmptyServices ensures a compose file with an empty
// services map validates cleanly (higher-level code handles the "no
// services" UX; the validator itself just checks what's present).
func TestValidateServicesEmptyServices(t *testing.T) {
	cfg := &composetypes.Config{
		Services: []composetypes.ServiceConfig{},
	}
	if err := validateServices(cfg); err != nil {
		t.Fatalf("expected nil on empty services, got %v", err)
	}
}

// TestCollectIgnoredFieldWarningsNoDeploy — retro-compat baseline: a
// compose file that does not declare any Swarm-only field must produce
// zero warnings. This is the case that existed before Phase 2 and MUST
// stay silent (otherwise we'd fire a warning on every single deploy).
func TestCollectIgnoredFieldWarningsNoDeploy(t *testing.T) {
	cfg := &composetypes.Config{
		Services: []composetypes.ServiceConfig{
			{Name: "web", Image: "nginx:latest"},
		},
	}
	if w := collectIgnoredFieldWarnings(cfg); len(w) != 0 {
		t.Fatalf("expected no warnings, got %v", w)
	}
}

// TestCollectIgnoredFieldWarningsDeployBlock — a service with a non-empty
// `deploy:` block must emit exactly one warning mentioning the service name.
func TestCollectIgnoredFieldWarningsDeployBlock(t *testing.T) {
	replicas := uint64(3)
	cfg := &composetypes.Config{
		Services: []composetypes.ServiceConfig{
			{Name: "worker", Image: "busybox", Deploy: composetypes.DeployConfig{Replicas: &replicas}},
		},
	}
	w := collectIgnoredFieldWarnings(cfg)
	if len(w) != 1 {
		t.Fatalf("expected 1 warning, got %d: %v", len(w), w)
	}
	if !strings.Contains(w[0], "service worker") {
		t.Fatalf("warning should mention service name, got %q", w[0])
	}
	if !strings.Contains(w[0], "deploy:") {
		t.Fatalf("warning should mention the deploy: block, got %q", w[0])
	}
}

// TestCollectIgnoredFieldWarningsMultipleServices — every offending service
// contributes its own warning; non-offending services do not.
func TestCollectIgnoredFieldWarningsMultipleServices(t *testing.T) {
	cfg := &composetypes.Config{
		Services: []composetypes.ServiceConfig{
			{Name: "web", Image: "nginx"},
			{Name: "api", Image: "api", Deploy: composetypes.DeployConfig{Mode: "replicated"}},
			{Name: "db", Image: "postgres"},
			{Name: "cache", Image: "redis", Deploy: composetypes.DeployConfig{EndpointMode: "dnsrr"}},
		},
	}
	w := collectIgnoredFieldWarnings(cfg)
	if len(w) != 2 {
		t.Fatalf("expected 2 warnings, got %d: %v", len(w), w)
	}
}

// TestIsZeroDeployConfig sanity-checks the zero-value detector for every
// branch a real compose file can populate.
func TestIsZeroDeployConfig(t *testing.T) {
	var zero composetypes.DeployConfig
	if !isZeroDeployConfig(zero) {
		t.Fatalf("zero value should be detected as empty")
	}
	r := uint64(1)
	if isZeroDeployConfig(composetypes.DeployConfig{Replicas: &r}) {
		t.Fatalf("Replicas should count as non-empty")
	}
	if isZeroDeployConfig(composetypes.DeployConfig{Mode: "global"}) {
		t.Fatalf("Mode should count as non-empty")
	}
	if isZeroDeployConfig(composetypes.DeployConfig{EndpointMode: "vip"}) {
		t.Fatalf("EndpointMode should count as non-empty")
	}
}
