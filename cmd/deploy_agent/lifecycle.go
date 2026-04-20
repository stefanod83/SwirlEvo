package deploy_agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
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
	pullBudgetMin         = 60 * time.Second
	pullBudgetMax         = 5 * time.Minute
	stopGraceDefault      = 30 * time.Second
	rollbackHealthTimeout = 30 * time.Second
)

// runDeploy implements the full sidekick lifecycle.
//
// v2: the compose project name is `j.StackName` (derived from the
// source ComposeStack.Name), NOT hardcoded to "swirl". This means the
// sidekick cooperates cleanly with the Swirl-managed stack: the same
// project name on both sides so Swirl's stack-list UI keeps showing
// the stack as active throughout the deploy.
func runDeploy(ctx context.Context, j *biz.SelfDeployJob, sw *stateWriter) error {
	if j == nil {
		return errors.New("deploy-agent: nil job")
	}
	if err := biz.ValidateSelfDeployJob(j); err != nil {
		sw.Fail(err)
		return err
	}
	logger := log.Get("deploy-agent")

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

	// Step 0 — capture the primary's original container name so we can
	// rename it back on rollback. Inspect BEFORE stop so the response
	// still reflects the live container regardless of any race.
	originalName := ""
	if insp, ierr := inspectContainer(deployCtx, cli, j.PrimaryContainer); ierr == nil {
		originalName = strings.TrimPrefix(insp.Name, "/")
	}
	if originalName == "" {
		// Fallback: the YAML's first service name prefixed with project —
		// matches compose's own `<project>-<service>-1` pattern. The
		// compose engine honours explicit container_name, but the YAML
		// we got from the ComposeStack usually doesn't set one for Swirl.
		originalName = j.StackName
	}

	// Step 1+2 — stop and rename the primary.
	sw.SetPhase(biz.SelfDeployPhaseStopping)
	sw.Logf("stopping primary container %s (graceful %s)", short(j.PrimaryContainer), stopGraceDefault)
	if err := stopPrimary(deployCtx, cli, j.PrimaryContainer, stopGraceDefault); err != nil {
		sw.Fail(err)
		return err
	}
	sw.Logf("renaming primary container %s -> %s (original name: %s)", short(j.PrimaryContainer), previousContainerName, originalName)
	if err := renamePrimary(deployCtx, cli, j.PrimaryContainer, previousContainerName); err != nil {
		_ = startContainer(deployCtx, cli, j.PrimaryContainer)
		sw.Fail(err)
		return err
	}

	// Step 3 — pull the target image.
	sw.SetPhase(biz.SelfDeployPhasePulling)
	sw.Logf("pulling image %s", j.TargetImageTag)
	pullBudget := splitPullBudget(timeout)
	if err := pullImage(deployCtx, cli, j.TargetImageTag, pullBudget); err != nil {
		return handleDeployFailure(deployCtx, cli, j, sw, logger, originalName, fmt.Errorf("image pull: %w", err))
	}

	// Step 4 — deploy the new stack via the standalone engine. Project
	// name is j.StackName so the update lands on the same compose project
	// the source ComposeStack is already tracked under.
	sw.SetPhase(biz.SelfDeployPhaseStarting)
	sw.Logf("deploying stack (project=%s)", j.StackName)
	if err := deployNew(deployCtx, cli, j.StackName, j.ComposeYAML, j.EnvVars); err != nil {
		return handleDeployFailure(deployCtx, cli, j, sw, logger, originalName, fmt.Errorf("deploy new stack: %w", err))
	}

	// Step 5 — wait for the new container to answer /api/system/mode.
	// The sidekick runs with `network_mode: host`, which means
	// 127.0.0.1:<port> only works when the YAML publishes the port to
	// the host. With Traefik fronting Swirl, the YAML typically omits
	// `ports:` on purpose. Resolve the target container by compose
	// labels (project + service containing "swirl") on every probe so
	// the check survives an in-flight container restart (e.g. Swirl
	// crashes once while warming up DB connection, restarts, comes up
	// healthy on a new bridge IP).
	sw.SetPhase(biz.SelfDeployPhaseHealthCheck)
	healthBudget := splitHealthBudget(timeout, pullBudget)
	resolver := func(rctx context.Context) (string, error) {
		return resolveHealthURL(rctx, cli, j)
	}
	if initial, _ := resolver(deployCtx); initial != "" {
		sw.Logf("waiting for new Swirl to answer GET %s (budget %s, resolved by label)", initial, healthBudget)
	} else {
		sw.Logf("waiting for new Swirl to answer /api/system/mode (budget %s, awaiting container registration)", healthBudget)
	}
	if err := waitHealthyResolver(deployCtx, resolver, healthBudget); err != nil {
		return handleDeployFailure(deployCtx, cli, j, sw, logger, originalName, fmt.Errorf("health check: %w", err))
	}

	// Step 6 — new version is healthy. Remove the previous container.
	sw.Logf("deploy healthy; removing %s", previousContainerName)
	if err := removePrevious(deployCtx, cli); err != nil {
		sw.Logf("warning: could not remove %s: %v (leaving in place for operator cleanup)", previousContainerName, err)
	}
	sw.Succeed()
	logger.Infof("self-deploy job %s succeeded (target %s, stack %s)", j.ID, j.TargetImageTag, j.StackName)
	return nil
}

func short(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

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

func splitHealthBudget(total, pullBudget time.Duration) time.Duration {
	remaining := total - pullBudget
	if remaining < minHealthTimeout {
		return minHealthTimeout
	}
	return remaining
}

func buildHealthURL(j *biz.SelfDeployJob) string {
	port := j.Placeholders.ExposePort
	if port == 0 {
		port = misc.SelfDeployExposePort
	}
	return fmt.Sprintf("http://127.0.0.1:%d/api/system/mode", port)
}

// resolveHealthURL finds the IP of the newly-deployed Swirl container
// on its primary network and returns an http:// URL pointing at it.
// Falls back to buildHealthURL (127.0.0.1:port) when the container
// cannot be located — preserves the original behaviour on edge cases.
//
// We search for the container by compose labels
// (com.docker.compose.project=<stack> AND service=<service-with-swirl>).
// The sidekick's host network mode means any container IP routes via
// the daemon's docker0 bridge, so this works without any extra port
// publication in the target YAML.
func resolveHealthURL(ctx context.Context, cli *client.Client, j *biz.SelfDeployJob) (string, error) {
	port := j.Placeholders.ExposePort
	if port == 0 {
		port = misc.SelfDeployExposePort
	}
	ip, err := findSwirlContainerIP(ctx, cli, j.StackName)
	if err != nil || ip == "" {
		// Best-effort fallback — avoids breaking deploys where the
		// YAML DOES publish 8001 on the host.
		return buildHealthURL(j), nil
	}
	return fmt.Sprintf("http://%s:%d/api/system/mode", ip, port), nil
}

func stopPrimary(ctx context.Context, cli *client.Client, containerID string, grace time.Duration) error {
	return stopContainer(ctx, cli, containerID, grace)
}

func renamePrimary(ctx context.Context, cli *client.Client, oldName, newName string) error {
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

func pullImage(ctx context.Context, cli *client.Client, ref string, budget time.Duration) error {
	pullCtx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	return pullImageRaw(pullCtx, cli, ref)
}

func deployNew(ctx context.Context, cli *client.Client, projectName, composeYAML string, envVars map[string]string) error {
	engine := compose.NewStandaloneEngine(cli)
	// PreserveContainerNames: the sidekick renamed the previous swirl
	// container to "swirl-previous" but kept all its labels — including
	// the compose-project label. Without this exclusion the engine's
	// own removeProjectContainers would destroy the backup on the first
	// line of the deploy, leaving nothing to roll back to.
	//
	// EnvVars: the ComposeStack's .env file values, carried through the
	// job descriptor. The engine Setenv's them around the compose
	// parse so `${VAR}` references in volumes/ports/env resolve.
	opts := compose.DeployOptions{
		PullImages:             false,
		PreserveContainerNames: []string{previousContainerName},
		EnvVars:                envVars,
	}
	if _, err := engine.DeployWithResult(ctx, projectName, composeYAML, opts); err != nil {
		return err
	}
	return nil
}

// handleDeployFailure is the shared error path for every step past the
// rename. Decides between AutoRollback and Recovery.
func handleDeployFailure(ctx context.Context, cli *client.Client, j *biz.SelfDeployJob, sw *stateWriter, logger log.Logger, originalName string, cause error) error {
	sw.Logf("deploy failed: %v", cause)
	logger.Errorf("self-deploy job %s failed: %v", j.ID, cause)

	if j.AutoRollback {
		if rbErr := rollback(ctx, j, sw, cli, originalName); rbErr != nil {
			sw.Logf("rollback failed: %v", rbErr)
			sw.MarkRecovery(fmt.Errorf("deploy failed and rollback failed: deploy=%v; rollback=%v", cause, rbErr))
			return fmt.Errorf("self-deploy: deploy failed (%w) and rollback failed (%v)", cause, rbErr)
		}
		sw.MarkRolledBack(cause)
		return fmt.Errorf("self-deploy: deploy failed, rolled back to previous: %w", cause)
	}
	sw.MarkRecovery(cause)
	return fmt.Errorf("self-deploy: deploy failed (no auto-rollback): %w", cause)
}

// rollback attempts to restore the previous container under its
// original name and bring it back up.
func rollback(ctx context.Context, j *biz.SelfDeployJob, sw *stateWriter, cli *client.Client, originalName string) error {
	sw.Logf("attempting auto-rollback")

	// Step 1 — tear down the partially-deployed new stack. Preserve
	// the "swirl-previous" backup so the rename-back step below can
	// find it: it carries the original compose-project label and
	// would otherwise be destroyed by the engine's own cleanup.
	engine := compose.NewStandaloneEngine(cli)
	rmCtx, rmCancel := context.WithTimeout(ctx, 30*time.Second)
	defer rmCancel()
	if err := engine.RemoveExcept(rmCtx, j.StackName, false, []string{previousContainerName}); err != nil {
		sw.Logf("rollback: cleanup of new stack failed (continuing): %v", err)
	} else {
		sw.Logf("rollback: removed partial new stack")
	}

	// Step 1b — handle the edge case where a container exists outside
	// the compose project under the primary's original name.
	if originalName != "" {
		if exists, _ := containerExists(ctx, cli, originalName); exists {
			sw.Logf("rollback: removing leftover container named %q", originalName)
			if err := removeContainer(ctx, cli, originalName); err != nil {
				return fmt.Errorf("remove leftover %q: %w", originalName, err)
			}
		}
	}

	// Step 2 — rename the previous container back to its original name.
	exists, err := containerExists(ctx, cli, previousContainerName)
	if err != nil {
		return fmt.Errorf("probe previous: %w", err)
	}
	if !exists {
		return fmt.Errorf("previous container %q not found; manual recovery required", previousContainerName)
	}
	target := originalName
	if target == "" {
		target = j.StackName
	}
	sw.Logf("rollback: renaming %q back to %q", previousContainerName, target)
	if err := renameContainer(ctx, cli, previousContainerName, target); err != nil {
		return fmt.Errorf("rename back: %w", err)
	}

	// Step 3 — start the old container.
	sw.Logf("rollback: starting %q", target)
	if err := startContainer(ctx, cli, target); err != nil {
		return fmt.Errorf("start previous: %w", err)
	}

	// Step 4 — short health wait.
	healthURL := buildHealthURL(j)
	sw.Logf("rollback: waiting for old Swirl to respond at %s (budget %s)", healthURL, rollbackHealthTimeout)
	if err := waitHealthy(ctx, healthURL, rollbackHealthTimeout); err != nil {
		return fmt.Errorf("previous Swirl did not come back up: %w", err)
	}
	sw.Logf("rollback: previous Swirl is healthy again")
	return nil
}
