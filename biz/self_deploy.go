package biz

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/cuigh/auxo/log"
	"github.com/cuigh/auxo/net/web"
	"github.com/cuigh/swirl/dao"
	"github.com/cuigh/swirl/docker"
	"github.com/cuigh/swirl/docker/compose"
	composetypes "github.com/cuigh/swirl/docker/compose/types"
	"github.com/cuigh/swirl/misc"
	dockercontainer "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	dockerimage "github.com/docker/docker/api/types/image"
	dockermount "github.com/docker/docker/api/types/mount"
	dockernetwork "github.com/docker/docker/api/types/network"
	dockerclient "github.com/docker/docker/client"
	"github.com/docker/docker/errdefs"
)

// SelfDeployBiz is the orchestration surface for the self-deploy
// feature — v3 paradigm.
//
// v3 drops the template/placeholders/import machinery in favour of a
// simple flag on an existing compose stack:
//
//  1. The operator selects a ComposeStack that represents the Swirl
//     instance currently running and flips the Enabled switch.
//  2. Every edit to the YAML / env / bindings of that stack happens
//     through the normal compose_stack/Edit.vue page.
//  3. Clicking Auto-Deploy on that page triggers TriggerDeploy, which
//     reads the stack's Content verbatim and hands it to the sidekick.
//
// There is no re-rendering, no placeholder substitution and no
// container introspection.
type SelfDeployBiz interface {
	// LoadConfig returns the persisted self-deploy configuration,
	// falling back to sensible defaults when nothing has been saved.
	LoadConfig(ctx context.Context) (*SelfDeployConfig, error)

	// SaveConfig persists the self-deploy configuration. When Enabled
	// is true the SourceStackID must be non-empty.
	SaveConfig(ctx context.Context, cfg *SelfDeployConfig, user web.User) error

	// TriggerDeploy reads the source compose stack verbatim, builds a
	// SelfDeployJob, writes state + lock on the shared volume and
	// spawns the sidekick container.
	TriggerDeploy(ctx context.Context, user web.User) (*SelfDeployJob, error)

	// Status returns the most recent deploy snapshot read from the
	// shared volume.
	Status(ctx context.Context) (*SelfDeployStatus, error)

	// ResetLock force-clears a stuck lock + marks the last state as
	// Failed("abandoned"). Refuses (with ErrSelfDeployBlocked) if the
	// sidekick container is still Running — operators who really want
	// to force it can `docker rm -f` first.
	ResetLock(ctx context.Context, user web.User) (reclaimed bool, err error)
}

// SelfDeployConfig is the persisted settings blob for the self-deploy
// feature (v3).
//
// Retrocompat: older records that still carry the v1/v2 fields
// (`template`, `placeholders`, `recoveryPort`, `recoveryAllow`)
// unmarshal cleanly — json.Unmarshal drops unknown keys. No migration
// is required.
type SelfDeployConfig struct {
	Enabled       bool   `json:"enabled"`
	SourceStackID string `json:"sourceStackId"`
	AutoRollback  bool   `json:"autoRollback"`
	DeployTimeout int    `json:"deployTimeout"` // seconds
}

// SelfDeployStatus is the snapshot surfaced by the Status endpoint.
//
// SidekickContainer + SidekickAlive + SidekickLogs are populated by
// the biz layer at every poll by inspecting the expected agent
// container name (derived from the current job id). They let the UI
// show docker-logs of the sidekick even when the sidekick crashed
// before writing a single byte of state.json.
//
// CanReset is true when the on-disk state points at an in-progress
// phase but the sidekick is missing or exited — i.e. a stale lock.
// The UI surfaces a "Clear stuck lock" button gated on this flag.
type SelfDeployStatus struct {
	Phase             string   `json:"phase"`
	JobID             string   `json:"jobId,omitempty"`
	Error             string   `json:"error,omitempty"`
	LogTail           []string `json:"logTail,omitempty"`
	SidekickContainer string   `json:"sidekickContainer,omitempty"`
	SidekickAlive     bool     `json:"sidekickAlive"`
	SidekickLogs      string   `json:"sidekickLogs,omitempty"`
	CanReset          bool     `json:"canReset"`
}

// ErrSelfDeployNotImplemented is preserved for API-compatibility with
// older callers. v3 no longer produces this error — every code path
// has real behaviour — but the exported symbol stays so external code
// keeps compiling.
var ErrSelfDeployNotImplemented = errors.New("self-deploy: not implemented yet")

// settingIDSelfDeploy is the dao.Setting primary key used to persist
// the self-deploy config.
const settingIDSelfDeploy = "self_deploy"

// sidekickImageFallback is used when the primary container's image
// cannot be resolved via the daemon (e.g. the image was deleted after
// the container was started).
const sidekickImageFallback = "cuigh/swirl:latest"

// NewSelfDeploy is the DI constructor. It also spawns a best-effort
// boot-hook goroutine that reclaims a stale `/data/self-deploy/.lock`
// left over by a Swirl (or sidekick) crash — without it the very
// first auto-deploy after a restart would fail with
// `{"code":1007,"info":"a self-deploy is already in progress"}`.
func NewSelfDeploy(sb SettingBiz, csb ComposeStackBiz, d *docker.Docker, eb EventBiz, di dao.Interface) SelfDeployBiz {
	b := &selfDeployBiz{sb: sb, csb: csb, d: d, eb: eb, di: di, logger: log.Get("self-deploy")}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		var cli *dockerclient.Client
		if d != nil {
			cli, _ = d.Client()
		}
		if _, err := reclaimStaleLock(ctx, cli, b.logger); err != nil && b.logger != nil {
			b.logger.Warnf("self-deploy: boot-hook reclaim failed: %v", err)
		}
		// Opportunistic cleanup of orphan exited sidekick containers
		// accumulated over many deploys. Safe: only touches exited ones.
		b.cleanupExitedSidekicks(ctx)
	}()
	return b
}

type selfDeployBiz struct {
	sb     SettingBiz
	csb    ComposeStackBiz
	d      *docker.Docker
	eb     EventBiz
	di     dao.Interface
	logger log.Logger
}

// LoadConfig returns the persisted config merged with v3 defaults.
// Legacy fields (template / placeholders) are ignored silently thanks
// to the unknown-key tolerance of json.Unmarshal.
func (b *selfDeployBiz) LoadConfig(ctx context.Context) (*SelfDeployConfig, error) {
	raw, err := b.sb.Find(ctx, settingIDSelfDeploy)
	if err != nil {
		return nil, fmt.Errorf("self-deploy: load config: %w", err)
	}
	cfg := &SelfDeployConfig{}
	if raw != nil {
		if buf, merr := json.Marshal(raw); merr == nil {
			_ = json.Unmarshal(buf, cfg)
		}
	}
	return applyConfigDefaults(cfg), nil
}

// applyConfigDefaults fills zero-valued fields with safe defaults.
// Package-level (not a method) so it can be shared with tests that
// build a config manually.
func applyConfigDefaults(cfg *SelfDeployConfig) *SelfDeployConfig {
	if cfg == nil {
		cfg = &SelfDeployConfig{}
	}
	if cfg.DeployTimeout <= 0 {
		cfg.DeployTimeout = misc.SelfDeployDefaultTimeoutSec
	}
	// AutoRollback has no "unset" sentinel — default to true when a
	// brand-new record is being created (Enabled=false && no source
	// stack yet). Once the operator saves a config we trust the
	// persisted value.
	if !cfg.Enabled && cfg.SourceStackID == "" && !cfg.AutoRollback {
		cfg.AutoRollback = true
	}
	return cfg
}

// SaveConfig validates + persists the config. No template rendering.
// When Enabled==true the SourceStackID must reference a concrete stack.
func (b *selfDeployBiz) SaveConfig(ctx context.Context, cfg *SelfDeployConfig, user web.User) error {
	if cfg == nil {
		return errors.New("self-deploy: nil config")
	}
	cfg.SourceStackID = strings.TrimSpace(cfg.SourceStackID)
	if cfg.Enabled && cfg.SourceStackID == "" {
		return errors.New("self-deploy: source stack is required when enabled")
	}

	buf, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("self-deploy: marshal config: %w", err)
	}
	return b.sb.Save(ctx, settingIDSelfDeploy, json.RawMessage(buf), user)
}

// detectTargetImage scans the parsed compose config for the service
// that represents Swirl and returns its image tag. Heuristic:
//  1. service whose image name contains "swirl" (case-insensitive)
//  2. otherwise the first service in deterministic order by name.
//
// Returns an error when no service at all is declared, or when the
// chosen service has an empty image.
func detectTargetImage(cfg *composetypes.Config) (string, error) {
	if cfg == nil || len(cfg.Services) == 0 {
		return "", errors.New("self-deploy: compose file has no services")
	}
	services := append(composetypes.Services(nil), cfg.Services...)
	// Deterministic fallback order.
	// Not using sort.Slice to avoid the extra dep in tests that inline
	// types.ServiceConfig — the slice is typically short (<10).
	for i := 0; i < len(services); i++ {
		for j := i + 1; j < len(services); j++ {
			if services[j].Name < services[i].Name {
				services[i], services[j] = services[j], services[i]
			}
		}
	}
	var pick *composetypes.ServiceConfig
	for i := range services {
		if strings.Contains(strings.ToLower(services[i].Image), "swirl") {
			pick = &services[i]
			break
		}
	}
	if pick == nil {
		pick = &services[0]
	}
	img := strings.TrimSpace(pick.Image)
	if img == "" {
		return "", fmt.Errorf("self-deploy: service %q has no image declared", pick.Name)
	}
	return img, nil
}

// detectExposePort scans the chosen Swirl service for a published
// port, preferring the target (container) port when set. Falls back
// to the default 8001. Best-effort — used only to build the health
// check URL on the sidekick side.
func detectExposePort(cfg *composetypes.Config) int {
	if cfg == nil {
		return misc.SelfDeployExposePort
	}
	var swirl *composetypes.ServiceConfig
	for i := range cfg.Services {
		if strings.Contains(strings.ToLower(cfg.Services[i].Image), "swirl") {
			swirl = &cfg.Services[i]
			break
		}
	}
	if swirl == nil && len(cfg.Services) > 0 {
		swirl = &cfg.Services[0]
	}
	if swirl == nil {
		return misc.SelfDeployExposePort
	}
	for _, p := range swirl.Ports {
		if p.Target != 0 {
			return int(p.Target)
		}
		if p.Published != 0 {
			return int(p.Published)
		}
	}
	return misc.SelfDeployExposePort
}

// prepareJob assembles a SelfDeployJob from the persisted config + the
// source ComposeStack record + the primary container inspection. Pure
// inspection — does not spawn anything and does not touch the state
// directory.
func (b *selfDeployBiz) prepareJob(ctx context.Context) (*SelfDeployJob, error) {
	if !misc.IsStandalone() {
		return nil, misc.Error(misc.ErrSelfDeployBlocked, errors.New("self-deploy requires standalone mode"))
	}
	selfID, ok := misc.SelfContainerID()
	if !ok || selfID == "" {
		return nil, misc.Error(misc.ErrSelfDeployBlocked, errors.New("cannot identify primary container (set SWIRL_CONTAINER_ID or run inside Docker)"))
	}

	cfg, err := b.LoadConfig(ctx)
	if err != nil {
		return nil, err
	}
	if !cfg.Enabled {
		return nil, misc.Error(misc.ErrSelfDeployBlocked, errors.New("self-deploy is not enabled; enable it in Settings first"))
	}
	if strings.TrimSpace(cfg.SourceStackID) == "" {
		return nil, misc.Error(misc.ErrSelfDeployBlocked, errors.New("no source stack configured; select one in Settings"))
	}
	if b.csb == nil {
		return nil, misc.Error(misc.ErrSelfDeployBlocked, errors.New("compose stack service is not wired — internal configuration error"))
	}
	stack, err := b.csb.Find(ctx, cfg.SourceStackID)
	if err != nil {
		return nil, misc.Error(misc.ErrSelfDeployBlocked, fmt.Errorf("cannot load source stack %q: %v", cfg.SourceStackID, err))
	}
	if stack == nil {
		return nil, misc.Error(misc.ErrSelfDeployBlocked, fmt.Errorf("source stack %q no longer exists — select a different stack in Settings → Self-deploy", cfg.SourceStackID))
	}
	yaml := strings.TrimSpace(stack.Content)
	if yaml == "" {
		return nil, misc.Error(misc.ErrSelfDeployBlocked, fmt.Errorf("source stack %q has no compose content — open the stack editor and paste the YAML first", stack.Name))
	}

	// Inject the stack's EnvFile vars into process env BEFORE parsing
	// so `${VAR}` references (used by ports/volumes/env in the YAML)
	// are resolved. Restore afterwards so the primary Swirl process
	// stays clean. Same pattern as composeStackBiz.runDeploy.
	envVars := parseEnvFile(stack.EnvFile)
	if len(envVars) > 0 {
		restore := make(map[string]string, len(envVars))
		for k, v := range envVars {
			if prev, had := os.LookupEnv(k); had {
				restore[k] = prev
			} else {
				restore[k] = ""
			}
			_ = os.Setenv(k, v)
		}
		defer func() {
			for k, prev := range restore {
				if prev == "" {
					_ = os.Unsetenv(k)
				} else {
					_ = os.Setenv(k, prev)
				}
			}
		}()
	}

	parsed, perr := compose.Parse(stack.Name, stack.Content)
	if perr != nil {
		return nil, misc.Error(misc.ErrSelfDeployBlocked, fmt.Errorf("source stack YAML is invalid: %v", perr))
	}
	target, terr := detectTargetImage(parsed)
	if terr != nil {
		return nil, misc.Error(misc.ErrSelfDeployBlocked, terr)
	}
	expose := detectExposePort(parsed)

	// Inspect the primary container so we can record the previous
	// image tag (used for rollback).
	cli, cerr := b.d.Client()
	if cerr != nil {
		return nil, misc.Error(misc.ErrSelfDeployBlocked, fmt.Errorf("Docker client unavailable: %v — check Swirl has /var/run/docker.sock mounted", cerr))
	}
	prevImage := ""
	inspectCtx, inspectCancel := context.WithTimeout(ctx, 10*time.Second)
	defer inspectCancel()
	if ctr, _, ierr := b.d.ContainerInspect(inspectCtx, "", selfID); ierr == nil {
		if ctr.Config != nil {
			prevImage = ctr.Config.Image
		}
		if prevImage == "" {
			prevImage = ctr.Image
		}
	}
	if prevImage == "" {
		prevImage = sidekickImageFallback
	}

	job := &SelfDeployJob{
		ID:          createId(),
		CreatedAt:   time.Now().UTC(),
		ComposeYAML: stack.Content, // verbatim — no re-rendering
		Placeholders: SelfDeployJobPlaceholders{
			ImageTag:   target,
			ExposePort: expose,
		},
		PreviousImageTag: prevImage,
		TargetImageTag:   target,
		PrimaryContainer: selfID,
		SourceStackID:    cfg.SourceStackID,
		StackName:        stack.Name,
		TimeoutSec:       cfg.DeployTimeout,
		AutoRollback:     cfg.AutoRollback,
		EnvVars:          envVars,
	}

	// Sanity ping the daemon before the daemon-aware invariant pass.
	pingCtx, pingCancel := context.WithTimeout(ctx, 5*time.Second)
	defer pingCancel()
	if _, perr := cli.Ping(pingCtx); perr != nil {
		return nil, misc.Error(misc.ErrSelfDeployBlocked, fmt.Errorf("Docker daemon not reachable: %v", perr))
	}
	if err := validateInvariantsWithDaemon(ctx, cli, job); err != nil {
		return nil, misc.Error(misc.ErrSelfDeployBlocked, err)
	}

	// Preflight: compare the currently-running stack (start state) with
	// the YAML about to be deployed (target state). Blocks with
	// ErrSelfDeployBlocked when a mismatch would almost certainly break
	// the new deploy (e.g. the Swirl primary references `mongodb` in
	// its env but the target YAML has no `mongodb` service).
	if blockers := b.checkStackCompatibility(ctx, cli, selfID, parsed); len(blockers) > 0 {
		return nil, misc.Error(misc.ErrSelfDeployBlocked, fmt.Errorf("self-deploy blocked: %s", strings.Join(blockers, "; ")))
	}

	// Extra invariant: the Swirl service in the target YAML MUST mount
	// a persistent volume at /data, otherwise the sidekick's state
	// (`/data/self-deploy/state.json`) is lost the moment the new
	// container is created. Without this mount the UI shows "Idle" +
	// "No logs yet" right after a successful deploy, and every further
	// auto-deploy begins from zero state on disk.
	if err := requireDataVolume(parsed); err != nil {
		return nil, misc.Error(misc.ErrSelfDeployBlocked, err)
	}
	return job, nil
}

// requireDataVolume checks that the service whose image contains
// "swirl" declares a volume mount (named or bind) at `/data`. Anonymous
// VOLUME directives from the image do NOT count — they are recreated
// fresh on every container re-create, defeating the self-deploy state
// persistence.
func requireDataVolume(cfg *composetypes.Config) error {
	if cfg == nil {
		return nil
	}
	var swirlSvc *composetypes.ServiceConfig
	for i := range cfg.Services {
		if strings.Contains(strings.ToLower(cfg.Services[i].Image), "swirl") {
			swirlSvc = &cfg.Services[i]
			break
		}
	}
	if swirlSvc == nil {
		return nil
	}
	for _, v := range swirlSvc.Volumes {
		if v.Target == "/data" {
			if v.Type == "bind" || v.Type == "volume" || v.Type == "" || v.Source != "" {
				return nil
			}
		}
	}
	return fmt.Errorf("service %q does not declare a persistent volume at /data — add `volumes: [<name>:/data]` and a top-level `volumes:` entry, otherwise self-deploy state (lock, progress, logs) is lost on every restart", swirlSvc.Name)
}

// checkStackCompatibility inspects the primary container (start state)
// and cross-checks it against the services declared in the target
// compose file (target state). Returns the list of human-readable
// blockers — empty when the deploy is safe to proceed.
//
// Rules enforced:
//   1. Every hostname referenced in the primary's env variables that
//      matches a current project-sibling service MUST also exist as a
//      service in the target YAML. A "peer service disappears"
//      scenario breaks Swirl immediately on restart.
//   2. If the target YAML declares networks, the Swirl service MUST
//      attach to at least one network that also carries each sibling
//      service it depends on. Different-network deployments lose DNS.
//   3. External networks referenced by the target YAML must exist on
//      the daemon (already enforced by validateInvariantsWithDaemon,
//      mirrored here for symmetry).
//
// Non-blockers (warnings only — not surfaced yet): target YAML adding
// new services, removing services the primary never referenced.
func (b *selfDeployBiz) checkStackCompatibility(ctx context.Context, cli *dockerclient.Client, primaryID string, targetCfg *composetypes.Config) []string {
	if targetCfg == nil || cli == nil {
		return nil
	}
	var blockers []string

	// Collect primary env vars.
	ictx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	raw, err := cli.ContainerInspect(ictx, primaryID)
	if err != nil {
		// Can't inspect — skip all runtime comparisons rather than
		// blocking on a transient daemon issue.
		return nil
	}
	primaryEnv := map[string]string{}
	if raw.Config != nil {
		for _, e := range raw.Config.Env {
			if i := strings.Index(e, "="); i > 0 {
				primaryEnv[e[:i]] = e[i+1:]
			}
		}
	}

	// Build a set of services declared in the target YAML, plus a map
	// of which networks each service attaches to.
	targetSvcNets := map[string]map[string]struct{}{}
	for i := range targetCfg.Services {
		svc := &targetCfg.Services[i]
		nets := map[string]struct{}{}
		for n := range svc.Networks {
			nets[n] = struct{}{}
		}
		if len(nets) == 0 {
			nets["default"] = struct{}{}
		}
		targetSvcNets[svc.Name] = nets
	}

	// Find the Swirl service in the target YAML so we can check its
	// network coverage against references from the primary env.
	swirlSvcName := ""
	for name := range targetSvcNets {
		if strings.Contains(strings.ToLower(name), "swirl") {
			swirlSvcName = name
			break
		}
	}
	if swirlSvcName == "" {
		// Fallback: first alphabetical.
		names := make([]string, 0, len(targetSvcNets))
		for n := range targetSvcNets {
			names = append(names, n)
		}
		if len(names) > 0 {
			sortStringsInPlace(names)
			swirlSvcName = names[0]
		}
	}

	// Scan primary env values for `<scheme>://<host>:<port>...`
	// references. For each hostname that matches a target-declared
	// service, verify that swirl + that service share a network.
	for k, v := range primaryEnv {
		hosts := extractEnvHosts(v)
		for _, h := range hosts {
			// Hostname references the container-internal DNS of another
			// compose service only if it's one of our declared services.
			svcNets, ok := targetSvcNets[h]
			if !ok {
				// Either external DNS (e.g. registry.devarch.local),
				// `localhost`, or a typo. We don't flag external DNS.
				continue
			}
			if swirlSvcName == "" {
				continue
			}
			swirlNets := targetSvcNets[swirlSvcName]
			shared := false
			for n := range swirlNets {
				if _, found := svcNets[n]; found {
					shared = true
					break
				}
			}
			if !shared {
				blockers = append(blockers,
					fmt.Sprintf("env %s references service %q but %q and %q share no network in the target YAML", k, h, swirlSvcName, h))
			}
		}
	}

	return blockers
}

// extractEnvHosts pulls hostnames out of URL-looking env values. Rough
// parser — targets the common compose-env shapes `scheme://host:port`,
// `host:port`, `host`. Returns unique hosts, excluding pure numeric
// literals and IPv4 addresses (which would never match a service name).
func extractEnvHosts(v string) []string {
	seen := map[string]struct{}{}
	out := []string{}
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		// Strip path/query after the host — splits on '/' or '?'.
		for i := 0; i < len(s); i++ {
			if s[i] == '/' || s[i] == '?' {
				s = s[:i]
				break
			}
		}
		// Drop port.
		if i := strings.LastIndex(s, ":"); i > 0 {
			s = s[:i]
		}
		// Skip numeric IPs and empty.
		if s == "" || s == "localhost" || s == "127.0.0.1" {
			return
		}
		if allDigitsOrDots(s) {
			return
		}
		if _, dup := seen[s]; dup {
			return
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	// Scan for `scheme://…` first.
	lower := strings.ToLower(v)
	for {
		idx := strings.Index(lower, "://")
		if idx < 0 {
			break
		}
		// Find end of host part: next whitespace or comma.
		rest := v[idx+3:]
		end := len(rest)
		for i := 0; i < len(rest); i++ {
			if rest[i] == ' ' || rest[i] == ',' || rest[i] == ';' {
				end = i
				break
			}
		}
		add(rest[:end])
		v = rest[end:]
		lower = strings.ToLower(v)
	}
	return out
}

func allDigitsOrDots(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && c != '.' {
			return false
		}
	}
	return true
}

func sortStringsInPlace(s []string) {
	for i := 0; i < len(s); i++ {
		for j := i + 1; j < len(s); j++ {
			if s[j] < s[i] {
				s[i], s[j] = s[j], s[i]
			}
		}
	}
}

func (b *selfDeployBiz) TriggerDeploy(ctx context.Context, user web.User) (*SelfDeployJob, error) {
	job, err := b.prepareJob(ctx)
	if err != nil {
		return nil, err
	}
	if user != nil {
		job.CreatedBy = user.Name()
	}

	// Best-effort reclaim: if a previous deploy crashed and left a
	// stale lock, clear it now so this TriggerDeploy can acquire.
	// Safe because reclaimStaleLock refuses to touch a running sidekick.
	if cli, cerr := b.d.Client(); cerr == nil && cli != nil {
		_, _ = reclaimStaleLock(ctx, cli, b.logger)
	}

	// Clear any stale error message on the source stack. A "cannot
	// deploy a stack that includes this Swirl instance" banner from a
	// prior normal-deploy attempt would otherwise linger on the Edit
	// page even after the auto-deploy succeeds. The sidekick never
	// writes back to the stack record, so this is the only natural
	// place to clear it.
	b.clearSourceStackError(ctx, job.SourceStackID)

	release, err := acquireSelfDeployLock()
	if err != nil {
		if errors.Is(err, errLockHeld) {
			return nil, misc.Error(misc.ErrSelfDeployBlocked, errors.New("a self-deploy is already in progress"))
		}
		return nil, err
	}
	spawnOK := false
	defer func() {
		if !spawnOK {
			release()
		}
	}()

	jobPath, err := writeSelfDeployJob(job)
	if err != nil {
		return nil, err
	}
	initState := &SelfDeployState{
		JobID:     job.ID,
		Phase:     SelfDeployPhasePending,
		StartedAt: time.Now().UTC(),
	}
	if werr := writeSelfDeployState(initState); werr != nil {
		return nil, werr
	}

	cli, err := b.d.Client()
	if err != nil {
		return nil, fmt.Errorf("self-deploy: obtain docker client: %w", err)
	}
	// Sweep away exited sidekicks from previous deploys so the daemon
	// doesn't accumulate `swirl-deploy-agent-*` zombies over time.
	b.cleanupExitedSidekicks(ctx)
	if spawnErr := b.spawnSidekick(ctx, cli, job, jobPath); spawnErr != nil {
		failState := &SelfDeployState{
			JobID:      job.ID,
			Phase:      SelfDeployPhaseFailed,
			StartedAt:  initState.StartedAt,
			FinishedAt: time.Now().UTC(),
			Error:      spawnErr.Error(),
		}
		_ = writeSelfDeployState(failState)
		b.eb.CreateSelfDeploy(EventActionSelfDeployFailure, job.ID, job.TargetImageTag, user)
		return nil, spawnErr
	}
	spawnOK = true

	// Watchdog: if the sidekick fails to leave Phase=Pending within the
	// boot window, mark the job Failed so the UI + next /status poll
	// surface a real error instead of an eternal Pending. Runs in a
	// detached goroutine so TriggerDeploy returns immediately.
	go b.watchSidekickBoot(job.ID, initState.StartedAt)

	b.eb.CreateSelfDeploy(EventActionSelfDeployStart, job.ID, job.TargetImageTag, user)
	return job, nil
}

// clearSourceStackError zeroes the ErrorMessage field on the source
// ComposeStack record. Best-effort — failures are logged but never
// propagate, because a failed clear must not block the deploy. Any
// legitimate error from a follow-up compose_stack.Deploy (on a
// different stack, or after the user disables auto-deploy) will
// repopulate the field naturally.
func (b *selfDeployBiz) clearSourceStackError(ctx context.Context, stackID string) {
	if stackID == "" || b.di == nil {
		return
	}
	if err := b.di.ComposeStackUpdateError(ctx, stackID, ""); err != nil && b.logger != nil {
		b.logger.Warnf("self-deploy: could not clear ErrorMessage on source stack %s: %v", stackID, err)
	}
}

// sidekickBootTimeout is how long the primary gives the sidekick to
// move off Phase=Pending before declaring it dead. Long enough that a
// legitimate slow pull of a 100 MB image does not trip it; short
// enough that an operator watching the UI sees the failure within
// coffee-break time.
const sidekickBootTimeout = 90 * time.Second

// watchSidekickBoot runs after spawnSidekick and watches `state.json`
// for a phase transition. If after sidekickBootTimeout the phase is
// still Pending and the same job id is still in flight, the sidekick
// is declared failed and the job is marked accordingly.
//
// The watchdog never fights the sidekick: it only writes if the state
// is unchanged from the initial Pending snapshot. Any forward progress
// by the sidekick silences this check for good.
func (b *selfDeployBiz) watchSidekickBoot(jobID string, startedAt time.Time) {
	select {
	case <-time.After(sidekickBootTimeout):
	}
	st, err := readSelfDeployState()
	if err != nil || st == nil {
		return
	}
	if st.JobID != jobID {
		// Another deploy has replaced this one — not our concern.
		return
	}
	if st.Phase != SelfDeployPhasePending {
		// Sidekick moved on; no action.
		return
	}
	// Double-check via docker: if the sidekick is alive, do not touch.
	if cli, cerr := b.d.Client(); cerr == nil && cli != nil {
		name := sidekickContainerName(jobID)
		ictx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		info, ierr := cli.ContainerInspect(ictx, name)
		cancel()
		if ierr == nil && info.State != nil && info.State.Running {
			return
		}
	}
	now := time.Now().UTC()
	msg := fmt.Sprintf("sidekick did not report status within %s — check container logs of %s", sidekickBootTimeout, sidekickContainerName(jobID))
	failState := &SelfDeployState{
		JobID:      jobID,
		Phase:      SelfDeployPhaseFailed,
		StartedAt:  startedAt,
		FinishedAt: now,
		Error:      "self-deploy: " + msg,
		LogTail:    appendLogLine(st.LogTail, now.Format(time.RFC3339)+" "+msg),
	}
	if werr := writeSelfDeployState(failState); werr != nil && b.logger != nil {
		b.logger.Warnf("self-deploy: watchdog could not rewrite state: %v", werr)
	}
	// Remove the lock so a retry is possible without operator intervention.
	_, _, lockPath := selfDeployPaths()
	_ = os.Remove(lockPath)
	if b.logger != nil {
		b.logger.Warnf("self-deploy: watchdog declared job %s abandoned", jobID)
	}
}

// cleanupExitedSidekicks removes any container matching the
// `swirl-deploy-agent-*` naming convention that is NOT currently
// running. Exited sidekicks retain their logs + exit code (useful for
// post-mortem) but they accumulate over many deploys; this sweep runs
// at Swirl boot and right before every spawn to keep the daemon tidy.
//
// Never touches a running sidekick — only `exited`, `dead`, or
// `created`-but-never-started are removed.
func (b *selfDeployBiz) cleanupExitedSidekicks(ctx context.Context) {
	cli, err := b.d.Client()
	if err != nil || cli == nil {
		return
	}
	list, err := cli.ContainerList(ctx, dockercontainer.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("label", selfDeployLabelRole+"="+selfDeployLabelRoleAgent)),
	})
	if err != nil {
		return
	}
	for _, c := range list {
		if c.State == "running" {
			continue
		}
		removeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		rerr := cli.ContainerRemove(removeCtx, c.ID, dockercontainer.RemoveOptions{Force: true, RemoveVolumes: true})
		cancel()
		if rerr != nil && b.logger != nil {
			name := c.ID[:12]
			if len(c.Names) > 0 {
				name = strings.TrimPrefix(c.Names[0], "/")
			}
			b.logger.Warnf("self-deploy: could not remove exited sidekick %s: %v", name, rerr)
		}
	}
}

// spawnSidekick creates and starts the deploy-agent container. Logic
// is identical to v2 — kept here because the sidekick lifecycle is
// the core of the feature and MUST remain aligned with the state on
// disk written by writeSelfDeployJob / writeSelfDeployState.
func (b *selfDeployBiz) spawnSidekick(ctx context.Context, cli *dockerclient.Client, job *SelfDeployJob, jobPath string) error {
	sidekickImage := job.PreviousImageTag
	if sidekickImage == "" {
		sidekickImage = sidekickImageFallback
	}

	name := sidekickContainerName(job.ID)

	env := []string{
		"SWIRL_SELF_DEPLOY_JOB=" + jobPath,
	}

	// ENTRYPOINT of the Swirl image is `/app/swirl`. Passing only
	// `deploy-agent` in Cmd makes Docker execute `/app/swirl deploy-agent`,
	// which main.go dispatches to the sidekick binary. Do NOT prefix
	// with `/swirl` — that used to double the binary path and Swirl
	// booted as the full server (attempting MongoDB at localhost:27017).
	ccfg := &dockercontainer.Config{
		Image:     sidekickImage,
		Cmd:       []string{"deploy-agent"},
		Env:       env,
		Tty:       false,
		OpenStdin: false,
		Labels: map[string]string{
			selfDeployLabelRole: selfDeployLabelRoleAgent,
			selfDeployLabelJob:  job.ID,
		},
	}

	dataMount, err := b.resolvePrimaryDataMount(ctx, cli, job.PrimaryContainer)
	if err != nil {
		return fmt.Errorf("self-deploy: resolve primary /data mount: %w", err)
	}

	hcfg := &dockercontainer.HostConfig{
		NetworkMode: dockercontainer.NetworkMode("host"),
		AutoRemove:  false,
		Mounts: []dockermount.Mount{
			{
				Type:   dockermount.TypeBind,
				Source: "/var/run/docker.sock",
				Target: "/var/run/docker.sock",
			},
			dataMount,
		},
		RestartPolicy: dockercontainer.RestartPolicy{Name: dockercontainer.RestartPolicyDisabled},
	}

	createCtx, createCancel := context.WithTimeout(ctx, 30*time.Second)
	defer createCancel()
	resp, err := cli.ContainerCreate(createCtx, ccfg, hcfg, nil, nil, name)
	if err != nil {
		if strings.Contains(err.Error(), "No such image") || strings.Contains(err.Error(), "not found") {
			pullCtx, pullCancel := context.WithTimeout(ctx, 5*time.Minute)
			defer pullCancel()
			rc, perr := cli.ImagePull(pullCtx, sidekickImage, dockerimage.PullOptions{})
			if perr != nil {
				return fmt.Errorf("self-deploy: pull sidekick image %s: %w", sidekickImage, perr)
			}
			_, _ = io.Copy(io.Discard, rc)
			_ = rc.Close()
			resp, err = cli.ContainerCreate(createCtx, ccfg, hcfg, nil, nil, name)
		}
		if err != nil {
			return fmt.Errorf("self-deploy: create sidekick container: %w", err)
		}
	}

	startCtx, startCancel := context.WithTimeout(ctx, 30*time.Second)
	defer startCancel()
	if err := cli.ContainerStart(startCtx, resp.ID, dockercontainer.StartOptions{}); err != nil {
		_ = cli.ContainerRemove(ctx, resp.ID, dockercontainer.RemoveOptions{Force: true})
		return fmt.Errorf("self-deploy: start sidekick container: %w", err)
	}

	b.logger.Infof("self-deploy: sidekick %s spawned (container %s, image %s, job %s, stack %s)", name, resp.ID[:12], sidekickImage, job.ID, job.StackName)
	return nil
}

// resolvePrimaryDataMount inspects the primary Swirl container and
// returns a mount.Mount that the sidekick attaches so it sees the SAME
// /data directory — required for the sidekick to read job.json /
// write state.json on the shared volume.
func (b *selfDeployBiz) resolvePrimaryDataMount(ctx context.Context, cli *dockerclient.Client, primaryID string) (dockermount.Mount, error) {
	inspectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	raw, err := cli.ContainerInspect(inspectCtx, primaryID)
	if err != nil {
		return dockermount.Mount{}, fmt.Errorf("inspect primary %s: %w", primaryID, err)
	}
	for _, m := range raw.Mounts {
		if m.Destination != "/data" {
			continue
		}
		switch m.Type {
		case "volume":
			return dockermount.Mount{
				Type:   dockermount.TypeVolume,
				Source: m.Name,
				Target: "/data",
			}, nil
		case "bind":
			return dockermount.Mount{
				Type:   dockermount.TypeBind,
				Source: m.Source,
				Target: "/data",
			}, nil
		}
	}
	return dockermount.Mount{}, fmt.Errorf("primary container %s has no /data mount; self-deploy requires a persistent volume at /data", primaryID)
}

func (b *selfDeployBiz) Status(ctx context.Context) (*SelfDeployStatus, error) {
	st, err := readSelfDeployState()
	if err != nil {
		return nil, err
	}
	if st == nil {
		return &SelfDeployStatus{Phase: "idle"}, nil
	}
	b.publishTerminalEvent(st)

	out := &SelfDeployStatus{
		Phase:   st.Phase,
		JobID:   st.JobID,
		Error:   st.Error,
		LogTail: st.LogTail,
	}

	// Enrich with sidekick container info. Only meaningful while the
	// job is in flight (Pending/Stopping/…/Recovery). For terminal
	// phases we still surface the container name so the UI can link
	// to its logs post-mortem.
	if st.JobID != "" {
		name := sidekickContainerName(st.JobID)
		out.SidekickContainer = name
		alive := false
		if cli, cerr := b.d.Client(); cerr == nil && cli != nil {
			inspectCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			info, ierr := cli.ContainerInspect(inspectCtx, name)
			cancel()
			if ierr == nil {
				if info.State != nil && info.State.Running {
					alive = true
				}
				// Tail sidekick logs regardless of running state — when the
				// sidekick crashed without writing state.json, this is the
				// only way operators see what happened.
				out.SidekickLogs = fetchSidekickLogs(ctx, cli, name)
			}
		}
		out.SidekickAlive = alive
		if !alive && isInProgressPhase(st.Phase) {
			out.CanReset = true
		}
	}
	return out, nil
}

// sidekickContainerName returns the deterministic name the biz layer
// uses when spawning the sidekick for a given job id. Kept as a
// package-level helper so Status + ResetLock + spawnSidekick agree
// on the exact naming convention.
func sidekickContainerName(jobID string) string {
	if len(jobID) > 8 {
		jobID = jobID[:8]
	}
	return "swirl-deploy-agent-" + jobID
}

// isInProgressPhase returns true for phases that imply the sidekick
// should still be working. Used by the stale-lock reclaim path.
func isInProgressPhase(phase string) bool {
	switch phase {
	case SelfDeployPhasePending,
		SelfDeployPhaseStopping,
		SelfDeployPhasePulling,
		SelfDeployPhaseStarting,
		SelfDeployPhaseHealthCheck,
		SelfDeployPhaseRecovery:
		return true
	}
	return false
}

// fetchSidekickLogs reads the last ~200 lines of the sidekick
// container's logs via the Docker daemon. Errors are swallowed — the
// UI is better off seeing an empty log than a broken status response.
func fetchSidekickLogs(ctx context.Context, cli *dockerclient.Client, name string) string {
	logCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	rc, err := cli.ContainerLogs(logCtx, name, dockercontainer.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Tail:       "200",
		Timestamps: false,
	})
	if err != nil {
		return ""
	}
	defer rc.Close()
	// Docker multiplexes stdout/stderr with an 8-byte header per frame
	// when the container has no TTY. For a readable tail, strip the
	// header bytes. Implementation mirrors what the official docker CLI
	// does with stdcopy.StdCopy, but we write into a single buffer so
	// both streams are preserved in order.
	var buf strings.Builder
	header := make([]byte, 8)
	for {
		_, err := io.ReadFull(rc, header)
		if err != nil {
			break
		}
		size := int64(header[4])<<24 | int64(header[5])<<16 | int64(header[6])<<8 | int64(header[7])
		if size <= 0 {
			continue
		}
		if size > 1<<20 {
			// Sanity cap: skip overly large frames rather than allocate.
			_, _ = io.CopyN(io.Discard, rc, size)
			continue
		}
		chunk := make([]byte, size)
		if _, rerr := io.ReadFull(rc, chunk); rerr != nil {
			break
		}
		buf.Write(chunk)
	}
	return buf.String()
}

// ResetLock implements the public entry point for operator-triggered
// stale-lock clearing. It double-checks that the sidekick is NOT
// currently running (a misuse would abort an in-flight deploy), then
// calls the shared reclaim path. Emits an audit event regardless of
// outcome.
func (b *selfDeployBiz) ResetLock(ctx context.Context, user web.User) (bool, error) {
	cli, _ := b.d.Client()
	if cli != nil {
		// Refuse if the expected sidekick is actually running.
		if job, _ := readSelfDeployJob(); job != nil && job.ID != "" {
			name := sidekickContainerName(job.ID)
			inspectCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			info, ierr := cli.ContainerInspect(inspectCtx, name)
			cancel()
			if ierr == nil && info.State != nil && info.State.Running {
				return false, misc.Error(misc.ErrSelfDeployBlocked, errors.New("sidekick is still running — wait for it or docker rm -f the container first"))
			}
		}
	}
	reclaimed, err := reclaimStaleLock(ctx, cli, b.logger)
	if err != nil {
		return false, err
	}
	if b.eb != nil {
		jobID := ""
		if job, _ := readSelfDeployJob(); job != nil {
			jobID = job.ID
		}
		b.eb.CreateSelfDeploy(EventActionSelfDeployReset, jobID, "", user)
	}
	return reclaimed, nil
}

// publishTerminalEvent is the idempotent audit-emission helper.
func (b *selfDeployBiz) publishTerminalEvent(st *SelfDeployState) {
	if st == nil || b.eb == nil {
		return
	}
	if st.EventPublished {
		return
	}
	var action EventAction
	switch st.Phase {
	case SelfDeployPhaseSuccess:
		action = EventActionSelfDeploySuccess
	case SelfDeployPhaseFailed, SelfDeployPhaseRecovery, SelfDeployPhaseRolledBack:
		action = EventActionSelfDeployFailure
	default:
		return
	}

	imageTag := ""
	if job, err := readSelfDeployJob(); err == nil && job != nil {
		imageTag = job.TargetImageTag
	}
	b.eb.CreateSelfDeploy(action, st.JobID, imageTag, nil)

	st.EventPublished = true
	if err := writeSelfDeployState(st); err != nil {
		if b.logger != nil {
			b.logger.Warnf("self-deploy: could not mark event published for job %s: %v", st.JobID, err)
		}
	}
}

// ValidateSelfDeployJob is the public entry point for the pure
// (structural-only) invariant check. The sidekick calls this right
// before runDeploy as a double-check.
func ValidateSelfDeployJob(job *SelfDeployJob) error {
	return validateInvariants(job)
}

// sidekickContainerNameRe matches container names that collide with
// the sidekick naming convention (`swirl-deploy-agent-<short>`).
var sidekickContainerNameRe = regexp.MustCompile(`(?i)^swirl-deploy-agent(-|$)`)

// validateInvariants is the pure form: structural checks only.
func validateInvariants(job *SelfDeployJob) error {
	if job == nil {
		return errors.New("self-deploy: nil job")
	}
	if strings.TrimSpace(job.PrimaryContainer) == "" {
		return errors.New("self-deploy: PrimaryContainer is empty")
	}
	if strings.TrimSpace(job.TargetImageTag) == "" {
		return errors.New("self-deploy: TargetImageTag is empty")
	}
	if strings.TrimSpace(job.ComposeYAML) == "" {
		return errors.New("self-deploy: ComposeYAML is empty")
	}
	if strings.TrimSpace(job.StackName) == "" {
		return errors.New("self-deploy: StackName is empty")
	}
	cfg, err := compose.Parse("self-deploy-validate", job.ComposeYAML)
	if err != nil {
		return fmt.Errorf("self-deploy: YAML is invalid: %w", err)
	}
	if cfg != nil {
		for _, svc := range cfg.Services {
			if svc.ContainerName != "" && sidekickContainerNameRe.MatchString(svc.ContainerName) {
				return fmt.Errorf("self-deploy: service %q uses container_name %q which collides with the sidekick naming pattern (swirl-deploy-agent-*); rename the service", svc.Name, svc.ContainerName)
			}
		}
	}
	return nil
}

// validateInvariantsWithDaemon extends the pure invariants with checks
// that need a live Docker client.
func validateInvariantsWithDaemon(ctx context.Context, cli *dockerclient.Client, job *SelfDeployJob) error {
	if err := validateInvariants(job); err != nil {
		return err
	}
	if cli == nil {
		return errors.New("self-deploy: nil docker client")
	}
	inspectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if _, err := cli.ContainerInspect(inspectCtx, job.PrimaryContainer); err != nil {
		if errdefs.IsNotFound(err) {
			return fmt.Errorf("self-deploy: PrimaryContainer %q not found on daemon — SWIRL_CONTAINER_ID might be stale", job.PrimaryContainer)
		}
		return fmt.Errorf("self-deploy: inspect PrimaryContainer %q: %w", job.PrimaryContainer, err)
	}

	cfg, err := compose.Parse("self-deploy-validate-daemon", job.ComposeYAML)
	if err != nil {
		return fmt.Errorf("self-deploy: YAML is invalid: %w", err)
	}
	if cfg == nil {
		return nil
	}
	if err := validateExternalNetworks(ctx, cli, cfg.Networks); err != nil {
		return err
	}
	if err := validateExternalVolumes(ctx, cli, cfg.Volumes); err != nil {
		return err
	}
	return nil
}

// validateExternalNetworks checks that every external network exists.
func validateExternalNetworks(ctx context.Context, cli *dockerclient.Client, nets map[string]composetypes.NetworkConfig) error {
	for name, ncfg := range nets {
		if !ncfg.External.External {
			continue
		}
		resolved := ncfg.Name
		if resolved == "" {
			resolved = name
		}
		inspectCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		_, err := cli.NetworkInspect(inspectCtx, resolved, dockernetwork.InspectOptions{})
		cancel()
		if err != nil {
			if errdefs.IsNotFound(err) {
				return fmt.Errorf("self-deploy: external network %q referenced in compose does not exist; run `docker network create %s` before deploying", resolved, resolved)
			}
			return fmt.Errorf("self-deploy: inspect external network %q: %w", resolved, err)
		}
	}
	return nil
}

// validateExternalVolumes mirrors validateExternalNetworks for volumes.
func validateExternalVolumes(ctx context.Context, cli *dockerclient.Client, vols map[string]composetypes.VolumeConfig) error {
	for name, vcfg := range vols {
		if !vcfg.External.External {
			continue
		}
		resolved := vcfg.Name
		if resolved == "" {
			resolved = name
		}
		inspectCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		_, err := cli.VolumeInspect(inspectCtx, resolved)
		cancel()
		if err != nil {
			if errdefs.IsNotFound(err) {
				return fmt.Errorf("self-deploy: external volume %q referenced in compose does not exist; run `docker volume create %s` before deploying", resolved, resolved)
			}
			return fmt.Errorf("self-deploy: inspect external volume %q: %w", resolved, err)
		}
	}
	return nil
}

// Docker label keys reserved for the self-deploy feature.
const (
	selfDeployLabelRole      = "com.swirl.self-deploy"
	selfDeployLabelRoleAgent = "agent"
	selfDeployLabelJob       = "com.swirl.self-deploy.job"
)
