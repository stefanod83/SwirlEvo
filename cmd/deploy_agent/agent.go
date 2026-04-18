package deploy_agent

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/cuigh/auxo/log"
	"github.com/cuigh/swirl/biz"
)

// Run is the sidekick entry point. It is invoked by main.go when the
// first CLI argument is `deploy-agent`. Run returns an exit code that
// main passes straight to os.Exit.
//
// Phase 4 behaviour:
//   - `--help` / `-h` → prints a short banner and exits 0.
//   - missing EnvJobPath → writes a one-line error to stderr and exits 2.
//   - otherwise → loads job.json, seeds state.json, spawns the
//     runDeploy pipeline, and exits with 0 on success / 3 on recovery-
//     or-failure terminal states.
//
// Signal handling: SIGINT/SIGTERM cancels the deploy context, marks
// the state as failed, releases the lock, and exits 130. The sidekick
// is designed to be killable — the rename-based safety pivot means a
// hard kill mid-deploy never leaves the operator container-less: the
// previous container still exists (possibly renamed back).
func Run() int {
	return runWithIO(os.Args[2:], os.Stdout, os.Stderr)
}

// runWithIO is the testable inner loop. It takes the argument slice
// that follows the `deploy-agent` subcommand (i.e. os.Args[2:]) plus
// explicit stdout/stderr writers so the behaviour is observable from
// unit tests without touching the process globals.
func runWithIO(args []string, stdout, stderr io.Writer) int {
	flagSet := flag.NewFlagSet("swirl deploy-agent", flag.ContinueOnError)
	flagSet.SetOutput(stderr)
	help := flagSet.Bool("help", false, "print this message and exit")
	flagSet.BoolVar(help, "h", false, "print this message and exit (short)")

	if err := flagSet.Parse(args); err != nil {
		// flag.ContinueOnError already wrote the usage line to stderr.
		return ExitUsage
	}

	if *help {
		printBanner(stdout)
		return ExitOK
	}

	jobPath := os.Getenv(EnvJobPath)
	if jobPath == "" {
		fmt.Fprintf(stderr, "swirl deploy-agent: %s is not set; nothing to do\n", EnvJobPath)
		return ExitUsage
	}

	job, err := loadJob(jobPath)
	if err != nil {
		fmt.Fprintf(stderr, "swirl deploy-agent: cannot read job file %q: %v\n", jobPath, err)
		return ExitUsage
	}

	// Derive the state file path from the job file's directory so the
	// primary and the sidekick stay in lockstep without hardcoding the
	// state filename twice.
	stateDir := filepath.Dir(jobPath)
	statePath := filepath.Join(stateDir, DefaultStateFile)
	lockPath := filepath.Join(stateDir, DefaultLockFile)

	logger := log.Get("deploy-agent")
	logger.Infof("deploy-agent starting: job=%s id=%s target=%s", jobPath, job.ID, job.TargetImageTag)

	initState := &biz.SelfDeployState{
		JobID:     job.ID,
		Phase:     biz.SelfDeployPhasePending,
		StartedAt: time.Now().UTC(),
	}
	sw, err := newStateWriter(statePath, initState)
	if err != nil {
		fmt.Fprintf(stderr, "swirl deploy-agent: cannot open state file: %v\n", err)
		return ExitRuntime
	}

	// Install signal handlers BEFORE runDeploy so SIGTERM during the
	// very first Docker call gets a graceful shutdown (lock released,
	// state flushed) instead of a naked exit.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	interrupted := make(chan struct{})
	go func() {
		select {
		case s := <-sigCh:
			logger.Warnf("deploy-agent: received %s, cancelling deploy", s)
			sw.Logf("interrupted by signal %s", s)
			cancel()
			close(interrupted)
		case <-ctx.Done():
			// normal completion; unblock the goroutine
		}
	}()

	// Always-on progress server: start the HTTP server at the BEGINNING
	// of the deploy so the main Swirl UI can embed it via iframe and
	// show the operator real-time logs + phase. Previously this server
	// spawned ONLY on failure; keeping the same HTTP surface alive for
	// the happy path means operators don't stare at a banner while the
	// deploy is running.
	//
	// If startProgressServer fails (port busy, docker unreachable from
	// sidekick's perspective, …) we log a warning and keep going — the
	// deploy itself does NOT depend on the UI being up.
	port := selectRecoveryPort(job)
	allow := selectRecoveryAllow(job)
	trustProxy := selectRecoveryTrustProxy()
	progress, progressErr := startProgressServer(ctx, job, sw, port, allow, trustProxy)
	if progressErr != nil {
		logger.Warnf("deploy-agent: could not start progress server (deploy continues): %v", progressErr)
		sw.Logf("warning: progress UI unavailable: %v", progressErr)
	}

	runErr := runDeploy(ctx, job, sw)

	// Release the filesystem lock unconditionally. The sidekick owns
	// the lock lifetime once the primary has spawned it.
	if err := os.Remove(lockPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		logger.Warnf("deploy-agent: could not remove lock file %q: %v", lockPath, err)
	}

	// Rotate history (best-effort). One directory per job id, FIFO
	// purge over 20.
	if err := rotateHistory(stateDir, job, sw); err != nil {
		logger.Warnf("deploy-agent: history rotation failed: %v", err)
	}

	// Check interrupted BEFORE deciding the exit code so a
	// cancel-triggered failure is distinguishable from a deploy that
	// legitimately failed.
	select {
	case <-interrupted:
		if progress != nil {
			progress.shutdown()
		}
		sw.Close()
		logger.Warn("deploy-agent: exiting with code 130 after signal")
		return 130
	default:
	}

	if runErr == nil {
		// Happy path — new Swirl is up. Shut the progress UI down so
		// the port is free (and the JS in the iframe sees the server
		// going away as a success signal via its polling error path).
		if progress != nil {
			sw.Logf("deploy succeeded; shutting down progress UI")
			progress.shutdown()
		}
		sw.Close()
		logger.Infof("deploy-agent: job %s completed successfully", job.ID)
		return ExitOK
	}

	// Recovery / rolled-back / failed — the progress server (started
	// above) remains alive and seamlessly serves the recovery UI: same
	// HTTP handlers, same state.json. The JS embedded in index.html
	// flips button visibility based on state.phase, so no server-side
	// mode switch is required.
	logger.Errorf("deploy-agent: job %s did not succeed: %v", job.ID, runErr)

	// Already-rolled-back is a non-interactive terminal state: the
	// previous Swirl is healthy again, there is nothing for the
	// operator to do from the recovery UI. Exit straight away.
	finalPhase := ""
	sw.mu.Lock()
	finalPhase = sw.st.Phase
	sw.mu.Unlock()
	if finalPhase == biz.SelfDeployPhaseRolledBack {
		if progress != nil {
			// On rollback success the main UI is back up — shut the
			// progress server so the previous Swirl can reclaim any
			// shared port and the operator sees the normal UI again.
			sw.Logf("rollback succeeded; shutting down progress UI")
			progress.shutdown()
		}
		sw.Close()
		logger.Warnf("deploy-agent: job %s rolled back to previous version; exiting", job.ID)
		return ExitDeployErr
	}

	// Failure without rollback (or rollback failed): keep the server
	// alive and wait for operator action. awaitRecovery blocks until
	// Retry/Rollback succeeds OR ctx is cancelled.
	if progress == nil {
		// Couldn't even start the server earlier — nothing else to do.
		sw.Close()
		logger.Errorf("deploy-agent: recovery UI unavailable; exiting with failure")
		fmt.Fprintf(stderr, "swirl deploy-agent: recovery UI unavailable\n")
		return ExitDeployErr
	}

	sw.Logf("entering recovery mode on port %d (allow-list: %s)", port, strings.Join(allow, ","))
	recErr := progress.awaitRecovery(ctx)

	// Re-check interrupted now — awaitRecovery blocks until either
	// ctx cancel, success, or a server error. A SIGINT during the
	// recovery server should surface as 130 the same way as during
	// runDeploy.
	select {
	case <-interrupted:
		sw.Close()
		logger.Warn("deploy-agent: exiting with code 130 after signal during recovery")
		return 130
	default:
	}

	sw.Close()

	if recErr == nil {
		logger.Infof("deploy-agent: job %s recovered via operator action", job.ID)
		return ExitOK
	}
	logger.Errorf("deploy-agent: recovery ended with error: %v", recErr)
	fmt.Fprintf(stderr, "swirl deploy-agent: recovery exited: %v\n", recErr)
	return ExitDeployErr
}

// loadJob reads and unmarshals the JSON job descriptor from disk.
// Surfaces clear errors so the operator can tell whether the file is
// missing, empty, or malformed.
func loadJob(path string) (*biz.SelfDeployJob, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(b) == 0 {
		return nil, errors.New("job file is empty")
	}
	var j biz.SelfDeployJob
	if err := json.Unmarshal(b, &j); err != nil {
		return nil, fmt.Errorf("parse job JSON: %w", err)
	}
	if strings.TrimSpace(j.ID) == "" {
		return nil, errors.New("job file missing ID")
	}
	return &j, nil
}

// rotateHistory copies the current job.json + state.json into a
// per-job directory under history/ so past deploys remain auditable
// after the next job overwrites them. Caps the history at 20 entries
// (FIFO by directory mtime).
//
// Non-fatal — failure here does not change the deploy outcome.
func rotateHistory(stateDir string, job *biz.SelfDeployJob, sw *stateWriter) error {
	if job == nil || job.ID == "" {
		return errors.New("nil job or empty id")
	}
	historyRoot := filepath.Join(stateDir, "history")
	if err := os.MkdirAll(historyRoot, 0o700); err != nil {
		return fmt.Errorf("mkdir history: %w", err)
	}
	target := filepath.Join(historyRoot, job.ID)
	if err := os.MkdirAll(target, 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", target, err)
	}

	// Flush state so the copy reflects the final phase.
	sw.mu.Lock()
	_ = sw.flushLocked()
	sw.mu.Unlock()

	if err := copyFileIfExists(filepath.Join(stateDir, DefaultJobFile), filepath.Join(target, DefaultJobFile)); err != nil {
		return err
	}
	if err := copyFileIfExists(filepath.Join(stateDir, DefaultStateFile), filepath.Join(target, DefaultStateFile)); err != nil {
		return err
	}
	if err := pruneHistory(historyRoot, 20); err != nil {
		return err
	}
	return nil
}

// copyFileIfExists is a small helper that copies src to dst preserving
// the 0600 mode; missing src is not an error (so rotation on a fresh
// install is safe).
func copyFileIfExists(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return nil
}

// pruneHistory enforces the FIFO cap on history/ by deleting the
// oldest directories once the count exceeds `keep`.
func pruneHistory(root string, keep int) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	type entryInfo struct {
		name string
		mod  time.Time
	}
	var dirs []entryInfo
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		dirs = append(dirs, entryInfo{name: e.Name(), mod: info.ModTime()})
	}
	if len(dirs) <= keep {
		return nil
	}
	sort.Slice(dirs, func(i, j int) bool { return dirs[i].mod.Before(dirs[j].mod) })
	toDelete := dirs[:len(dirs)-keep]
	for _, d := range toDelete {
		_ = os.RemoveAll(filepath.Join(root, d.name))
	}
	return nil
}

func printBanner(w io.Writer) {
	const banner = `swirl deploy-agent — self-deploy sidekick

Usage:
  swirl deploy-agent            run the sidekick against the job file
                                pointed to by ` + EnvJobPath + `
  swirl deploy-agent --help     print this message

Required environment:
  ` + EnvJobPath + `   path to the JSON job descriptor

Optional environment:
  ` + EnvRecoveryPort + `       recovery UI listen port override
  ` + EnvRecoveryAllow + `      recovery UI CIDR allow-list override
  ` + EnvRecoveryTrustProxy + `  honour X-Forwarded-For when "1"/"true"

State directory default: ` + DefaultStateDir + `

This sidekick is meant to be spawned by the primary Swirl container
and is NOT a user-facing command. Running it manually is safe but
useful only for diagnostics.
`
	fmt.Fprint(w, banner)
}
