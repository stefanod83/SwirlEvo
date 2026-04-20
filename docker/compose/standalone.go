package compose

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	composetypes "github.com/cuigh/swirl/docker/compose/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"github.com/docker/docker/errdefs"
	"github.com/docker/go-connections/nat"
	"gopkg.in/yaml.v2"
)

// Standard docker-compose labels — same naming as the official CLI so containers
// created by `docker compose` are visible here as well.
const (
	LabelProject     = "com.docker.compose.project"
	LabelService     = "com.docker.compose.service"
	LabelNumber      = "com.docker.compose.container-number"
	LabelOneoff      = "com.docker.compose.oneoff"
	LabelNetworkName = "com.docker.compose.network"
	LabelVolumeName  = "com.docker.compose.volume"
	LabelManaged     = "com.swirl.compose.managed"
)

// StackInfo is a summary of a compose stack discovered on a host.
type StackInfo struct {
	Name       string
	Services   []string
	Containers int
	Running    int
	Status     string
}

// DeployOptions controls deploy behaviour.
type DeployOptions struct {
	PullImages bool // pull each service image before creating containers
	// EnvVars are injected into the process environment BEFORE the
	// compose YAML is parsed, so `${VAR}` references in the YAML are
	// expanded. They're cleaned up after parsing to avoid leaking
	// into subsequent operations in the same process.
	EnvVars map[string]string

	// Hook, if non-nil, lets an external component (e.g. VaultSecret
	// materializer) influence the deploy without the engine having to know
	// about Vault. The engine calls the hook methods at well-defined points
	// in the Deploy lifecycle.
	Hook DeployHook

	// PreserveContainerNames is a set of container names that must NOT
	// be removed by the pre-deploy cleanup, even when they carry the
	// project label. The self-deploy sidekick uses this to preserve
	// its renamed backup ("swirl-previous") across the engine's
	// removeProjectContainers call — without this, the backup is
	// destroyed before the new deploy is even attempted and any
	// rollback is impossible.
	PreserveContainerNames []string
}

// DeployResult summarises the non-fatal observations produced by a deploy.
// Warnings are additive diagnostics — they never abort the deploy. Callers
// typically persist them alongside the stack record and surface them in the
// UI so the user is aware of fields that were silently dropped (e.g. the
// Swarm-only `deploy:` block).
type DeployResult struct {
	// Warnings carries one entry per ignored / non-portable compose feature,
	// each prefixed with the offending service name when applicable.
	Warnings []string
}

// DeployHook lets a caller inject side effects into the Deploy lifecycle
// without the engine taking a dependency on higher-level packages (biz,
// vault). The standard use is to materialize VaultSecret bindings as
// environment variables or files inside service containers.
//
// All methods must be safe to invoke when the hook has no work to do for
// the given stack/service — they should return nil without side effects.
type DeployHook interface {
	// BeforeDeploy runs after networks/volumes are ensured but before any
	// service container is created. Typical use: populate secret volumes
	// via short-lived helper containers.
	BeforeDeploy(ctx context.Context, cli *client.Client, project string) error
	// ApplyToService may add env vars or mounts to the given service.
	// Modifications happen in-place on the returned slices (the engine
	// passes them back into ContainerCreate).
	ApplyToService(ctx context.Context, project, service string, env []string, mounts []mount.Mount) (newEnv []string, newMounts []mount.Mount, err error)
	// AfterCreate runs after ContainerCreate but before ContainerStart for
	// the given service. Typical use: CopyToContainer to populate a
	// tmpfs-backed secret file.
	AfterCreate(ctx context.Context, cli *client.Client, project, service, containerID string) error
	// AfterRemove runs after all project containers have been removed.
	// Typical use: cleanup of secret volumes scoped to the project.
	AfterRemove(ctx context.Context, cli *client.Client, project string) error
}

// StandaloneEngine deploys a docker-compose file on a single Docker daemon using the SDK only.
type StandaloneEngine struct {
	cli *client.Client
}

// NewStandaloneEngine wraps a live docker client.
func NewStandaloneEngine(cli *client.Client) *StandaloneEngine {
	return &StandaloneEngine{cli: cli}
}

// Deploy is the backwards-compatible thin wrapper around DeployWithResult.
// Existing callers that don't care about warnings can keep using it
// verbatim — the engine behaviour is identical.
func (e *StandaloneEngine) Deploy(ctx context.Context, projectName, content string, opts DeployOptions) error {
	_, err := e.DeployWithResult(ctx, projectName, content, opts)
	return err
}

// DeployWithResult applies the compose file and returns a DeployResult
// carrying non-fatal warnings (e.g. ignored `deploy:` blocks). The fatal
// behaviour is strictly additive vs the previous Deploy:
//
//  1. preflight: parse + validate + scan for ignored fields (warnings);
//  2. preflight: verify every `external: true` network/volume actually
//     exists on the target daemon — abort early when any is missing so we
//     never tear down the previous stack state on a typo;
//  3. preflight: when PullImages is set, pull ALL service images BEFORE
//     touching existing containers. On failure the old stack is intact.
//  4. tear down the previous project containers (was before: same);
//  5. ensure networks / volumes / hook BeforeDeploy (unchanged);
//  6. create + start each service container (unchanged).
//
// Warnings are additive — they never change the outcome.
func (e *StandaloneEngine) DeployWithResult(ctx context.Context, projectName, content string, opts DeployOptions) (*DeployResult, error) {
	result := &DeployResult{}

	// Inject env vars before parsing so ${VAR} references in the YAML
	// are expanded by the compose loader. Cleaned up after parsing to
	// avoid polluting the process for subsequent operations.
	if len(opts.EnvVars) > 0 {
		for k, v := range opts.EnvVars {
			os.Setenv(k, v)
		}
		defer func() {
			for k := range opts.EnvVars {
				os.Unsetenv(k)
			}
		}()
	}

	cfg, err := Parse(projectName, content)
	if err != nil {
		return result, fmt.Errorf("parse compose: %w", err)
	}

	if err := validateServices(cfg); err != nil {
		return result, err
	}

	// B1 — collect warnings for compose-spec fields the standalone engine
	// silently ignores, so the operator knows the running stack is not a
	// faithful materialisation of the YAML.
	result.Warnings = append(result.Warnings, collectIgnoredFieldWarnings(cfg)...)

	// B6 — external networks/volumes must exist before we touch anything.
	// A typo'd external reference would otherwise tear down the previous
	// stack containers and then fail halfway through recreation, leaving
	// the project in an unusable state.
	if err := e.preflightExternalNetworks(ctx, projectName, cfg.Networks); err != nil {
		return result, err
	}
	if err := e.preflightExternalVolumes(ctx, projectName, cfg.Volumes); err != nil {
		return result, err
	}

	// B5 — pull all images up-front. On any failure we abort without
	// touching the old containers. Errors are aggregated so the operator
	// sees every missing image at once, not just the first.
	if opts.PullImages {
		var pullErrors []string
		for _, svc := range cfg.Services {
			if svc.Image == "" {
				continue
			}
			if perr := e.pullImage(ctx, svc.Image); perr != nil {
				pullErrors = append(pullErrors, fmt.Sprintf("%s (%s): %v", svc.Name, svc.Image, perr))
			}
		}
		if len(pullErrors) > 0 {
			return result, fmt.Errorf("pre-flight pull failed for: %s", strings.Join(pullErrors, "; "))
		}
	}

	if err := e.removeProjectContainers(ctx, projectName, false, opts.PreserveContainerNames...); err != nil {
		return result, err
	}

	if err := e.ensureNetworks(ctx, projectName, cfg.Networks); err != nil {
		return result, err
	}
	if err := e.ensureVolumes(ctx, projectName, cfg.Volumes); err != nil {
		return result, err
	}

	if opts.Hook != nil {
		if err := opts.Hook.BeforeDeploy(ctx, e.cli, projectName); err != nil {
			return result, fmt.Errorf("deploy hook (before): %w", err)
		}
	}

	// Compose-spec parity: create + start services in the order dictated
	// by `depends_on`. Without this, the Docker SDK creates containers in
	// map-iteration order and a service that needs its DB peer up-front
	// can race the daemon.
	order, err := topologicalOrder(cfg.Services)
	if err != nil {
		return result, err
	}

	idxByName := map[string]int{}
	for i := range cfg.Services {
		idxByName[cfg.Services[i].Name] = i
	}

	for _, name := range order {
		i := idxByName[name]
		svc := &cfg.Services[i]

		// Before starting this service, wait for every dependency to
		// reach the requested condition. Errors here abort the deploy —
		// the rationale is compose-spec parity: `service_healthy` means
		// "do not proceed if the dependency is not healthy".
		for _, dep := range svc.DependsOn {
			if err := e.waitForDependency(ctx, projectName, dep); err != nil {
				return result, fmt.Errorf("service %s: dependency %s: %w", svc.Name, dep.Service, err)
			}
		}

		if err := e.createAndStart(ctx, projectName, cfg, svc, opts.Hook); err != nil {
			return result, fmt.Errorf("service %s: %w", svc.Name, err)
		}
	}
	return result, nil
}

// topologicalOrder returns the service names sorted so every dependency
// appears before the services that declare it in their `depends_on`.
// Uses Kahn's algorithm; returns a clear error on cycles + on missing
// dependency targets (compose-spec requires them to exist).
//
// Ties are broken by alphabetical service name so the order is
// deterministic across runs (same input → same order).
func topologicalOrder(services composetypes.Services) ([]string, error) {
	name := make(map[string]struct{}, len(services))
	for _, s := range services {
		name[s.Name] = struct{}{}
	}
	// inEdges[service] = number of unresolved dependencies.
	inEdges := make(map[string]int, len(services))
	// outEdges[service] = list of services that depend on it.
	outEdges := make(map[string][]string, len(services))
	for _, s := range services {
		if _, seen := inEdges[s.Name]; !seen {
			inEdges[s.Name] = 0
		}
		for _, d := range s.DependsOn {
			if _, exists := name[d.Service]; !exists {
				return nil, fmt.Errorf("service %q references undefined dependency %q in depends_on", s.Name, d.Service)
			}
			inEdges[s.Name]++
			outEdges[d.Service] = append(outEdges[d.Service], s.Name)
		}
	}

	// Start with every service that has no dependency, sorted
	// alphabetically.
	ready := make([]string, 0, len(services))
	for _, s := range services {
		if inEdges[s.Name] == 0 {
			ready = append(ready, s.Name)
		}
	}
	sort.Strings(ready)

	order := make([]string, 0, len(services))
	for len(ready) > 0 {
		current := ready[0]
		ready = ready[1:]
		order = append(order, current)
		children := outEdges[current]
		sort.Strings(children)
		for _, child := range children {
			inEdges[child]--
			if inEdges[child] == 0 {
				// Preserve alphabetical order among simultaneously-ready.
				insertAt := sort.SearchStrings(ready, child)
				ready = append(ready, "")
				copy(ready[insertAt+1:], ready[insertAt:])
				ready[insertAt] = child
			}
		}
	}
	if len(order) != len(services) {
		unresolved := make([]string, 0)
		for n, e := range inEdges {
			if e > 0 {
				unresolved = append(unresolved, n)
			}
		}
		sort.Strings(unresolved)
		return nil, fmt.Errorf("circular dependency detected in depends_on involving services %v", unresolved)
	}
	return order, nil
}

// waitForDependency polls the container that backs the given dependency
// service until the declared condition is met, or the internal deadline
// elapses. The condition semantics mirror the compose spec:
//
//   - service_started (default when empty) — container is Running.
//   - service_healthy — container has a healthcheck AND its current
//     Health.Status is "healthy". If the service has no healthcheck we
//     fall back to service_started with a warning log.
//   - service_completed_successfully — container has exited with code 0.
//
// Returns an error on timeout. The ctx is honoured — cancelling it
// aborts the poll immediately.
func (e *StandaloneEngine) waitForDependency(ctx context.Context, project string, dep composetypes.ServiceDependency) error {
	cond := dep.Condition
	if cond == "" {
		cond = "service_started"
	}

	// Timeout budget depends on the condition. These are pragmatic
	// defaults; a user who needs longer should tune the healthcheck
	// start_period/retries or split the deploy.
	var timeout time.Duration
	switch cond {
	case "service_started":
		timeout = 30 * time.Second
	case "service_healthy":
		timeout = 2 * time.Minute
	case "service_completed_successfully":
		timeout = 5 * time.Minute
	default:
		return fmt.Errorf("unsupported depends_on condition %q (expected service_started, service_healthy, or service_completed_successfully)", cond)
	}

	deadline := time.Now().Add(timeout)
	const pollInterval = 1 * time.Second

	for {
		// Locate the dependency's container by compose labels.
		list, lerr := e.cli.ContainerList(ctx, container.ListOptions{
			All: true,
			Filters: filters.NewArgs(
				filters.Arg("label", LabelProject+"="+project),
				filters.Arg("label", LabelService+"="+dep.Service),
			),
		})
		if lerr != nil {
			return fmt.Errorf("list dependency container: %w", lerr)
		}
		if len(list) > 0 {
			info, ierr := e.cli.ContainerInspect(ctx, list[0].ID)
			if ierr == nil && info.State != nil {
				switch cond {
				case "service_started":
					if info.State.Running {
						return nil
					}
					// Also accept a healthy container as "started" —
					// some images are up + listening before the daemon
					// flips Running, and State.Health implies Running.
					if info.State.Health != nil && info.State.Health.Status == "healthy" {
						return nil
					}
				case "service_healthy":
					if info.State.Health == nil {
						// Service has no healthcheck → compose spec
						// says this is a user error; we relax to
						// service_started so pre-healthcheck stacks
						// don't regress when upgrading Swirl.
						if info.State.Running {
							return nil
						}
					} else if info.State.Health.Status == "healthy" {
						return nil
					}
				case "service_completed_successfully":
					if info.State.Status == "exited" {
						if info.State.ExitCode == 0 {
							return nil
						}
						return fmt.Errorf("service %q exited with code %d (condition service_completed_successfully expects 0)", dep.Service, info.State.ExitCode)
					}
				}
			}
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("timed out after %s waiting for %s on dependency %q", timeout, cond, dep.Service)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}

// collectIgnoredFieldWarnings scans the parsed compose config for fields
// the standalone engine is known to drop and emits one warning per
// service. Pure function, no Docker calls — safe to unit test.
func collectIgnoredFieldWarnings(cfg *composetypes.Config) []string {
	if cfg == nil {
		return nil
	}
	var warnings []string
	for _, svc := range cfg.Services {
		// `deploy:` — Swarm-only scheduling/replication block. The
		// compose spec explicitly says it's ignored by non-Swarm
		// engines; we echo that to the operator.
		if !isZeroDeployConfig(svc.Deploy) {
			warnings = append(warnings, fmt.Sprintf("service %s: deploy: block ignored in standalone mode", svc.Name))
		}
	}
	return warnings
}

// isZeroDeployConfig reports whether the compose-level DeployConfig is
// effectively empty (i.e. the YAML omitted the block). We can't compare
// against the struct zero value with == because DeployConfig contains
// map/slice/pointer fields; a field-by-field check is the portable
// version.
func isZeroDeployConfig(d composetypes.DeployConfig) bool {
	return d.Mode == "" &&
		d.Replicas == nil &&
		len(d.Labels) == 0 &&
		d.UpdateConfig == nil &&
		d.RollbackConfig == nil &&
		isZeroResources(d.Resources) &&
		d.RestartPolicy == nil &&
		isZeroPlacement(d.Placement) &&
		d.EndpointMode == ""
}

func isZeroResources(r composetypes.Resources) bool {
	return r.Limits == nil && r.Reservations == nil
}

func isZeroPlacement(p composetypes.Placement) bool {
	return len(p.Constraints) == 0 && len(p.Preferences) == 0 && p.MaxReplicas == 0
}

// preflightExternalNetworks verifies every `external: true` network
// declared in the compose file actually exists on the target daemon.
// External networks are referenced by their resolved name (the `Name:`
// field when set, otherwise the key). Non-external networks are
// skipped — they're created on demand by ensureNetworks.
func (e *StandaloneEngine) preflightExternalNetworks(ctx context.Context, project string, nets map[string]composetypes.NetworkConfig) error {
	for name, ncfg := range nets {
		if !ncfg.External.External {
			continue
		}
		resolved := ncfg.Name
		if resolved == "" {
			resolved = name
		}
		if _, err := e.cli.NetworkInspect(ctx, resolved, network.InspectOptions{}); err != nil {
			if errdefs.IsNotFound(err) {
				return fmt.Errorf("external network %q not found on host", resolved)
			}
			return fmt.Errorf("external network %q: %w", resolved, err)
		}
	}
	return nil
}

// preflightExternalVolumes mirrors preflightExternalNetworks for volumes.
func (e *StandaloneEngine) preflightExternalVolumes(ctx context.Context, project string, vols map[string]composetypes.VolumeConfig) error {
	for name, vcfg := range vols {
		if !vcfg.External.External {
			continue
		}
		resolved := vcfg.Name
		if resolved == "" {
			resolved = name
		}
		if _, err := e.cli.VolumeInspect(ctx, resolved); err != nil {
			if errdefs.IsNotFound(err) {
				return fmt.Errorf("external volume %q not found on host", resolved)
			}
			return fmt.Errorf("external volume %q: %w", resolved, err)
		}
	}
	return nil
}

// validateServices enforces the standalone-mode service contract BEFORE any
// side effect is performed (no pull, no container create, no hook call).
//
// Rules (strict, Opzione A1):
//
//  1. `build:` is NOT supported — the standalone engine does not invoke
//     `ImageBuild` on the daemon. A service that declares `build.context`
//     (with or without `image:`) is rejected up-front with an actionable
//     message. Previously such services reached `ContainerCreate` with an
//     empty image reference and produced a confusing "no command specified"
//     error from the Docker daemon.
//  2. Each service must reference an image. A service that has neither
//     `image:` nor `build:` is a malformed compose file and is rejected
//     with a clear error.
//
// The function is pure (no Docker calls, no env mutation) so it can be
// exercised by unit tests without a live daemon.
func validateServices(cfg *composetypes.Config) error {
	if cfg == nil {
		return nil
	}
	for _, svc := range cfg.Services {
		if svc.Build.Context != "" {
			return fmt.Errorf("service %s: 'build:' is not supported in standalone mode; pre-build the image and reference it with 'image:' only", svc.Name)
		}
		if svc.Image == "" {
			return fmt.Errorf("service %s: neither 'image:' nor 'build:' is set; an image reference is required", svc.Name)
		}
	}
	return nil
}

// Start starts all containers belonging to a project.
func (e *StandaloneEngine) Start(ctx context.Context, projectName string) error {
	containers, err := e.listProjectContainers(ctx, projectName, true)
	if err != nil {
		return err
	}
	for _, c := range containers {
		if c.State == "running" {
			continue
		}
		if err := e.cli.ContainerStart(ctx, c.ID, container.StartOptions{}); err != nil {
			return err
		}
	}
	return nil
}

// Stop stops all running containers of a project.
func (e *StandaloneEngine) Stop(ctx context.Context, projectName string) error {
	containers, err := e.listProjectContainers(ctx, projectName, true)
	if err != nil {
		return err
	}
	for _, c := range containers {
		if c.State != "running" {
			continue
		}
		if err := e.cli.ContainerStop(ctx, c.ID, container.StopOptions{}); err != nil {
			return err
		}
	}
	return nil
}

// Remove stops and deletes all resources of a project.
// When removeVolumes is true, project-labeled volumes are removed too.
// The optional hook lets callers run cleanup tasks (e.g. drop secret volumes)
// after the project containers are gone.
func (e *StandaloneEngine) Remove(ctx context.Context, projectName string, removeVolumes bool, hook ...DeployHook) error {
	return e.removeWithPreserve(ctx, projectName, removeVolumes, nil, hook...)
}

// RemoveExcept is Remove but skips the given container names during the
// project container cleanup. Used by the self-deploy sidekick rollback
// so the "swirl-previous" backup container is preserved long enough to
// be renamed back to its original name.
func (e *StandaloneEngine) RemoveExcept(ctx context.Context, projectName string, removeVolumes bool, preserveContainerNames []string, hook ...DeployHook) error {
	return e.removeWithPreserve(ctx, projectName, removeVolumes, preserveContainerNames, hook...)
}

func (e *StandaloneEngine) removeWithPreserve(ctx context.Context, projectName string, removeVolumes bool, preserveNames []string, hook ...DeployHook) error {
	if err := e.removeProjectContainers(ctx, projectName, true, preserveNames...); err != nil {
		return err
	}
	// remove project networks
	nets, err := e.cli.NetworkList(ctx, network.ListOptions{
		Filters: filters.NewArgs(filters.Arg("label", LabelProject+"="+projectName)),
	})
	if err == nil {
		for _, n := range nets {
			_ = e.cli.NetworkRemove(ctx, n.ID)
		}
	}
	if removeVolumes {
		// Double-filter: project label AND managed=true. External/imported
		// volumes that happen to carry the same project label (e.g. a user
		// who `docker volume create --label com.docker.compose.project=...`
		// to share data across stacks) are intentionally spared — Swirl only
		// deletes volumes it created itself.
		vols, err := e.cli.VolumeList(ctx, volume.ListOptions{
			Filters: filters.NewArgs(
				filters.Arg("label", LabelProject+"="+projectName),
				filters.Arg("label", LabelManaged+"=true"),
			),
		})
		if err == nil {
			for _, v := range vols.Volumes {
				_ = e.cli.VolumeRemove(ctx, v.Name, false)
			}
		}
	}
	for _, h := range hook {
		if h != nil {
			_ = h.AfterRemove(ctx, e.cli, projectName)
		}
	}
	return nil
}

// VolumeSummary is a minimal projection of a project-scoped volume for the
// preservation check. HasData is a best-effort heuristic: the Docker daemon
// only populates UsageData when the `?filters` include size calculation, so
// falling through to "unknown" is preferable to a false-negative that lets
// the caller wipe user data.
type VolumeSummary struct {
	Name    string
	Size    int64
	HasData bool
}

// ListProjectVolumes returns the managed named volumes of a project. The
// filter matches the (project + managed=true) pair that the engine stamps
// at creation time, so external/imported volumes sharing the project label
// are never included — the operator can inspect them separately.
//
// The HasData flag is populated from the daemon's UsageData when available,
// otherwise it falls back to false (we'd rather prompt the user to confirm
// an empty-looking volume than silently delete a non-empty one — the call
// site treats "HasData=true OR unknown size" as "ask for confirmation").
func ListProjectVolumes(ctx context.Context, cli *client.Client, project string) ([]VolumeSummary, error) {
	resp, err := cli.VolumeList(ctx, volume.ListOptions{
		Filters: filters.NewArgs(
			filters.Arg("label", LabelProject+"="+project),
			filters.Arg("label", LabelManaged+"=true"),
		),
	})
	if err != nil {
		return nil, err
	}
	out := make([]VolumeSummary, 0, len(resp.Volumes))
	for _, v := range resp.Volumes {
		vs := VolumeSummary{Name: v.Name}
		if v.UsageData != nil && v.UsageData.Size > 0 {
			vs.Size = v.UsageData.Size
			vs.HasData = true
		}
		out = append(out, vs)
	}
	return out, nil
}

// List returns all compose stacks discovered on the host (grouped by project label).
func (e *StandaloneEngine) List(ctx context.Context) ([]StackInfo, error) {
	all, err := e.cli.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("label", LabelProject)),
	})
	if err != nil {
		return nil, err
	}
	byProject := map[string]*StackInfo{}
	for _, c := range all {
		name := c.Labels[LabelProject]
		if name == "" {
			continue
		}
		s, ok := byProject[name]
		if !ok {
			s = &StackInfo{Name: name}
			byProject[name] = s
		}
		s.Containers++
		if c.State == "running" {
			s.Running++
		}
		svc := c.Labels[LabelService]
		if svc != "" && !containsStr(s.Services, svc) {
			s.Services = append(s.Services, svc)
		}
	}
	out := make([]StackInfo, 0, len(byProject))
	for _, s := range byProject {
		switch {
		case s.Running == 0:
			s.Status = "inactive"
		case s.Running == s.Containers:
			s.Status = "active"
		default:
			s.Status = "partial"
		}
		out = append(out, *s)
	}
	return out, nil
}

// ==== internal helpers ====

func (e *StandaloneEngine) listProjectContainers(ctx context.Context, project string, includeStopped bool) ([]container.Summary, error) {
	return e.cli.ContainerList(ctx, container.ListOptions{
		All:     includeStopped,
		Filters: filters.NewArgs(filters.Arg("label", LabelProject+"="+project)),
	})
}

func (e *StandaloneEngine) removeProjectContainers(ctx context.Context, project string, removeAll bool, preserveNames ...string) error {
	list, err := e.listProjectContainers(ctx, project, true)
	if err != nil {
		return err
	}
	preserve := map[string]struct{}{}
	for _, n := range preserveNames {
		n = strings.TrimPrefix(n, "/")
		if n != "" {
			preserve[n] = struct{}{}
		}
	}
	for _, c := range list {
		if len(preserve) > 0 {
			skip := false
			for _, cn := range c.Names {
				stripped := strings.TrimPrefix(cn, "/")
				if _, match := preserve[stripped]; match {
					skip = true
					break
				}
			}
			if skip {
				continue
			}
		}
		if c.State == "running" {
			_ = e.cli.ContainerStop(ctx, c.ID, container.StopOptions{})
		}
		// RemoveVolumes=true drops *anonymous* volumes attached to the
		// container (e.g. VOLUME directives in the image) — named volumes are
		// not affected by this flag; they stay live and are gated separately
		// by the `removeVolumes` parameter of Remove() via label-filtered
		// VolumeRemove calls. This prevents anonymous-volume leaks on
		// every redeploy without touching user data in named volumes.
		if err := e.cli.ContainerRemove(ctx, c.ID, container.RemoveOptions{Force: true, RemoveVolumes: true}); err != nil {
			if !removeAll {
				return err
			}
		}
	}
	return nil
}

func (e *StandaloneEngine) ensureNetworks(ctx context.Context, project string, nets map[string]composetypes.NetworkConfig) error {
	existing, _ := e.cli.NetworkList(ctx, network.ListOptions{
		Filters: filters.NewArgs(filters.Arg("label", LabelProject+"="+project)),
	})
	existingByName := map[string]struct{}{}
	for _, n := range existing {
		existingByName[n.Name] = struct{}{}
	}
	if len(nets) == 0 {
		// implicit "default" network
		nets = map[string]composetypes.NetworkConfig{"default": {}}
	}
	for name, ncfg := range nets {
		if ncfg.External.External {
			continue
		}
		qualified := project + "_" + name
		if _, ok := existingByName[qualified]; ok {
			continue
		}
		// Compose v2 label compliance: the daemon validates that the
		// `com.docker.compose.network` label matches the short (non-qualified)
		// network name when the network is referenced by a service. Missing
		// this label produces the confusing "network has incorrect label" error
		// on subsequent deploys that touch the same network.
		labels := map[string]string{
			LabelProject:     project,
			LabelNetworkName: name,
			LabelManaged:     "true",
		}
		for k, v := range ncfg.Labels {
			labels[k] = v
		}
		opts := network.CreateOptions{
			Driver: ncfg.Driver,
			Labels: labels,
		}
		if opts.Driver == "" {
			opts.Driver = "bridge"
		}
		if _, err := e.cli.NetworkCreate(ctx, qualified, opts); err != nil {
			return fmt.Errorf("create network %s: %w", qualified, err)
		}
	}
	return nil
}

func (e *StandaloneEngine) ensureVolumes(ctx context.Context, project string, vols map[string]composetypes.VolumeConfig) error {
	for name, vcfg := range vols {
		if vcfg.External.External {
			continue
		}
		qualified := project + "_" + name
		_, err := e.cli.VolumeInspect(ctx, qualified)
		if err == nil {
			continue
		}
		// Compose v2 label compliance (mirrors the network case): the daemon
		// validates the `com.docker.compose.volume` label against the short
		// volume name on subsequent operations, so we stamp it at creation.
		labels := map[string]string{
			LabelProject:    project,
			LabelVolumeName: name,
			LabelManaged:    "true",
		}
		for k, v := range vcfg.Labels {
			labels[k] = v
		}
		if _, err := e.cli.VolumeCreate(ctx, volume.CreateOptions{
			Name:   qualified,
			Driver: vcfg.Driver,
			Labels: labels,
		}); err != nil {
			return fmt.Errorf("create volume %s: %w", qualified, err)
		}
	}
	return nil
}

func (e *StandaloneEngine) pullImage(ctx context.Context, ref string) error {
	rc, err := e.cli.ImagePull(ctx, ref, image.PullOptions{})
	if err != nil {
		return err
	}
	defer rc.Close()
	_, err = io.Copy(io.Discard, rc)
	return err
}

func (e *StandaloneEngine) createAndStart(ctx context.Context, project string, cfg *composetypes.Config, svc *composetypes.ServiceConfig, hook DeployHook) error {
	labels := map[string]string{
		LabelProject: project,
		LabelService: svc.Name,
		LabelNumber:  "1",
		LabelManaged: "true",
	}
	for k, v := range svc.Labels {
		labels[k] = v
	}

	env := make([]string, 0, len(svc.Environment))
	for k, v := range svc.Environment {
		if v == nil {
			env = append(env, k)
		} else {
			env = append(env, fmt.Sprintf("%s=%s", k, *v))
		}
	}

	// ports
	exposed := nat.PortSet{}
	bindings := nat.PortMap{}
	for _, p := range svc.Ports {
		proto := p.Protocol
		if proto == "" {
			proto = "tcp"
		}
		containerPort := nat.Port(fmt.Sprintf("%d/%s", p.Target, proto))
		exposed[containerPort] = struct{}{}
		if p.Published > 0 {
			bindings[containerPort] = append(bindings[containerPort], nat.PortBinding{
				HostPort: fmt.Sprintf("%d", p.Published),
			})
		}
	}

	// volumes / mounts
	var mounts []mount.Mount
	for _, v := range svc.Volumes {
		m := mount.Mount{Target: v.Target, ReadOnly: v.ReadOnly}
		switch v.Type {
		case "bind":
			m.Type = mount.TypeBind
			m.Source = v.Source
		case "tmpfs":
			m.Type = mount.TypeTmpfs
		default:
			// named volume
			m.Type = mount.TypeVolume
			if v.Source != "" {
				if _, defined := cfg.Volumes[v.Source]; defined {
					m.Source = project + "_" + v.Source
				} else {
					m.Source = v.Source
				}
			}
		}
		mounts = append(mounts, m)
	}

	// Let the hook (VaultSecret materializer) inject additional env/mounts.
	if hook != nil {
		newEnv, newMounts, err := hook.ApplyToService(ctx, project, svc.Name, env, mounts)
		if err != nil {
			return fmt.Errorf("hook apply %s: %w", svc.Name, err)
		}
		env = newEnv
		mounts = newMounts
	}

	// networks: first listed (alphabetical or explicit) becomes primary
	var primaryNet string
	aliases := map[string][]string{}
	networksOrder := []string{}
	for n, cfgN := range svc.Networks {
		networksOrder = append(networksOrder, n)
		if cfgN != nil {
			aliases[n] = cfgN.Aliases
		}
	}
	if len(networksOrder) == 0 {
		networksOrder = []string{"default"}
	}
	// Compose v2 parity: the service name is ALWAYS a DNS alias on
	// every network the service attaches to. Without this, a peer
	// looking up `mongodb` on `swirlevo-net` gets NXDOMAIN and the
	// application blows up mid-handshake — classic compose behaviour
	// the standalone engine used to drop.
	for _, n := range networksOrder {
		if !containsStr(aliases[n], svc.Name) {
			aliases[n] = append([]string{svc.Name}, aliases[n]...)
		}
	}
	primaryNet = qualifyNetwork(project, cfg.Networks, networksOrder[0])

	restart := container.RestartPolicy{}
	switch strings.ToLower(svc.Restart) {
	case "always":
		restart.Name = container.RestartPolicyAlways
	case "on-failure":
		restart.Name = container.RestartPolicyOnFailure
	case "unless-stopped":
		restart.Name = container.RestartPolicyUnlessStopped
	case "no", "":
		restart.Name = container.RestartPolicyDisabled
	}

	containerName := svc.ContainerName
	if containerName == "" {
		containerName = project + "_" + svc.Name + "_1"
	}

	ccfg := &container.Config{
		Hostname:     svc.Hostname,
		User:         svc.User,
		Env:          env,
		Image:        svc.Image,
		Labels:       labels,
		ExposedPorts: exposed,
		WorkingDir:   svc.WorkingDir,
		Tty:          svc.Tty,
		OpenStdin:    svc.StdinOpen,
		Healthcheck:  buildHealthcheck(svc),
	}
	if len(svc.Entrypoint) > 0 {
		ccfg.Entrypoint = []string(svc.Entrypoint)
	}
	if len(svc.Command) > 0 {
		ccfg.Cmd = []string(svc.Command)
	}

	hcfg := &container.HostConfig{
		PortBindings:  bindings,
		Mounts:        mounts,
		RestartPolicy: restart,
		Privileged:    svc.Privileged,
		ReadonlyRootfs: svc.ReadOnly,
		NetworkMode:   container.NetworkMode(primaryNet),
		CapAdd:        svc.CapAdd,
		CapDrop:       svc.CapDrop,
		DNS:           svc.DNS,
		DNSSearch:     svc.DNSSearch,
		LogConfig:     buildLogConfig(svc),
	}

	netCfg := &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{
			primaryNet: {Aliases: aliases[networksOrder[0]]},
		},
	}

	resp, err := e.cli.ContainerCreate(ctx, ccfg, hcfg, netCfg, nil, containerName)
	if err != nil {
		return err
	}

	// connect additional networks
	for _, n := range networksOrder[1:] {
		qn := qualifyNetwork(project, cfg.Networks, n)
		if err := e.cli.NetworkConnect(ctx, qn, resp.ID, &network.EndpointSettings{
			Aliases: aliases[n],
		}); err != nil {
			return err
		}
	}

	// AfterCreate runs before Start so the hook can populate tmpfs-backed
	// secrets via CopyToContainer while the container is still stopped.
	if hook != nil {
		if err := hook.AfterCreate(ctx, e.cli, project, svc.Name, resp.ID); err != nil {
			return fmt.Errorf("hook after-create %s: %w", svc.Name, err)
		}
	}

	return e.cli.ContainerStart(ctx, resp.ID, container.StartOptions{})
}

// buildHealthcheck maps a compose-level HealthCheckConfig onto the Docker SDK
// container.HealthConfig. Mirrors the logic in convertHealthcheck (Swarm path)
// but without the error return: a malformed healthcheck (both Disable=true
// and a Test command) is silently ignored rather than aborting the whole
// deploy — the engine already validated the service earlier, and a warning
// is captured via DeployResult in the caller.
//
// Returns nil when the service has no healthcheck — the Docker daemon then
// falls back to the image's own HEALTHCHECK (if any).
func buildHealthcheck(svc *composetypes.ServiceConfig) *container.HealthConfig {
	if svc == nil || svc.HealthCheck == nil {
		return nil
	}
	hc := svc.HealthCheck
	if hc.Disable {
		// Disable overrides every other field — emits NONE to explicitly
		// disable the image-level HEALTHCHECK.
		return &container.HealthConfig{Test: []string{"NONE"}}
	}
	out := &container.HealthConfig{
		Test: []string(hc.Test),
	}
	if hc.Timeout != nil {
		out.Timeout = time.Duration(*hc.Timeout)
	}
	if hc.Interval != nil {
		out.Interval = time.Duration(*hc.Interval)
	}
	if hc.StartPeriod != nil {
		out.StartPeriod = time.Duration(*hc.StartPeriod)
	}
	if hc.Retries != nil {
		out.Retries = int(*hc.Retries)
	}
	return out
}

// buildLogConfig maps a compose-level LoggingConfig onto the Docker SDK
// container.LogConfig. Returns the zero value when the service has no
// logging block — the daemon then uses its default driver (usually
// json-file), which is the existing behaviour.
func buildLogConfig(svc *composetypes.ServiceConfig) container.LogConfig {
	if svc == nil || svc.Logging == nil {
		return container.LogConfig{}
	}
	return container.LogConfig{
		Type:   svc.Logging.Driver,
		Config: svc.Logging.Options,
	}
}

func qualifyNetwork(project string, declared map[string]composetypes.NetworkConfig, name string) string {
	if cfg, ok := declared[name]; ok && cfg.External.External {
		if cfg.Name != "" {
			return cfg.Name
		}
		return name
	}
	return project + "_" + name
}

func containsStr(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// ProjectDetail collects the live state of a compose project on a host.
type ProjectDetail struct {
	Name       string
	Status     string
	Services   []string
	Containers []container.Summary
	Networks   []string
	Volumes    []string
}

// GetProject returns the full project detail by scanning containers with the
// matching com.docker.compose.project label.
func (e *StandaloneEngine) GetProject(ctx context.Context, projectName string) (*ProjectDetail, error) {
	containers, err := e.listProjectContainers(ctx, projectName, true)
	if err != nil {
		return nil, err
	}
	pd := &ProjectDetail{Name: projectName, Containers: containers}

	serviceSet := map[string]struct{}{}
	netSet := map[string]struct{}{}
	volSet := map[string]struct{}{}
	running := 0

	for _, c := range containers {
		if c.State == "running" {
			running++
		}
		if svc, ok := c.Labels[LabelService]; ok && svc != "" {
			serviceSet[svc] = struct{}{}
		}
		for _, m := range c.Mounts {
			if m.Type == mount.TypeVolume && m.Name != "" {
				volSet[m.Name] = struct{}{}
			}
		}
		if c.NetworkSettings != nil {
			for n := range c.NetworkSettings.Networks {
				netSet[n] = struct{}{}
			}
		}
	}

	switch {
	case len(containers) == 0:
		pd.Status = "inactive"
	case running == 0:
		pd.Status = "inactive"
	case running == len(containers):
		pd.Status = "active"
	default:
		pd.Status = "partial"
	}

	pd.Services = sortedSetKeys(serviceSet)
	pd.Networks = sortedSetKeys(netSet)
	pd.Volumes = sortedSetKeys(volSet)
	return pd, nil
}

func sortedSetKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ReconstructCompose inspects each container of a project and emits a best-effort
// docker-compose v3 YAML that can be fed back to Deploy. It covers the subset of
// fields actually supported by StandaloneEngine.createAndStart. Fields not
// derivable at runtime (build, healthcheck definition, secrets, configs,
// deploy, depends_on) are omitted — the caller is expected to surface a
// warning in the UI.
func (e *StandaloneEngine) ReconstructCompose(ctx context.Context, projectName string) (string, error) {
	containers, err := e.listProjectContainers(ctx, projectName, true)
	if err != nil {
		return "", err
	}
	if len(containers) == 0 {
		return "", fmt.Errorf("project %q has no containers", projectName)
	}

	type portMapping struct {
		Target    uint32 `yaml:"target"`
		Published uint32 `yaml:"published,omitempty"`
		Protocol  string `yaml:"protocol,omitempty"`
	}
	type volumeMapping struct {
		Type     string `yaml:"type,omitempty"`
		Source   string `yaml:"source,omitempty"`
		Target   string `yaml:"target"`
		ReadOnly bool   `yaml:"read_only,omitempty"`
	}
	type serviceSpec struct {
		Image       string            `yaml:"image,omitempty"`
		Entrypoint  []string          `yaml:"entrypoint,omitempty"`
		Command     []string          `yaml:"command,omitempty"`
		Environment []string          `yaml:"environment,omitempty"`
		Labels      map[string]string `yaml:"labels,omitempty"`
		Ports       []portMapping     `yaml:"ports,omitempty"`
		Volumes     []volumeMapping   `yaml:"volumes,omitempty"`
		Networks    []string          `yaml:"networks,omitempty"`
		Restart     string            `yaml:"restart,omitempty"`
		User        string            `yaml:"user,omitempty"`
		WorkingDir  string            `yaml:"working_dir,omitempty"`
		Hostname    string            `yaml:"hostname,omitempty"`
		Tty         bool              `yaml:"tty,omitempty"`
		StdinOpen   bool              `yaml:"stdin_open,omitempty"`
		Privileged  bool              `yaml:"privileged,omitempty"`
		ReadOnly    bool              `yaml:"read_only,omitempty"`
		CapAdd      []string          `yaml:"cap_add,omitempty"`
		CapDrop     []string          `yaml:"cap_drop,omitempty"`
		DNS         []string          `yaml:"dns,omitempty"`
		DNSSearch   []string          `yaml:"dns_search,omitempty"`
	}
	type spec struct {
		Version  string                     `yaml:"version,omitempty"`
		Services map[string]*serviceSpec    `yaml:"services"`
		Networks map[string]map[string]any  `yaml:"networks,omitempty"`
		Volumes  map[string]map[string]any  `yaml:"volumes,omitempty"`
	}

	out := spec{
		Version:  "3.8",
		Services: map[string]*serviceSpec{},
		Networks: map[string]map[string]any{},
		Volumes:  map[string]map[string]any{},
	}

	for _, sum := range containers {
		// prefer the compose service label; fall back to a cleaned container name
		svcName := sum.Labels[LabelService]
		if svcName == "" {
			svcName = strings.TrimPrefix(sum.Names[0], "/")
			svcName = strings.TrimPrefix(svcName, projectName+"_")
			svcName = strings.TrimSuffix(svcName, "_1")
		}
		if _, dup := out.Services[svcName]; dup {
			continue // one service may have multiple containers; reuse the first
		}

		full, err := e.cli.ContainerInspect(ctx, sum.ID)
		if err != nil {
			return "", fmt.Errorf("inspect %s: %w", sum.ID[:12], err)
		}

		s := &serviceSpec{
			Image:      normalizeImage(full.Config.Image),
			WorkingDir: full.Config.WorkingDir,
			User:       full.Config.User,
			Hostname:   full.Config.Hostname,
			Tty:        full.Config.Tty,
			StdinOpen:  full.Config.OpenStdin,
			Labels:     stripInternalLabels(full.Config.Labels),
		}
		if len(full.Config.Entrypoint) > 0 {
			s.Entrypoint = []string(full.Config.Entrypoint)
		}
		if len(full.Config.Cmd) > 0 {
			s.Command = []string(full.Config.Cmd)
		}
		for _, e := range full.Config.Env {
			s.Environment = append(s.Environment, e)
		}

		// Ports
		if full.HostConfig != nil {
			for port, bindings := range full.HostConfig.PortBindings {
				tgt := uint32(port.Int())
				proto := port.Proto()
				if len(bindings) == 0 {
					s.Ports = append(s.Ports, portMapping{Target: tgt, Protocol: cleanProto(proto)})
					continue
				}
				for _, b := range bindings {
					var pub uint32
					fmt.Sscanf(b.HostPort, "%d", &pub)
					s.Ports = append(s.Ports, portMapping{Target: tgt, Published: pub, Protocol: cleanProto(proto)})
				}
			}
			// Mounts
			for _, m := range full.HostConfig.Mounts {
				vm := volumeMapping{Target: m.Target, ReadOnly: m.ReadOnly}
				switch m.Type {
				case mount.TypeBind:
					vm.Type = "bind"
					vm.Source = m.Source
				case mount.TypeTmpfs:
					vm.Type = "tmpfs"
				case mount.TypeVolume:
					vm.Type = "volume"
					vm.Source = strings.TrimPrefix(m.Source, projectName+"_")
					if strings.HasPrefix(m.Source, projectName+"_") {
						out.Volumes[vm.Source] = map[string]any{}
					} else {
						vm.Source = m.Source
						out.Volumes[m.Source] = map[string]any{"external": true, "name": m.Source}
					}
				}
				s.Volumes = append(s.Volumes, vm)
			}
			// Restart
			switch full.HostConfig.RestartPolicy.Name {
			case container.RestartPolicyAlways:
				s.Restart = "always"
			case container.RestartPolicyOnFailure:
				s.Restart = "on-failure"
			case container.RestartPolicyUnlessStopped:
				s.Restart = "unless-stopped"
			}
			s.Privileged = full.HostConfig.Privileged
			s.ReadOnly = full.HostConfig.ReadonlyRootfs
			s.CapAdd = append(s.CapAdd, full.HostConfig.CapAdd...)
			s.CapDrop = append(s.CapDrop, full.HostConfig.CapDrop...)
			s.DNS = append(s.DNS, full.HostConfig.DNS...)
			s.DNSSearch = append(s.DNSSearch, full.HostConfig.DNSSearch...)
		}

		// Networks
		if full.NetworkSettings != nil {
			for n := range full.NetworkSettings.Networks {
				short := strings.TrimPrefix(n, projectName+"_")
				s.Networks = append(s.Networks, short)
				if strings.HasPrefix(n, projectName+"_") {
					out.Networks[short] = map[string]any{}
				} else {
					out.Networks[short] = map[string]any{"external": true, "name": n}
				}
			}
			sort.Strings(s.Networks)
		}

		out.Services[svcName] = s
	}

	buf, err := yaml.Marshal(&out)
	if err != nil {
		return "", err
	}
	header := "# Reconstructed from running containers. Review before deploying.\n" +
		"# Fields not derivable at runtime (build, healthcheck, secrets, configs,\n" +
		"# deploy, depends_on) are omitted.\n\n"
	return header + string(buf), nil
}

func normalizeImage(ref string) string {
	if i := strings.Index(ref, "@sha256:"); i > 0 {
		return ref[:i]
	}
	return ref
}

func cleanProto(p string) string {
	if p == "tcp" {
		return ""
	}
	return p
}

// stripInternalLabels removes labels added by Swirl/docker-compose internals so
// the reconstructed YAML doesn't echo them back.
func stripInternalLabels(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := map[string]string{}
	for k, v := range in {
		if strings.HasPrefix(k, "com.docker.compose.") || strings.HasPrefix(k, "com.swirl.compose.") {
			continue
		}
		out[k] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
