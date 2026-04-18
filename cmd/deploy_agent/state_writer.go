package deploy_agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/cuigh/swirl/biz"
)

// logRingCapacity bounds the in-memory log buffer persisted to
// state.json. 500 lines is enough context for post-mortem of the
// typical deploy without inflating the state file past a few dozen KB.
const logRingCapacity = 500

// stateFlushInterval is the max latency between a Logf/SetPhase call
// and its reflection in state.json. Kept short (2s) so the UI's status
// poll picks up sidekick progress in near real-time.
const stateFlushInterval = 2 * time.Second

// stateWriter coordinates the sidekick's state.json updates with its
// in-memory ring buffer of log lines. All public methods are safe to
// call concurrently — the sidekick runs lifecycle logic on the main
// goroutine but may emit logs from signal handlers and the HTTP health
// check loop.
//
// Design choices:
//   - mutex guards the struct (not atomic.Value) because SetPhase
//     mutates multiple fields and Logf appends to the ring slice.
//   - flush writes temp file + rename. Inherits the same atomic
//     semantics used by biz.writeSelfDeployState.
//   - Close() force-flushes AND stops the periodic flusher goroutine
//     so the sidekick can exit cleanly without losing the final
//     Succeed/Fail state.
type stateWriter struct {
	path string

	mu     sync.Mutex
	st     biz.SelfDeployState
	ring   []string // ring buffer of recent log lines
	next   int      // ring insertion index
	filled bool     // true once the ring has wrapped at least once
	dirty  bool     // something changed since the last flush

	stopCh chan struct{}
	doneCh chan struct{}
}

// newStateWriter seeds the writer with initial (typically the job's
// Pending state written by the primary). The initial state is flushed
// immediately so an observer reading state.json right after the
// sidekick spawns sees a coherent snapshot with the new Phase.
//
// A background goroutine flushes the state every stateFlushInterval
// whenever a change has been signalled via markDirty.
func newStateWriter(path string, initial *biz.SelfDeployState) (*stateWriter, error) {
	if path == "" {
		return nil, errors.New("deploy-agent: stateWriter path cannot be empty")
	}
	sw := &stateWriter{
		path:   path,
		ring:   make([]string, logRingCapacity),
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
	}
	if initial != nil {
		sw.st = *initial
	}
	if sw.st.StartedAt.IsZero() {
		sw.st.StartedAt = time.Now().UTC()
	}
	sw.dirty = true
	if err := sw.flushLocked(); err != nil {
		// Not fatal per se, but we want the operator to know early if
		// the volume is read-only or out of space.
		return nil, fmt.Errorf("deploy-agent: initial state flush: %w", err)
	}

	go sw.flusherLoop()
	return sw, nil
}

// SetPhase updates the lifecycle phase and bumps the dirty flag.
// No-op (but still marks dirty) if the phase is the same — keeps the
// caller simple.
func (sw *stateWriter) SetPhase(phase string) {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	sw.st.Phase = phase
	sw.dirty = true
	sw.appendLogLocked(fmt.Sprintf("phase=%s", phase))
}

// Logf appends a timestamped line to the ring buffer. Lines are
// prefixed with RFC3339 so correlating with `docker logs` on the
// sidekick container is trivial.
func (sw *stateWriter) Logf(format string, args ...any) {
	line := fmt.Sprintf(format, args...)
	sw.mu.Lock()
	defer sw.mu.Unlock()
	sw.appendLogLocked(line)
	sw.dirty = true
}

// appendLogLocked writes a timestamped line into the ring. The caller
// MUST hold sw.mu.
func (sw *stateWriter) appendLogLocked(line string) {
	stamp := time.Now().UTC().Format(time.RFC3339)
	entry := stamp + " " + line
	sw.ring[sw.next] = entry
	sw.next = (sw.next + 1) % logRingCapacity
	if sw.next == 0 {
		sw.filled = true
	}
}

// Fail records a terminal failure state with the supplied error.
// Always flushes synchronously — if the process is about to die we
// want the error visible to the UI.
func (sw *stateWriter) Fail(err error) {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	sw.st.Phase = biz.SelfDeployPhaseFailed
	sw.st.FinishedAt = time.Now().UTC()
	if err != nil {
		sw.st.Error = err.Error()
		sw.appendLogLocked("FAIL: " + err.Error())
	}
	sw.dirty = true
	_ = sw.flushLocked()
}

// Succeed records a terminal success state and flushes immediately.
func (sw *stateWriter) Succeed() {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	sw.st.Phase = biz.SelfDeployPhaseSuccess
	sw.st.FinishedAt = time.Now().UTC()
	sw.st.Error = ""
	sw.appendLogLocked("SUCCESS: deploy complete")
	sw.dirty = true
	_ = sw.flushLocked()
}

// MarkRolledBack records the `rolled_back` terminal state. Separate
// from Fail so the UI can distinguish "new version rejected, running
// on old" from "deploy outright failed".
func (sw *stateWriter) MarkRolledBack(reason error) {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	sw.st.Phase = biz.SelfDeployPhaseRolledBack
	sw.st.FinishedAt = time.Now().UTC()
	if reason != nil {
		sw.st.Error = "rolled back: " + reason.Error()
		sw.appendLogLocked("ROLLED BACK: " + reason.Error())
	} else {
		sw.appendLogLocked("ROLLED BACK: deploy recovered to previous version")
	}
	sw.dirty = true
	_ = sw.flushLocked()
}

// MarkRecovery records the `recovery` phase the sidekick enters when a
// failed deploy has NOT auto-rolled-back. Phase 6 will replace the
// exit-1 fallback with a real recovery server; today the state tells
// the UI which phase to render.
func (sw *stateWriter) MarkRecovery(reason error) {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	sw.st.Phase = biz.SelfDeployPhaseRecovery
	if reason != nil {
		sw.st.Error = reason.Error()
		sw.appendLogLocked("RECOVERY MODE: " + reason.Error())
	} else {
		sw.appendLogLocked("RECOVERY MODE entered")
	}
	sw.dirty = true
	_ = sw.flushLocked()
}

// Close stops the background flusher and performs one final flush so
// terminal state is durable on disk.
func (sw *stateWriter) Close() {
	select {
	case <-sw.stopCh:
		// already closed
		return
	default:
	}
	close(sw.stopCh)
	<-sw.doneCh
	sw.mu.Lock()
	sw.dirty = true
	_ = sw.flushLocked()
	sw.mu.Unlock()
}

// flusherLoop runs on its own goroutine and writes state.json every
// stateFlushInterval as long as something has changed. Exits when
// stopCh is closed.
func (sw *stateWriter) flusherLoop() {
	defer close(sw.doneCh)
	ticker := time.NewTicker(stateFlushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-sw.stopCh:
			return
		case <-ticker.C:
			sw.mu.Lock()
			if sw.dirty {
				_ = sw.flushLocked()
			}
			sw.mu.Unlock()
		}
	}
}

// flushLocked materialises the ring buffer into st.LogTail and writes
// state.json atomically. Caller MUST hold sw.mu.
func (sw *stateWriter) flushLocked() error {
	sw.st.LogTail = sw.snapshotRingLocked()
	if sw.st.JobID == "" {
		// Nothing identifies this state yet — skip (ring still in
		// memory, will flush on the next SetPhase that carries info).
		sw.dirty = false
		return nil
	}
	buf, err := json.MarshalIndent(&sw.st, "", "  ")
	if err != nil {
		return fmt.Errorf("deploy-agent: marshal state: %w", err)
	}
	if err := atomicWriteFile(sw.path, buf, 0o600); err != nil {
		return err
	}
	sw.dirty = false
	return nil
}

// snapshotRingLocked copies the ring into a plain slice in insertion
// order. The ring is indexed by sw.next (the *next* write slot), so
// the oldest entry is at sw.next when the ring has wrapped.
func (sw *stateWriter) snapshotRingLocked() []string {
	if !sw.filled {
		// Ring hasn't wrapped yet — entries are in [0, next).
		if sw.next == 0 {
			return nil
		}
		out := make([]string, sw.next)
		copy(out, sw.ring[:sw.next])
		return out
	}
	out := make([]string, logRingCapacity)
	copy(out, sw.ring[sw.next:])
	copy(out[logRingCapacity-sw.next:], sw.ring[:sw.next])
	return out
}

// atomicWriteFile mirrors biz.atomicWriteFile — kept private to this
// package so the sidekick doesn't depend on a biz helper that is
// intentionally unexported. Behaviour is identical: temp file in the
// same directory + rename, mode set BEFORE close.
func atomicWriteFile(path string, b []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("deploy-agent: mkdir %s: %w", dir, err)
	}
	f, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("deploy-agent: create temp: %w", err)
	}
	tmp := f.Name()
	if _, err := f.Write(b); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("deploy-agent: write temp: %w", err)
	}
	if err := f.Chmod(mode); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("deploy-agent: chmod temp: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("deploy-agent: close temp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("deploy-agent: rename temp: %w", err)
	}
	return nil
}
