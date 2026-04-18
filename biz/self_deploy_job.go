package biz

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// selfDeployStateDir is the filesystem path where the primary Swirl
// writes job + state descriptors consumed by the sidekick. Lives on
// the same persistent volume as the BoltDB data so self-deploy state
// survives a container swap. Exported as a var (not a const) so tests
// can swap the directory without touching the global filesystem.
var selfDeployStateDir = "/data/self-deploy"

const (
	selfDeployJobFile   = "job.json"
	selfDeployStateFile = "state.json"
	selfDeployLockFile  = ".lock"
)

// Phase names emitted by the sidekick and surfaced in SelfDeployStatus.
// Kept as named constants (not a typed enum) because the sidekick is a
// separate process and both sides round-trip the raw string through
// state.json — introducing a custom type on one side only would be
// symmetry noise without safety gain.
const (
	SelfDeployPhasePending     = "pending"
	SelfDeployPhaseStopping    = "stopping"
	SelfDeployPhasePulling     = "pulling"
	SelfDeployPhaseStarting    = "starting"
	SelfDeployPhaseHealthCheck = "health_check"
	SelfDeployPhaseSuccess     = "success"
	SelfDeployPhaseFailed      = "failed"
	SelfDeployPhaseRecovery    = "recovery"
	SelfDeployPhaseRolledBack  = "rolled_back"
)

// SelfDeployJob is the descriptor the primary Swirl writes to disk and
// the sidekick reads. Field names are stable across the primary/sidekick
// boundary — renaming a JSON tag is a breaking change for any sidekick
// already in flight on upgrade.
//
// Deviation from the plan: CreatedAt is serialised as RFC3339, not as a
// Go time.Time struct, so the sidekick (which is built from the same
// module) does not need to vendor a duplicate type and the JSON is
// readable by operators during diagnostics.
type SelfDeployJob struct {
	ID               string                 `json:"id"` // uuid-ish short hex (createId)
	CreatedAt        time.Time              `json:"createdAt"`
	CreatedBy        string                 `json:"createdBy,omitempty"`
	ComposeYAML      string                 `json:"composeYaml"`
	Placeholders     SelfDeployPlaceholders `json:"placeholders"`
	PreviousImageTag string                 `json:"previousImageTag,omitempty"`
	TargetImageTag   string                 `json:"targetImageTag"`
	PrimaryContainer string                 `json:"primaryContainer"`
	RecoveryPort     int                    `json:"recoveryPort"`
	RecoveryAllow    []string               `json:"recoveryAllow"`
	TimeoutSec       int                    `json:"timeoutSec"`
	AutoRollback     bool                   `json:"autoRollback"`
}

// SelfDeployState is the mutable record updated by the sidekick as the
// lifecycle progresses. Written atomically (temp + rename) so readers
// that happen to hit the file mid-write never observe a partial JSON.
//
// EventPublished is the idempotency flag used by the main Swirl's Status
// endpoint to decide whether the success/failure audit event for this
// terminal phase has already been written. The sidekick does NOT have
// access to the DB — it only writes state.json — so the audit event
// cannot be emitted sidekick-side. Instead, the main Swirl's Status
// handler reads state.json on every poll and, when it observes a
// terminal phase (success / failed / rolled_back / recovery) with
// EventPublished=false, it emits the corresponding CreateSelfDeploy
// event and flips the flag to true by rewriting state.json in place.
// This keeps the audit trail complete while the sidekick stays
// DB-ignorant.
type SelfDeployState struct {
	JobID          string    `json:"jobId"`
	Phase          string    `json:"phase"` // see SelfDeployPhase*
	StartedAt      time.Time `json:"startedAt"`
	FinishedAt     time.Time `json:"finishedAt,omitempty"`
	Error          string    `json:"error,omitempty"`
	LogTail        []string  `json:"logTail,omitempty"`
	EventPublished bool      `json:"eventPublished,omitempty"`
}

// errLockHeld is returned by acquireSelfDeployLock when a deploy is
// already in flight. Not a sentinel operators need to match against —
// the biz layer wraps it with misc.Error for the coded response.
var errLockHeld = errors.New("self-deploy: lock file already held")

// selfDeployPaths returns the absolute paths used for job / state / lock
// files under the currently-configured selfDeployStateDir. Factored out
// so tests that swap selfDeployStateDir exercise the same path layout.
func selfDeployPaths() (jobPath, statePath, lockPath string) {
	return filepath.Join(selfDeployStateDir, selfDeployJobFile),
		filepath.Join(selfDeployStateDir, selfDeployStateFile),
		filepath.Join(selfDeployStateDir, selfDeployLockFile)
}

// ensureSelfDeployDir creates the state directory with tight (0700)
// permissions. Idempotent — running on a pre-existing directory is a
// no-op. Errors surface as-is so callers can distinguish "path exists
// but is a file" from "permission denied".
func ensureSelfDeployDir() error {
	if err := os.MkdirAll(selfDeployStateDir, 0o700); err != nil {
		return fmt.Errorf("self-deploy: mkdir %s: %w", selfDeployStateDir, err)
	}
	// Re-chmod in case the directory pre-existed with looser perms.
	if err := os.Chmod(selfDeployStateDir, 0o700); err != nil && !errors.Is(err, fs.ErrPermission) {
		return fmt.Errorf("self-deploy: chmod %s: %w", selfDeployStateDir, err)
	}
	return nil
}

// writeSelfDeployJob serialises the job descriptor to JSON and writes it
// atomically (temp + rename) at mode 0600 under selfDeployStateDir. The
// directory is created if missing. Returns the absolute path the file
// was written to — the caller passes this to the sidekick via
// SWIRL_SELF_DEPLOY_JOB.
func writeSelfDeployJob(job *SelfDeployJob) (string, error) {
	if job == nil {
		return "", errors.New("self-deploy: nil job")
	}
	if err := ensureSelfDeployDir(); err != nil {
		return "", err
	}
	jobPath, _, _ := selfDeployPaths()
	buf, err := json.MarshalIndent(job, "", "  ")
	if err != nil {
		return "", fmt.Errorf("self-deploy: marshal job: %w", err)
	}
	if err := atomicWriteFile(jobPath, buf, 0o600); err != nil {
		return "", err
	}
	return jobPath, nil
}

// writeSelfDeployState mirrors writeSelfDeployJob for the state file.
func writeSelfDeployState(st *SelfDeployState) error {
	if st == nil {
		return errors.New("self-deploy: nil state")
	}
	if err := ensureSelfDeployDir(); err != nil {
		return err
	}
	_, statePath, _ := selfDeployPaths()
	buf, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("self-deploy: marshal state: %w", err)
	}
	return atomicWriteFile(statePath, buf, 0o600)
}

// readSelfDeployJob reads the persisted job.json into a typed
// SelfDeployJob. Returns (nil, nil) when the file is absent — the
// caller interprets the absence as "no deploy ever triggered" and
// surfaces an idle status. Used by Status() to resolve the target
// image tag when emitting a delayed success/failure audit event.
func readSelfDeployJob() (*SelfDeployJob, error) {
	jobPath, _, _ := selfDeployPaths()
	b, err := os.ReadFile(jobPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("self-deploy: read job: %w", err)
	}
	var j SelfDeployJob
	if err := json.Unmarshal(b, &j); err != nil {
		return nil, fmt.Errorf("self-deploy: unmarshal job: %w", err)
	}
	return &j, nil
}

// readSelfDeployState reads the latest state.json. Returns (nil, nil) if
// the file is absent — callers interpret that as "no deploy has ever
// happened on this volume" and surface an idle status to the UI.
func readSelfDeployState() (*SelfDeployState, error) {
	_, statePath, _ := selfDeployPaths()
	b, err := os.ReadFile(statePath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("self-deploy: read state: %w", err)
	}
	var st SelfDeployState
	if err := json.Unmarshal(b, &st); err != nil {
		return nil, fmt.Errorf("self-deploy: unmarshal state: %w", err)
	}
	return &st, nil
}

// acquireSelfDeployLock creates the lock file with O_CREATE|O_EXCL. If
// the file already exists the function returns errLockHeld so the
// caller can surface a "deploy in progress" error. The returned closer
// removes the file on release — callers MUST defer it on the happy path.
//
// This is a cooperative lock, not a flock syscall: two processes
// racing still fire two O_CREATE|O_EXCL and only one wins. Good enough
// for single-primary Swirl on a shared volume; upgrade to flock when
// active-active primaries become a real concern.
func acquireSelfDeployLock() (release func(), err error) {
	if err := ensureSelfDeployDir(); err != nil {
		return nil, err
	}
	_, _, lockPath := selfDeployPaths()
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return nil, errLockHeld
		}
		return nil, fmt.Errorf("self-deploy: open lock: %w", err)
	}
	// Best-effort stamp so operators diagnosing a stuck lock know when
	// it was taken. Close errors are ignored — the lock is the file's
	// existence, not its content.
	_, _ = fmt.Fprintln(f, time.Now().UTC().Format(time.RFC3339))
	_ = f.Close()
	return func() { _ = os.Remove(lockPath) }, nil
}

// atomicWriteFile writes b to path via a temp file in the same
// directory followed by rename. The temp file inherits the requested
// mode; the rename is atomic on POSIX filesystems, so readers never
// see a half-written file.
func atomicWriteFile(path string, b []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("self-deploy: create temp: %w", err)
	}
	tmp := f.Name()
	if _, err := f.Write(b); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("self-deploy: write temp: %w", err)
	}
	if err := f.Chmod(mode); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("self-deploy: chmod temp: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("self-deploy: close temp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("self-deploy: rename temp: %w", err)
	}
	return nil
}
