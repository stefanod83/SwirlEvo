package biz

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
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
}

// SelfDeployConfig is the persisted settings blob for the self-deploy
// feature (v3).
//
// Retrocompat: older records that still carry the v1/v2 fields
// (`template`, `placeholders`) unmarshal cleanly — json.Unmarshal
// drops unknown keys. No migration is required.
type SelfDeployConfig struct {
	Enabled       bool     `json:"enabled"`
	SourceStackID string   `json:"sourceStackId"`
	AutoRollback  bool     `json:"autoRollback"`
	DeployTimeout int      `json:"deployTimeout"` // seconds
	RecoveryPort  int      `json:"recoveryPort"`
	RecoveryAllow []string `json:"recoveryAllow"`
}

// SelfDeployStatus is the snapshot surfaced by the Status endpoint.
type SelfDeployStatus struct {
	Phase   string   `json:"phase"`
	JobID   string   `json:"jobId,omitempty"`
	Error   string   `json:"error,omitempty"`
	LogTail []string `json:"logTail,omitempty"`
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

// NewSelfDeploy is the DI constructor.
func NewSelfDeploy(sb SettingBiz, csb ComposeStackBiz, d *docker.Docker, eb EventBiz, di dao.Interface) SelfDeployBiz {
	return &selfDeployBiz{sb: sb, csb: csb, d: d, eb: eb, di: di, logger: log.Get("self-deploy")}
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
	if cfg.RecoveryPort <= 0 {
		cfg.RecoveryPort = misc.SelfDeployRecoveryPort
	}
	if len(cfg.RecoveryAllow) == 0 {
		cfg.RecoveryAllow = []string{misc.SelfDeployDefaultRecoveryCIDR}
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
	// Normalise recovery_allow: strip empties, de-dup whitespace.
	allow := make([]string, 0, len(cfg.RecoveryAllow))
	for _, c := range cfg.RecoveryAllow {
		if s := strings.TrimSpace(c); s != "" {
			allow = append(allow, s)
		}
	}
	cfg.RecoveryAllow = allow

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
		return nil, errors.New("self-deploy: compose stack biz not wired")
	}
	stack, err := b.csb.Find(ctx, cfg.SourceStackID)
	if err != nil {
		return nil, fmt.Errorf("self-deploy: find source stack: %w", err)
	}
	if stack == nil {
		return nil, errors.New("self-deploy: source stack not found or deleted")
	}
	yaml := strings.TrimSpace(stack.Content)
	if yaml == "" {
		return nil, errors.New("self-deploy: source stack has no compose content")
	}
	parsed, perr := compose.Parse(stack.Name, stack.Content)
	if perr != nil {
		return nil, fmt.Errorf("self-deploy: parse source stack YAML: %w", perr)
	}
	target, terr := detectTargetImage(parsed)
	if terr != nil {
		return nil, terr
	}
	expose := detectExposePort(parsed)

	// Inspect the primary container so we can record the previous
	// image tag (used for rollback).
	cli, cerr := b.d.Client()
	if cerr != nil {
		return nil, fmt.Errorf("self-deploy: obtain docker client: %w", cerr)
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

	allow := cfg.RecoveryAllow
	if len(allow) == 0 {
		allow = []string{misc.SelfDeployDefaultRecoveryCIDR}
	}
	recPort := cfg.RecoveryPort
	if recPort <= 0 {
		recPort = misc.SelfDeployRecoveryPort
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
		RecoveryPort:     recPort,
		RecoveryAllow:    allow,
		TimeoutSec:       cfg.DeployTimeout,
		AutoRollback:     cfg.AutoRollback,
	}

	// Sanity ping the daemon before the daemon-aware invariant pass.
	pingCtx, pingCancel := context.WithTimeout(ctx, 5*time.Second)
	defer pingCancel()
	if _, perr := cli.Ping(pingCtx); perr != nil {
		return nil, fmt.Errorf("self-deploy: docker daemon not reachable: %w", perr)
	}
	if err := validateInvariantsWithDaemon(ctx, cli, job); err != nil {
		return nil, err
	}
	return job, nil
}

func (b *selfDeployBiz) TriggerDeploy(ctx context.Context, user web.User) (*SelfDeployJob, error) {
	job, err := b.prepareJob(ctx)
	if err != nil {
		return nil, err
	}
	if user != nil {
		job.CreatedBy = user.Name()
	}

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

	b.eb.CreateSelfDeploy(EventActionSelfDeployStart, job.ID, job.TargetImageTag, user)
	return job, nil
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

	shortID := job.ID
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}
	name := "swirl-deploy-agent-" + shortID

	env := []string{
		"SWIRL_SELF_DEPLOY_JOB=" + jobPath,
		"SWIRL_RECOVERY_PORT=" + strconv.Itoa(job.RecoveryPort),
		"SWIRL_RECOVERY_ALLOW=" + strings.Join(job.RecoveryAllow, ","),
	}

	ccfg := &dockercontainer.Config{
		Image:     sidekickImage,
		Cmd:       []string{"/swirl", "deploy-agent"},
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

func (b *selfDeployBiz) Status(_ context.Context) (*SelfDeployStatus, error) {
	st, err := readSelfDeployState()
	if err != nil {
		return nil, err
	}
	if st == nil {
		return &SelfDeployStatus{Phase: "idle"}, nil
	}
	b.publishTerminalEvent(st)
	return &SelfDeployStatus{
		Phase:   st.Phase,
		JobID:   st.JobID,
		Error:   st.Error,
		LogTail: st.LogTail,
	}, nil
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
	if job.RecoveryPort != 0 && job.Placeholders.ExposePort != 0 && job.RecoveryPort == job.Placeholders.ExposePort {
		return fmt.Errorf("self-deploy: RecoveryPort (%d) must differ from ExposePort", job.RecoveryPort)
	}
	if cfg != nil {
		for _, svc := range cfg.Services {
			if svc.ContainerName != "" && sidekickContainerNameRe.MatchString(svc.ContainerName) {
				return fmt.Errorf("self-deploy: service %q uses container_name %q which collides with the sidekick naming pattern (swirl-deploy-agent-*); rename the service", svc.Name, svc.ContainerName)
			}
		}
	}
	for _, cidr := range job.RecoveryAllow {
		if strings.TrimSpace(cidr) == "0.0.0.0/0" {
			log.Get("self-deploy").Warnf("self-deploy: RecoveryAllow includes 0.0.0.0/0 — recovery UI will accept connections from ANY source; ensure the port is firewalled")
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
