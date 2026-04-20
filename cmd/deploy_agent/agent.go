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
// v3 simplified:
//   - `--help` / `-h` → prints a short banner and exits 0.
//   - missing EnvJobPath → writes a one-line error to stderr and exits 2.
//   - otherwise → loads job.json, seeds state.json, runs the deploy
//     pipeline, writes the terminal state, releases the lock, rotates
//     history, and exits 0 on success / 3 on any non-success terminal.
//
// No HTTP progress server. No recovery UI. No allow-list. The main
// Swirl UI tracks progress by polling `/api/system/mode` (primary
// comes back up) and reading state.json via `/api/self-deploy/status`
// once the new primary is alive. If a deploy ends in `recovery` or
// `failed` the operator runs the manual-rollback snippet documented
// in `docs/self-deploy.md`.
//
// Signal handling: SIGINT/SIGTERM cancels the deploy context, flushes
// state, releases the lock, and exits 130. The rename-based safety
// pivot means a hard kill mid-deploy never leaves the operator
// container-less: the previous container still exists (possibly
// renamed back).
func Run() int {
	return runWithIO(os.Args[2:], os.Stdout, os.Stderr)
}

// runWithIO is the testable inner loop.
func runWithIO(args []string, stdout, stderr io.Writer) int {
	flagSet := flag.NewFlagSet("swirl deploy-agent", flag.ContinueOnError)
	flagSet.SetOutput(stderr)
	help := flagSet.Bool("help", false, "print this message and exit")
	flagSet.BoolVar(help, "h", false, "print this message and exit (short)")

	if err := flagSet.Parse(args); err != nil {
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
		}
	}()

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

	select {
	case <-interrupted:
		sw.Close()
		logger.Warn("deploy-agent: exiting with code 130 after signal")
		return 130
	default:
	}

	sw.Close()

	if runErr == nil {
		logger.Infof("deploy-agent: job %s completed successfully", job.ID)
		return ExitOK
	}
	logger.Errorf("deploy-agent: job %s did not succeed: %v", job.ID, runErr)
	fmt.Fprintf(stderr, "swirl deploy-agent: %v\n", runErr)
	return ExitDeployErr
}

// loadJob reads and unmarshals the JSON job descriptor from disk.
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
// (FIFO by directory mtime). Non-fatal — failure here does not change
// the deploy outcome.
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

State directory default: ` + DefaultStateDir + `

This sidekick is meant to be spawned by the primary Swirl container
and is NOT a user-facing command. Running it manually is safe but
useful only for diagnostics.
`
	fmt.Fprint(w, banner)
}
