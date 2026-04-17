package misc

import (
	"os"
	"path/filepath"
	"testing"
)

// TestParseCgroupV1 — the classic format found on cgroups v1 kernels.
func TestParseCgroupV1(t *testing.T) {
	content := "12:pids:/docker/abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789\n" +
		"11:memory:/docker/abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789\n"
	id, ok := parseFromString(t, content)
	if !ok {
		t.Fatalf("expected parse success on cgroups v1 format")
	}
	if id != "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789" {
		t.Fatalf("unexpected id: %s", id)
	}
}

// TestParseCgroupV2Scope — systemd-style v2 path with a `.scope` suffix.
func TestParseCgroupV2Scope(t *testing.T) {
	content := "0::/system.slice/docker-1111111111111111111111111111111111111111111111111111111111111111.scope\n"
	id, ok := parseFromString(t, content)
	if !ok {
		t.Fatalf("expected parse success on cgroups v2 scope format")
	}
	if id != "1111111111111111111111111111111111111111111111111111111111111111" {
		t.Fatalf("unexpected id: %s", id)
	}
}

// TestParseCgroupNoMatch — no container ID present; function must return
// (_, false) rather than produce a false positive.
func TestParseCgroupNoMatch(t *testing.T) {
	content := "0::/\n"
	if _, ok := parseFromString(t, content); ok {
		t.Fatalf("expected no match for non-docker cgroup")
	}
}

// TestParseCgroupMissingFile — missing file is a silent fail (the check
// is best-effort; callers MUST treat !ok as "no self-protection").
func TestParseCgroupMissingFile(t *testing.T) {
	if _, ok := parseCgroupForSelfID("/does/not/exist"); ok {
		t.Fatalf("missing file should return ok=false")
	}
}

// TestContainerIDMatchesSelfFullVsShort — Docker hostnames are the first
// 12 hex chars of the container ID, so both directions of a prefix match
// must work when the operator relies on the hostname fallback.
func TestContainerIDMatchesSelfFullVsShort(t *testing.T) {
	_ = os.Setenv("SWIRL_CONTAINER_ID", "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789")
	defer os.Unsetenv("SWIRL_CONTAINER_ID")
	if !ContainerIDMatchesSelf("abcdef012345") {
		t.Fatalf("short prefix of self should match")
	}
	if ContainerIDMatchesSelf("deadbeefcafe") {
		t.Fatalf("unrelated id should not match")
	}
}

func parseFromString(t *testing.T, content string) (string, bool) {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "cgroup")
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return parseCgroupForSelfID(p)
}
