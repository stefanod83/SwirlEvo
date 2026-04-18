package deploy_agent

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cuigh/auxo/log"
	"github.com/cuigh/swirl/biz"
	"github.com/cuigh/swirl/docker/compose"
	"github.com/cuigh/swirl/misc"
	"github.com/docker/docker/client"
)

// Name used to rename the primary container before deploying a new one.
// The rename-then-deploy strategy is the core safety pivot of self-deploy:
// we never remove the old container until the new one is fully healthy.
const previousContainerName = "swirl-previous"

// Deploy-time budgets. The global timeout (job.TimeoutSec) is divided
// among the sub-phases so a blown pull doesn't eat the entire budget.
const (
	pullBudgetMin    = 60 * time.Second
	pullBudgetMax    = 5 * time.Minute
	stopGraceDefault = 30 * time.Second
	rollbackHealthTimeout = 30 * time.Second
)

// runDeploy implements the full sidekick lifecycle. Sequence:
//
//  1. SetPhase(stopping) → stop primary with graceful timeout.
//  2. Rename primary to `swirl-previous` (does NOT remove).
//  3. SetPhase(pulling) → pull target image (if different).
//  4. SetPhase(starting) → deploy new stack via StandaloneEngine.Deploy.
//  5. SetPhase(health_check) → poll http://127.0.0.1:<exposePort>/api/system/mode.
//  6. On success: remove `swirl-previous`, SetPhase(success), release lock.
//  7. On failure with AutoRollback=true: rename-back, restart previous,
//     wait for short health, mark rolled_back.
//  8. On failure without AutoRollback (or rollback fails): MarkRecovery
//     and return a terminal error so Run() can decide the exit path.
//
// All pivotal actions are logged through the stateWriter so the UI
// gets live updates via state.json polling.
func runDeploy(ctx context.Context, j *biz.SelfDeployJob, sw *stateWriter) error {
	if j == nil {
		return errors.New("deploy-agent: nil job")
	}
	// Belt-and-braces: validate the job descriptor against the structural
	// invariants even though the main Swirl ran the same check before
	// writing job.json. A job crafted by hand (or by a future buggy
	// writer) that slipped past the primary's validation would otherwise
	// reach the Docker daemon and fail in a less actionable way.
	if err := biz.ValidateSelfDeployJob(j); err != nil {
		sw.Fail(err)
		return err
	}
	logger := log.Get("deploy-agent")

	// Wrap the caller's context with the job's global timeout so every
	// SDK call we make below picks up the deadline without us having
	// to thread it through explicitly.
	timeout := time.Duration(j.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = misc.SelfDeployDefaultTimeout
	}
	deployCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cli, err := newDockerClient()
	if err != nil {
		sw.Fail(err)
		return err
	}
	defer cli.Close()
	if err := pingDocker(deployCtx, cli); err != nil {
		sw.Fail(err)
		return err
	}

	// Step 1+2 — stop and rename the primary. These MUST succeed as a
	// pair; a stop without a rename would leave `swirl` as the stopped
	// container, and a subsequent Deploy would try to create a new
	// container with the same name and fail.
	sw.SetPhase(biz.SelfDeployPhaseStopping)
	sw.Logf("stopping primary container %s (graceful %s)", short(j.PrimaryContainer), stopGraceDefault)
	if err := stopPrimary(deployCtx, cli, j.PrimaryContainer, stopGraceDefault); err != nil {
		sw.Fail(err)
		return err
	}
	sw.Logf("renaming primary container %s -> %s", short(j.PrimaryContainer), previousContainerName)
	if err := renamePrimary(deployCtx, cli, j.PrimaryContainer, previousContainerName); err != nil {
		// If the rename failed, start the container back up so we don't
		// leave the operator with no Swirl at all.
		_ = startContainer(deployCtx, cli, j.PrimaryContainer)
		sw.Fail(err)
		return err
	}

	// Step 3 — pull the target image. Only if it differs from the
	// previous (same tag = no-op pull, but let's still pull to refresh
	// manifest in case a `:latest` moved underneath us).
	sw.SetPhase(biz.SelfDeployPhasePulling)
	sw.Logf("pulling image %s", j.TargetImageTag)
	pullBudget := splitPullBudget(timeout)
	if err := pullImage(deployCtx, cli, j.TargetImageTag, pullBudget); err != nil {
		return handleDeployFailure(deployCtx, cli, j, sw, logger, fmt.Errorf("image pull: %w", err))
	}

	// Step 4 — deploy the new stack via the standalone engine. We
	// deliberately pass PullImages=false because we already pulled
	// above; a second pull inside Deploy would burn time for no gain.
	sw.SetPhase(biz.SelfDeployPhaseStarting)
	sw.Logf("deploying new stack (project=%s)", misc.SelfDeployStackName)
	if err := deployNew(deployCtx, cli, misc.SelfDeployStackName, j.ComposeYAML); err != nil {
		return handleDeployFailure(deployCtx, cli, j, sw, logger, fmt.Errorf("deploy new stack: %w", err))
	}

	// Step 5 — wait for the new container to answer /api/system/mode.
	sw.SetPhase(biz.SelfDeployPhaseHealthCheck)
	healthURL := buildHealthURL(j)
	healthBudget := splitHealthBudget(timeout, pullBudget)
	sw.Logf("waiting for new Swirl to answer GET %s (budget %s)", healthURL, healthBudget)
	if err := waitHealthy(deployCtx, healthURL, healthBudget); err != nil {
		return handleDeployFailure(deployCtx, cli, j, sw, logger, fmt.Errorf("health check: %w", err))
	}

	// Step 6 — new version is healthy. Remove the previous container
	// to reclaim resources. Failures here are non-fatal: operator can
	// clean up manually, but the deploy itself succeeded.
	sw.Logf("deploy healthy; removing %s", previousContainerName)
	if err := removePrevious(deployCtx, cli); err != nil {
		sw.Logf("warning: could not remove %s: %v (leaving in place for operator cleanup)", previousContainerName, err)
	}
	sw.Succeed()
	logger.Infof("self-deploy job %s succeeded (target %s)", j.ID, j.TargetImageTag)
	return nil
}

// short trims a container ID to 12 chars (Docker convention) for nicer
// log output without losing uniqueness.
func short(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// splitPullBudget takes the global deploy timeout and returns the
// portion allocated to the image pull. Clamped between 60s and 5min
// so even very short timeouts leave the health check some headroom,
// and very long timeouts don't let a stuck pull run for hours.
func splitPullBudget(total time.Duration) time.Duration {
	budget := total / 2
	if budget < pullBudgetMin {
		budget = pullBudgetMin
	}
	if budget > pullBudgetMax {
		budget = pullBudgetMax
	}
	return budget
}

// splitHealthBudget returns the portion of the total deploy timeout
// reserved for the health check. Floor at minHealthTimeout (30s) so
// even after a slow pull the new version gets at least one probe
// cycle to answer.
func splitHealthBudget(total, pullBudget time.Duration) time.Duration {
	remaining := total - pullBudget
	if remaining < minHealthTimeout {
		return minHealthTimeout
	}
	return remaining
}

// buildHealthURL composes the health endpoint URL from the job
// placeholders. Uses 127.0.0.1 because the sidekick runs with
// NetworkMode=host and the new Swirl publishes the expose port on
// localhost.
func buildHealthURL(j *biz.SelfDeployJob) string {
	port := j.Placeholders.ExposePort
	if port == 0 {
		port = misc.SelfDeployExposePort
	}
	return fmt.Sprintf("http://127.0.0.1:%d/api/system/mode", port)
}

// stopPrimary is the public entry point for step 1.
func stopPrimary(ctx context.Context, cli *client.Client, containerID string, grace time.Duration) error {
	return stopContainer(ctx, cli, containerID, grace)
}

// renamePrimary is the public entry point for step 2.
func renamePrimary(ctx context.Context, cli *client.Client, oldName, newName string) error {
	// Ensure no stale `swirl-previous` is in the way. Leftover would
	// typically mean a previous deploy crashed before cleanup; we
	// remove it before renaming so the operator-initiated retry
	// proceeds cleanly.
	exists, err := containerExists(ctx, cli, newName)
	if err != nil {
		return fmt.Errorf("deploy-agent: probe stale %q: %w", newName, err)
	}
	if exists {
		if err := removeContainer(ctx, cli, newName); err != nil {
			return fmt.Errorf("deploy-agent: remove stale %q: %w", newName, err)
		}
	}
	return renameContainer(ctx, cli, oldName, newName)
}

// removePrevious deletes the renamed-aside container on the happy path.
// Failure is reported but NOT fatal — the deploy has already succeeded;
// the operator can remove it manually.
func removePrevious(ctx context.Context, cli *client.Client) error {
	exists, err := containerExists(ctx, cli, previousContainerName)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	return removeContainer(ctx, cli, previousContainerName)
}

// pullImage wraps pullImageRaw with its own sub-timeout and returns a
// cleaner error message. Caller controls the budget.
func pullImage(ctx context.Context, cli *client.Client, ref string, budget time.Duration) error {
	pullCtx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	return pullImageRaw(pullCtx, cli, ref)
}

// deployNew invokes the standalone compose engine. PullImages=false
// because we already pulled the target image in step 3 — a second
// pull inside the engine would just retry a success.
func deployNew(ctx context.Context, cli *client.Client, projectName, composeYAML string) error {
	engine := compose.NewStandaloneEngine(cli)
	opts := compose.DeployOptions{PullImages: false}
	if _, err := engine.DeployWithResult(ctx, projectName, composeYAML, opts); err != nil {
		return err
	}
	return nil
}

// handleDeployFailure is the shared error path for every step past
// the rename. It decides between AutoRollback and Recovery, updates
// state.json, and returns a terminal error.
//
// Critical: this function always returns a non-nil error (even when
// rollback succeeds) so Run() can choose the exit code distinctly from
// the success path. The stateWriter phase is what the UI reads to
// render the outcome.
func handleDeployFailure(ctx context.Context, cli *client.Client, j *biz.SelfDeployJob, sw *stateWriter, logger log.Logger, cause error) error {
	sw.Logf("deploy failed: %v", cause)
	logger.Errorf("self-deploy job %s failed: %v", j.ID, cause)

	if j.AutoRollback {
		if rbErr := rollback(ctx, j, sw, cli); rbErr != nil {
			sw.Logf("rollback failed: %v", rbErr)
			sw.MarkRecovery(fmt.Errorf("deploy failed and rollback failed: deploy=%v; rollback=%v", cause, rbErr))
			return fmt.Errorf("self-deploy: deploy failed (%w) and rollback failed (%v)", cause, rbErr)
		}
		// Rollback succeeded — old version is back up.
		sw.MarkRolledBack(cause)
		return fmt.Errorf("self-deploy: deploy failed, rolled back to previous: %w", cause)
	}
	sw.MarkRecovery(cause)
	return fmt.Errorf("self-deploy: deploy failed (no auto-rollback): %w", cause)
}

// rollback attempts to restore the previous container to the `swirl`
// name and bring it back up. Sequence:
//
//  1. Tear down the (likely broken) new stack by name. Best-effort —
//     a partial new stack is better off gone before we rename the
//     old one back.
//  2. Rename `swirl-previous` back to `swirl`.
//  3. Start it.
//  4. Wait for it to answer the health endpoint (short timeout —
//     the old version was already healthy before we started, so
//     it should answer quickly).
func rollback(ctx context.Context, j *biz.SelfDeployJob, sw *stateWriter, cli *client.Client) error {
	sw.Logf("attempting auto-rollback")

	// Step 1 — kill the partially-deployed new stack. Using the
	// compose engine so any networks/volumes it created are also
	// torn down. Swallowing errors: the engine may have been
	// unable to create the project at all, in which case there's
	// nothing to remove.
	engine := compose.NewStandaloneEngine(cli)
	rmCtx, rmCancel := context.WithTimeout(ctx, 30*time.Second)
	defer rmCancel()
	if err := engine.Remove(rmCtx, misc.SelfDeployStackName, false); err != nil {
		sw.Logf("rollback: cleanup of new stack failed (continuing): %v", err)
	} else {
		sw.Logf("rollback: removed partial new stack")
	}

	// Step 1b — handle the edge case where a new `swirl` container
	// exists outside the compose project (e.g. created manually
	// between steps). If so, remove it.
	if exists, _ := containerExists(ctx, cli, misc.SelfDeployContainerName); exists {
		sw.Logf("rollback: removing leftover container named %q", misc.SelfDeployContainerName)
		if err := removeContainer(ctx, cli, misc.SelfDeployContainerName); err != nil {
			return fmt.Errorf("remove leftover %q: %w", misc.SelfDeployContainerName, err)
		}
	}

	// Step 2 — rename the previous container back.
	exists, err := containerExists(ctx, cli, previousContainerName)
	if err != nil {
		return fmt.Errorf("probe previous: %w", err)
	}
	if !exists {
		return fmt.Errorf("previous container %q not found; manual recovery required", previousContainerName)
	}
	sw.Logf("rollback: renaming %q back to %q", previousContainerName, misc.SelfDeployContainerName)
	if err := renameContainer(ctx, cli, previousContainerName, misc.SelfDeployContainerName); err != nil {
		return fmt.Errorf("rename back: %w", err)
	}

	// Step 3 — start the old container.
	sw.Logf("rollback: starting %q", misc.SelfDeployContainerName)
	if err := startContainer(ctx, cli, misc.SelfDeployContainerName); err != nil {
		return fmt.Errorf("start previous: %w", err)
	}

	// Step 4 — short health wait. The old version should come up
	// quickly because it was healthy right before we stopped it.
	healthURL := buildHealthURL(j)
	sw.Logf("rollback: waiting for old Swirl to respond at %s (budget %s)", healthURL, rollbackHealthTimeout)
	if err := waitHealthy(ctx, healthURL, rollbackHealthTimeout); err != nil {
		return fmt.Errorf("previous Swirl did not come back up: %w", err)
	}
	sw.Logf("rollback: previous Swirl is healthy again")
	return nil
}

