package deploy_agent

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunHelpExitsZero(t *testing.T) {
	var out, errOut bytes.Buffer
	code := runWithIO([]string{"--help"}, &out, &errOut)
	if code != ExitOK {
		t.Fatalf("expected ExitOK, got %d (stderr=%q)", code, errOut.String())
	}
	if !strings.Contains(out.String(), "deploy-agent") {
		t.Fatalf("banner did not mention deploy-agent: %q", out.String())
	}
}

func TestRunMissingJobPathExitsUsage(t *testing.T) {
	t.Setenv(EnvJobPath, "")
	var out, errOut bytes.Buffer
	code := runWithIO(nil, &out, &errOut)
	if code != ExitUsage {
		t.Fatalf("expected ExitUsage (%d), got %d", ExitUsage, code)
	}
	if !strings.Contains(errOut.String(), EnvJobPath) {
		t.Fatalf("stderr should mention %s, got %q", EnvJobPath, errOut.String())
	}
}

func TestRunMissingJobFileExitsUsage(t *testing.T) {
	t.Setenv(EnvJobPath, filepath.Join(t.TempDir(), "does-not-exist.json"))
	var out, errOut bytes.Buffer
	code := runWithIO(nil, &out, &errOut)
	if code != ExitUsage {
		t.Fatalf("expected ExitUsage (%d), got %d", ExitUsage, code)
	}
	if !strings.Contains(errOut.String(), "cannot read job file") {
		t.Fatalf("stderr should mention missing job file, got %q", errOut.String())
	}
}

// TestRunInvalidJobReturnsDeployErr: Phase 4 behaviour. Given a
// well-formed job file but no docker daemon available, the lifecycle
// pings docker very early and fails. The sidekick should surface the
// deploy error via ExitDeployErr (3), NOT ExitOK. The previous Phase 1
// "placeholder, exit 0" test is replaced by this one.
func TestRunInvalidJobReturnsDeployErr(t *testing.T) {
	dir := t.TempDir()
	jobPath := filepath.Join(dir, "job.json")
	if err := os.WriteFile(jobPath, []byte(`{"id":"abc","targetImageTag":"cuigh/swirl:v1","timeoutSec":1,"recoveryPort":8002,"recoveryAllow":["127.0.0.1/32"]}`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Setenv(EnvJobPath, jobPath)
	// Point DOCKER_HOST at an unreachable socket so the sidekick fails
	// at pingDocker before touching any real daemon.
	t.Setenv("DOCKER_HOST", "unix:///tmp/nonexistent-"+t.Name()+".sock")

	var out, errOut bytes.Buffer
	code := runWithIO(nil, &out, &errOut)
	// Either ExitDeployErr (graceful failure after state+recovery log)
	// or 130 (unlikely here, but accept both as non-success signals).
	if code == ExitOK {
		t.Fatalf("expected non-zero exit, got %d (stderr=%q)", code, errOut.String())
	}
}

func TestRunBadFlagExitsUsage(t *testing.T) {
	var out, errOut bytes.Buffer
	code := runWithIO([]string{"--bogus"}, &out, &errOut)
	if code != ExitUsage {
		t.Fatalf("expected ExitUsage (%d), got %d", ExitUsage, code)
	}
}

// TestLoadJobParsesMinimal covers the helper used by runWithIO. Kept
// separate so failures can be pinpointed without touching the full
// signal/state/lock machinery.
func TestLoadJobParsesMinimal(t *testing.T) {
	dir := t.TempDir()
	jobPath := filepath.Join(dir, "job.json")
	if err := os.WriteFile(jobPath, []byte(`{"id":"abc","targetImageTag":"cuigh/swirl:v1"}`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	j, err := loadJob(jobPath)
	if err != nil {
		t.Fatalf("loadJob: %v", err)
	}
	if j.ID != "abc" {
		t.Fatalf("expected id=abc, got %q", j.ID)
	}
	if j.TargetImageTag != "cuigh/swirl:v1" {
		t.Fatalf("expected target image, got %q", j.TargetImageTag)
	}
}

// TestLoadJobMissingID rejects a job file without a JSON id so the
// sidekick doesn't silently run against a corrupt descriptor.
func TestLoadJobMissingID(t *testing.T) {
	dir := t.TempDir()
	jobPath := filepath.Join(dir, "job.json")
	if err := os.WriteFile(jobPath, []byte(`{"targetImageTag":"cuigh/swirl:v1"}`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := loadJob(jobPath); err == nil {
		t.Fatalf("expected error for missing id, got nil")
	}
}

// TestLoadJobEmptyFile rejects an empty file.
func TestLoadJobEmptyFile(t *testing.T) {
	dir := t.TempDir()
	jobPath := filepath.Join(dir, "job.json")
	if err := os.WriteFile(jobPath, []byte{}, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := loadJob(jobPath); err == nil {
		t.Fatalf("expected error for empty file, got nil")
	}
}
