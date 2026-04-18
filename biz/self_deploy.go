package biz

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cuigh/auxo/log"
	"github.com/cuigh/auxo/net/web"
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

// selfDeployInspect is the test seam for inspectCurrentContainer. It defaults
// to delegating to b.d.ContainerInspect, but tests swap it out to return a
// canned InspectResponse without needing a real Docker daemon. Centralising
// the override in one place keeps the production call site simple.
var selfDeployInspect = func(ctx context.Context, b *selfDeployBiz, selfID string) (dockercontainer.InspectResponse, error) {
	if b == nil || b.d == nil {
		return dockercontainer.InspectResponse{}, errors.New("self-deploy: nil docker client")
	}
	ctr, _, err := b.d.ContainerInspect(ctx, "", selfID)
	return ctr, err
}

// SelfDeployBiz is the orchestration surface for the self-deploy
// feature. Phase 3 wires every method end-to-end except the sidekick
// lifecycle itself — the sidekick side lands in Phase 4.
type SelfDeployBiz interface {
	// Preview renders the currently persisted (or, absent that, the
	// seed) compose template with the persisted (or default)
	// placeholders, validates the result with compose.Parse +
	// validateServices, and returns the YAML string.
	Preview(ctx context.Context) (string, error)

	// LoadConfig returns the persisted self-deploy configuration,
	// falling back to sensible defaults when nothing has been saved.
	LoadConfig(ctx context.Context) (*SelfDeployConfig, error)

	// SaveConfig persists the self-deploy configuration after
	// validating the template renders cleanly.
	SaveConfig(ctx context.Context, cfg *SelfDeployConfig, user web.User) error

	// PrepareJob assembles the job descriptor the sidekick consumes.
	// Pure inspection — does NOT spawn anything and does NOT touch the
	// filesystem. Exists so the UI can preview every field of the job
	// before committing to TriggerDeploy.
	PrepareJob(ctx context.Context) (*SelfDeployJob, error)

	// TriggerDeploy persists the job on the shared volume, acquires
	// the single-deploy lock, and spawns the sidekick container.
	// Returns the job descriptor the caller can surface as a 202
	// Accepted payload.
	TriggerDeploy(ctx context.Context, user web.User) (*SelfDeployJob, error)

	// Status returns the most recent deploy snapshot read from the
	// shared volume. Lightweight — does not touch the Docker daemon.
	Status(ctx context.Context) (*SelfDeployStatus, error)
}

// SelfDeployConfig is the persisted settings blob for the self-deploy
// feature. Mirrors the `SelfDeploy` group declared in misc.Setting; the
// duplication is intentional — the biz layer hands the operator this
// concrete type, while the setting layer stores it as a nested sub-tree
// on the Setting struct. Keeping them in sync is a single `go vet`-safe
// mapping (matching JSON tags).
type SelfDeployConfig struct {
	Enabled       bool                   `json:"enabled"`
	Template      string                 `json:"template"`
	Placeholders  SelfDeployPlaceholders `json:"placeholders"`
	AutoRollback  bool                   `json:"autoRollback"`
	DeployTimeout int                    `json:"deployTimeout"`
}

// SelfDeployStatus is the snapshot surfaced by the Status endpoint.
// Trims fields that are only useful to the sidekick itself (StartedAt,
// FinishedAt, full log buffer).
type SelfDeployStatus struct {
	Phase   string   `json:"phase"`
	JobID   string   `json:"jobId,omitempty"`
	Error   string   `json:"error,omitempty"`
	LogTail []string `json:"logTail,omitempty"`
}

// ErrSelfDeployNotImplemented is preserved for API-compatibility with
// callers that checked it against a sentinel during Phase 2. Phase 3
// replaces every return site with real logic, so the error is no longer
// produced — but the exported symbol stays so external code keeps
// compiling.
var ErrSelfDeployNotImplemented = errors.New("self-deploy: not implemented yet")

// settingIDSelfDeploy is the dao.Setting primary key used to persist
// the self-deploy config. Kept private — callers access the blob via
// SelfDeployBiz.LoadConfig / SaveConfig, not by round-tripping through
// SettingBiz.
const settingIDSelfDeploy = "self_deploy"

// sidekickImageFallback is used when the primary container's image
// cannot be resolved via the daemon (e.g. the image was deleted after
// the container was started). It keeps the default hardcoded so
// PrepareJob can still produce a meaningful error instead of panicking.
const sidekickImageFallback = misc.SelfDeployImageTag

// NewSelfDeploy is the DI constructor. Phase 3 wires the real
// dependencies (SettingBiz for persistence, *docker.Docker for the
// daemon spawn, EventBiz for audit).
func NewSelfDeploy(sb SettingBiz, d *docker.Docker, eb EventBiz) SelfDeployBiz {
	return &selfDeployBiz{sb: sb, d: d, eb: eb, logger: log.Get("self-deploy")}
}

type selfDeployBiz struct {
	sb     SettingBiz
	d      *docker.Docker
	eb     EventBiz
	logger log.Logger
}

// Preview mirrors Phase 2's contract but reads the persisted template +
// placeholders first. Falls back to the seed template / DefaultPlaceholders
// when the config is absent or empty.
func (b *selfDeployBiz) Preview(ctx context.Context) (string, error) {
	cfg, err := b.LoadConfig(ctx)
	if err != nil {
		return "", err
	}
	tmpl := cfg.Template
	if strings.TrimSpace(tmpl) == "" {
		tmpl = LoadSeedTemplate()
	}
	yaml, err := RenderTemplate(tmpl, cfg.Placeholders)
	if err != nil {
		return "", err
	}
	// Run the rendered YAML through the standalone parser so any
	// structural error (unsupported `build:`, missing image, bad port
	// mapping) surfaces in the operator's Preview instead of only at
	// deploy time.
	if _, err := compose.Parse("self-deploy-preview", yaml); err != nil {
		return "", fmt.Errorf("self-deploy: rendered YAML is not a valid compose file: %w", err)
	}
	return yaml, nil
}

func (b *selfDeployBiz) LoadConfig(ctx context.Context) (*SelfDeployConfig, error) {
	raw, err := b.sb.Find(ctx, settingIDSelfDeploy)
	if err != nil {
		return nil, fmt.Errorf("self-deploy: load config: %w", err)
	}
	firstTime := raw == nil
	cfg := &SelfDeployConfig{}
	if raw != nil {
		// SettingBiz.Find returns the decoded JSON as map[string]interface{};
		// re-marshal + unmarshal to hydrate our typed struct. The round-trip
		// is cheap (the payload is at most a few KiB) and keeps the biz
		// layer agnostic to whether the DAO stored JSON or BSON.
		if buf, merr := json.Marshal(raw); merr == nil {
			_ = json.Unmarshal(buf, cfg)
		}
	}
	return b.applyConfigDefaults(ctx, cfg, firstTime), nil
}

// applyConfigDefaults fills zero-valued fields of cfg with the planning
// defaults. Centralised so both Load and Save run the same normalisation
// — a newly saved record with an empty template does not silently
// deviate from a freshly-loaded default one.
//
// When firstTime is true (no persisted config yet), the biz layer also
// inspects the currently-running Swirl container and merges the detected
// values into cfg.Placeholders BEFORE applying static defaults. This
// gives the operator a UI pre-filled with their actual deployment — the
// zero-friction bootstrap path described by the self-deploy v1.1 plan.
func (b *selfDeployBiz) applyConfigDefaults(ctx context.Context, cfg *SelfDeployConfig, firstTime bool) *SelfDeployConfig {
	if cfg == nil {
		cfg = &SelfDeployConfig{}
	}
	if strings.TrimSpace(cfg.Template) == "" {
		cfg.Template = LoadSeedTemplate()
	}
	if firstTime {
		detected := b.inspectCurrentContainer(ctx)
		cfg.Placeholders = mergeDetected(cfg.Placeholders, detected)
	}
	cfg.Placeholders = mergeWithDefaults(cfg.Placeholders)
	if cfg.DeployTimeout <= 0 {
		cfg.DeployTimeout = misc.SelfDeployDefaultTimeoutSec
	}
	// Default AutoRollback = true per the "default decisions" in the
	// Phase 3 brief. Only apply the default when the incoming record
	// was never explicitly written (i.e. came from applyConfigDefaults
	// on the zero-value config). We cannot tell whether a stored
	// `false` was intentional or missing — err on the safe side and
	// leave explicit `false` values alone; the UI ships with the
	// checkbox pre-ticked.
	return cfg
}

// inspectCurrentContainer inspects the currently-running Swirl container
// and returns a SelfDeployPlaceholders pre-filled with values extracted
// from it. Used the first time LoadConfig runs (no persisted config) so
// the operator sees a sensible snapshot of the current deployment in the
// Settings UI — zero-friction bootstrap.
//
// Graceful degradation: if SelfContainerID, the test seam
// selfDeployInspect, or any access to the InspectResponse fails, a
// zero-value SelfDeployPlaceholders is returned and the caller falls
// back to static defaults. This method MUST NEVER panic — unit tests
// instantiate selfDeployBiz with a nil docker client.
//
// Uses a 5-second timeout so a slow / stuck daemon does not block the
// Settings page load.
func (b *selfDeployBiz) inspectCurrentContainer(ctx context.Context) SelfDeployPlaceholders {
	var zero SelfDeployPlaceholders
	selfID, ok := misc.SelfContainerID()
	if !ok || strings.TrimSpace(selfID) == "" {
		return zero
	}
	inspectCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ctr, err := selfDeployInspect(inspectCtx, b, selfID)
	if err != nil {
		return zero
	}

	out := SelfDeployPlaceholders{}

	// ImageTag: prefer the canonical ref baked in Config.Image;
	// fall back to the resolved digest-typed InspectResponse.Image
	// when the container was spawned with an image that no longer
	// resolves to a tag.
	if ctr.Config != nil {
		out.ImageTag = strings.TrimSpace(ctr.Config.Image)
	}
	if out.ImageTag == "" {
		out.ImageTag = strings.TrimSpace(ctr.Image)
	}

	// ContainerName: Docker prefixes container names with "/" in
	// the inspect response (historical convention). Strip it so the
	// placeholder matches the compose emitted form.
	out.ContainerName = strings.TrimPrefix(ctr.Name, "/")

	// ExposePort: scan HostConfig.PortBindings for the first entry
	// with non-empty bindings. `nat.PortMap` is map[nat.Port][]nat.PortBinding.
	// We pick the first published HostPort as the operator's chosen
	// expose value. Iteration order is not deterministic, so when
	// multiple ports are bound the operator can still edit the
	// detected value.
	if ctr.HostConfig != nil {
		for _, bindings := range ctr.HostConfig.PortBindings {
			if len(bindings) == 0 {
				continue
			}
			for _, bnd := range bindings {
				if bnd.HostPort == "" {
					continue
				}
				if p, perr := strconv.Atoi(bnd.HostPort); perr == nil && p > 0 {
					out.ExposePort = p
					break
				}
			}
			if out.ExposePort != 0 {
				break
			}
		}
	}

	// VolumeData: walk the Mounts slice and pick the first volume
	// mount targetting /data (the canonical Swirl data dir). If
	// /data is a bind mount or tmpfs, VolumeData stays empty and
	// the operator receives the static default.
	for _, m := range ctr.Mounts {
		if m.Type == dockermount.TypeVolume && m.Destination == "/data" {
			out.VolumeData = m.Name
			// m.Name is authoritative for named volumes; fall back
			// to m.Source (the actual path on disk) only if Name
			// is somehow empty.
			if out.VolumeData == "" {
				out.VolumeData = m.Source
			}
			break
		}
	}

	// NetworkName: pick the first non-default network. Docker's
	// defaults are bridge / host / none — any other name is either
	// a user-created network or a compose-managed one, both of
	// which are valid targets for the placeholder.
	if ctr.NetworkSettings != nil {
		for name := range ctr.NetworkSettings.Networks {
			switch name {
			case "bridge", "host", "none":
				continue
			}
			out.NetworkName = name
			break
		}
	}

	// TraefikLabels: filter labels by the Traefik HTTP provider
	// prefixes. `traefik.docker.*` is excluded because those keys
	// configure the Docker provider itself (e.g. network), not a
	// routing rule, and an operator pasting them into the compose
	// template would get duplicate keys. Output format is
	// `key=value` per entry, sorted for deterministic rendering.
	if ctr.Config != nil && len(ctr.Config.Labels) > 0 {
		for k, v := range ctr.Config.Labels {
			if !isTraefikRoutingLabel(k) {
				continue
			}
			out.TraefikLabels = append(out.TraefikLabels, k+"="+v)
		}
		if len(out.TraefikLabels) > 1 {
			sort.Strings(out.TraefikLabels)
		}
	}

	return out
}

// isTraefikRoutingLabel returns true when the label key configures a
// Traefik routing rule that should flow into the self-deploy
// placeholders. Accepts the routers/services/middlewares namespaces,
// the master `traefik.enable` switch, and the `traefik.tcp.*`
// subtree. Explicitly rejects `traefik.docker.*` (Docker-provider
// internals — not a routing rule).
func isTraefikRoutingLabel(key string) bool {
	if key == "traefik.enable" {
		return true
	}
	if strings.HasPrefix(key, "traefik.docker.") {
		return false
	}
	if strings.HasPrefix(key, "traefik.http.routers.") ||
		strings.HasPrefix(key, "traefik.http.services.") ||
		strings.HasPrefix(key, "traefik.http.middlewares.") ||
		strings.HasPrefix(key, "traefik.tcp.") {
		return true
	}
	return false
}

// mergeDetected copies non-zero fields from detected into current ONLY
// where current is still at its zero value. Ensures auto-detection
// never clobbers a value the operator has already set — for example on
// a re-detect triggered after a partial save.
func mergeDetected(current, detected SelfDeployPlaceholders) SelfDeployPlaceholders {
	if strings.TrimSpace(current.ImageTag) == "" && strings.TrimSpace(detected.ImageTag) != "" {
		current.ImageTag = detected.ImageTag
	}
	if current.ExposePort == 0 && detected.ExposePort != 0 {
		current.ExposePort = detected.ExposePort
	}
	if strings.TrimSpace(current.ContainerName) == "" && strings.TrimSpace(detected.ContainerName) != "" {
		current.ContainerName = detected.ContainerName
	}
	if strings.TrimSpace(current.VolumeData) == "" && strings.TrimSpace(detected.VolumeData) != "" {
		current.VolumeData = detected.VolumeData
	}
	if strings.TrimSpace(current.NetworkName) == "" && strings.TrimSpace(detected.NetworkName) != "" {
		current.NetworkName = detected.NetworkName
	}
	if len(current.TraefikLabels) == 0 && len(detected.TraefikLabels) > 0 {
		current.TraefikLabels = append([]string(nil), detected.TraefikLabels...)
	}
	return current
}

func (b *selfDeployBiz) SaveConfig(ctx context.Context, cfg *SelfDeployConfig, user web.User) error {
	if cfg == nil {
		return errors.New("self-deploy: nil config")
	}
	// Validate render BEFORE persisting so a malformed template cannot
	// land in the DB. Uses the live (possibly partial) payload, not a
	// default-merged copy, to give the operator an actionable error.
	tmpl := cfg.Template
	if strings.TrimSpace(tmpl) == "" {
		tmpl = LoadSeedTemplate()
	}
	yaml, err := RenderTemplate(tmpl, cfg.Placeholders)
	if err != nil {
		return fmt.Errorf("self-deploy: template validation failed: %w", err)
	}
	if _, err := compose.Parse("self-deploy-save", yaml); err != nil {
		return fmt.Errorf("self-deploy: rendered YAML is not a valid compose file: %w", err)
	}

	// Persist through SettingBiz so the live *misc.Setting snapshot is
	// refreshed in place (refreshInMemory captures the pointer at boot).
	buf, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("self-deploy: marshal config: %w", err)
	}
	return b.sb.Save(ctx, settingIDSelfDeploy, json.RawMessage(buf), user)
}

func (b *selfDeployBiz) PrepareJob(ctx context.Context) (*SelfDeployJob, error) {
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

	tmpl := cfg.Template
	if strings.TrimSpace(tmpl) == "" {
		tmpl = LoadSeedTemplate()
	}
	yaml, err := RenderTemplate(tmpl, cfg.Placeholders)
	if err != nil {
		return nil, err
	}
	if _, err := compose.Parse("self-deploy-prepare", yaml); err != nil {
		return nil, fmt.Errorf("self-deploy: rendered YAML is not a valid compose file: %w", err)
	}

	// Capture the primary container's current image so we have a
	// rollback target. d.ContainerInspect("", selfID) uses the primary
	// client; in standalone mode node "" means the local daemon.
	cli, err := b.d.Client()
	if err != nil {
		return nil, fmt.Errorf("self-deploy: obtain docker client: %w", err)
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

	mergedPlaceholders := mergeWithDefaults(cfg.Placeholders)
	target := strings.TrimSpace(mergedPlaceholders.ImageTag)
	if target == "" {
		target = misc.SelfDeployImageTag
	}
	allow := mergedPlaceholders.RecoveryAllow
	if len(allow) == 0 {
		allow = []string{misc.SelfDeployDefaultRecoveryCIDR}
	}

	job := &SelfDeployJob{
		ID:               createId(),
		CreatedAt:        time.Now().UTC(),
		ComposeYAML:      yaml,
		Placeholders:     mergedPlaceholders,
		PreviousImageTag: prevImage,
		TargetImageTag:   target,
		PrimaryContainer: selfID,
		RecoveryPort:     mergedPlaceholders.RecoveryPort,
		RecoveryAllow:    allow,
		TimeoutSec:       cfg.DeployTimeout,
		AutoRollback:     cfg.AutoRollback,
	}
	// Sanity: make sure cli is reachable (Ping) before running
	// daemon-aware invariants. Surfacing a broken daemon here means the
	// UI gets a 500 at PrepareJob time instead of a silent "sidekick
	// started" message that never materialises.
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
	job, err := b.PrepareJob(ctx)
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
	// Release the lock on every non-happy exit path below. The sidekick
	// removes the lock itself in Phase 4 once the lifecycle is complete;
	// for now we hold it only long enough to spawn the container so a
	// spawn failure doesn't leave a stale lock file behind.
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

	// Spawn the sidekick via direct Docker API — NOT via the compose
	// engine. The compose engine tears down the existing project first,
	// which would kill the primary mid-HTTP-response (classic
	// self-destruct). The sidekick runs OUTSIDE the swirl compose
	// project and is explicitly labelled so teardown never touches it.
	cli, err := b.d.Client()
	if err != nil {
		return nil, fmt.Errorf("self-deploy: obtain docker client: %w", err)
	}

	if spawnErr := b.spawnSidekick(ctx, cli, job, jobPath); spawnErr != nil {
		// Best-effort state update: the failure is in the spawn path,
		// not the filesystem, so surface it in state.json for the UI.
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

// spawnSidekick creates and starts the deploy-agent container. The
// container inherits the primary's image (so the deploy-agent binary is
// guaranteed identical to the primary's), mounts the docker socket and
// the self-deploy state directory, and binds the recovery port on
// 127.0.0.1 host-side by default. AutoRemove is OFF so operators can
// diagnose a post-failure recovery run.
func (b *selfDeployBiz) spawnSidekick(ctx context.Context, cli *dockerclient.Client, job *SelfDeployJob, jobPath string) error {
	// Determine the image the sidekick should run. Prefer the image
	// the primary container was spawned with — it guarantees the
	// sidekick binary matches the primary. Fall back to the target
	// image tag only if inspect is unavailable.
	sidekickImage := job.PreviousImageTag
	if sidekickImage == "" {
		sidekickImage = sidekickImageFallback
	}

	// Short ID for the sidekick container name so operators can spot it
	// via `docker ps` without copy-pasting a 64-char hex.
	shortID := job.ID
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}
	name := "swirl-deploy-agent-" + shortID

	// Env passed to the sidekick — mirrors the contract documented in
	// cmd/deploy_agent/doc.go.
	env := []string{
		"SWIRL_SELF_DEPLOY_JOB=" + jobPath,
		"SWIRL_RECOVERY_PORT=" + strconv.Itoa(job.RecoveryPort),
		"SWIRL_RECOVERY_ALLOW=" + strings.Join(job.RecoveryAllow, ","),
	}

	ccfg := &dockercontainer.Config{
		Image:      sidekickImage,
		Cmd:        []string{"/swirl", "deploy-agent"},
		Env:        env,
		Tty:        false,
		OpenStdin:  false,
		Labels: map[string]string{
			selfDeployLabelRole: selfDeployLabelRoleAgent,
			selfDeployLabelJob:  job.ID,
		},
	}

	hcfg := &dockercontainer.HostConfig{
		// host network: the sidekick needs to bind the recovery port
		// AND to reach the new Swirl container's exposed port for the
		// health check without racing the primary's network teardown.
		// Documented in the plan (Fase 3.6).
		NetworkMode: dockercontainer.NetworkMode("host"),
		AutoRemove:  false,
		Mounts: []dockermount.Mount{
			{
				Type:   dockermount.TypeBind,
				Source: "/var/run/docker.sock",
				Target: "/var/run/docker.sock",
			},
			{
				Type:   dockermount.TypeBind,
				Source: selfDeployStateDir,
				Target: selfDeployStateDir,
			},
		},
		RestartPolicy: dockercontainer.RestartPolicy{Name: dockercontainer.RestartPolicyDisabled},
	}

	createCtx, createCancel := context.WithTimeout(ctx, 30*time.Second)
	defer createCancel()
	resp, err := cli.ContainerCreate(createCtx, ccfg, hcfg, nil, nil, name)
	if err != nil {
		// If the sidekick image is missing locally (e.g. first-ever
		// deploy after a manual swirl_data volume mount), pull it
		// once and retry. ImagePull streams JSON to the reader; we
		// drain it via io.Copy to surface embedded errors.
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
		// Best-effort cleanup of the created-but-not-started
		// container so a retry can reuse the name.
		_ = cli.ContainerRemove(ctx, resp.ID, dockercontainer.RemoveOptions{Force: true})
		return fmt.Errorf("self-deploy: start sidekick container: %w", err)
	}

	b.logger.Infof("self-deploy: sidekick %s spawned (container %s, image %s, job %s)", name, resp.ID[:12], sidekickImage, job.ID)
	return nil
}

func (b *selfDeployBiz) Status(_ context.Context) (*SelfDeployStatus, error) {
	st, err := readSelfDeployState()
	if err != nil {
		return nil, err
	}
	if st == nil {
		// No deploy has ever been triggered on this volume — surface an
		// explicit idle snapshot so the UI doesn't have to special-case
		// nil.
		return &SelfDeployStatus{Phase: "idle"}, nil
	}
	// Main-side event publishing with idempotency: the sidekick has no
	// DB access, so success / failure / rolled_back / recovery events
	// cannot be emitted from there. On every Status poll we check the
	// terminal-phase + EventPublished pair and, when we see a terminal
	// phase that hasn't been audited yet, we emit the event and flip
	// the flag. The next poll finds EventPublished=true and skips.
	//
	// This relies on Status being called at least once after the
	// sidekick writes the terminal phase. The UI polls status while the
	// deploy is in flight and for a short window after reconnect — good
	// enough for single-primary Swirl. If operators never poll, the
	// event is still emitted the first time anyone opens the Self-deploy
	// panel.
	b.publishTerminalEvent(st)
	return &SelfDeployStatus{
		Phase:   st.Phase,
		JobID:   st.JobID,
		Error:   st.Error,
		LogTail: st.LogTail,
	}, nil
}

// publishTerminalEvent is the idempotent audit-emission helper invoked
// by Status. It is intentionally best-effort: a failure to rewrite
// state.json (e.g. volume full) logs a warning but does not surface
// to the caller — the audit trail is a convenience, not a dependency
// of the UI's render. If EventBiz is nil (unit tests that construct
// selfDeployBiz bare) we bail silently too.
func (b *selfDeployBiz) publishTerminalEvent(st *SelfDeployState) {
	if st == nil || b.eb == nil {
		return
	}
	if st.EventPublished {
		return
	}
	// Only emit for terminal phases. In-flight phases (pending, pulling,
	// starting, health_check) are mid-deploy — the event would be
	// premature.
	var action EventAction
	switch st.Phase {
	case SelfDeployPhaseSuccess:
		action = EventActionSelfDeploySuccess
	case SelfDeployPhaseFailed, SelfDeployPhaseRecovery, SelfDeployPhaseRolledBack:
		action = EventActionSelfDeployFailure
	default:
		return
	}

	// Best-effort read of the job descriptor so the event carries the
	// image tag. Missing or unreadable job file just elides the field —
	// the event still fires so the audit trail isn't blank.
	imageTag := ""
	if job, err := readSelfDeployJob(); err == nil && job != nil {
		imageTag = job.TargetImageTag
	}
	b.eb.CreateSelfDeploy(action, st.JobID, imageTag, nil)

	// Flip the flag and rewrite state.json so subsequent polls see
	// EventPublished=true and skip the emission. Failures here are
	// logged but not fatal: double-emission is annoying but not
	// incorrect (the UI only shows the last event per job).
	st.EventPublished = true
	if err := writeSelfDeployState(st); err != nil {
		if b.logger != nil {
			b.logger.Warnf("self-deploy: could not mark event published for job %s: %v", st.JobID, err)
		}
	}
}

// ValidateSelfDeployJob is the public entry point for the pure
// (structural-only) invariant check. The sidekick calls this right
// before runDeploy as a double-check: the main Swirl already ran the
// same checks before writing job.json, but calling them again makes
// the sidekick self-contained — a manually-crafted job.json written by
// an operator poking the volume still fails fast instead of producing
// a hard-to-diagnose error deep in the lifecycle.
func ValidateSelfDeployJob(job *SelfDeployJob) error {
	return validateInvariants(job)
}

// sidekickContainerNameRe matches container names that collide with the
// sidekick naming convention (`swirl-deploy-agent-<short>`). A rendered
// compose service that hard-codes `container_name` matching this pattern
// would put a child service into the sidekick's namespace — we refuse
// the deploy rather than let the operator discover the collision through
// a mysterious "container already exists" error from Docker.
var sidekickContainerNameRe = regexp.MustCompile(`(?i)^swirl-deploy-agent(-|$)`)

// validateInvariants is the pure form: structural checks only, no
// daemon round-trip. Safe to call from unit tests and from both the
// main Swirl (before spawning the sidekick) and the sidekick itself
// (belt-and-braces double-check before runDeploy).
//
// Enforced:
//  1. PrimaryContainer is non-empty (SelfContainerID must have
//     succeeded upstream).
//  2. TargetImageTag is non-empty.
//  3. ComposeYAML parses cleanly (compose.Parse runs Config validation
//     including the strict standalone rules: no `build:`, image is
//     required per service).
//  4. RecoveryPort != ExposePort — listening on the same port breaks
//     the deploy handshake because the new Swirl would race the
//     recovery UI for bind().
//  5. No service's `container_name` collides with the sidekick naming
//     pattern `swirl-deploy-agent-*` — preempt the "container already
//     exists" error that would otherwise surface only at create time.
//  6. RecoveryAllow containing "0.0.0.0/0" logs a warning (not an
//     error) — the operator explicitly asked for wide-open recovery,
//     we just note it in the audit trail.
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
	cfg, err := compose.Parse("self-deploy-validate", job.ComposeYAML)
	if err != nil {
		return fmt.Errorf("self-deploy: rendered YAML is invalid: %w", err)
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
			// Not an error — operator asked for it.
		}
	}
	return nil
}

// validateInvariantsWithDaemon extends the pure invariants with checks
// that need a live Docker client: the primary container actually exists,
// and every external network / external volume referenced by the
// rendered compose file resolves on the host. Called by the main Swirl
// in PrepareJob right before TriggerDeploy spawns the sidekick — this
// is the last chance to fail a deploy with an actionable message
// instead of letting the sidekick discover the problem at create time.
//
// The sidekick itself does NOT call this: by the time the sidekick
// boots, the primary has been renamed to `swirl-previous` and the
// simple `ContainerInspect(PrimaryContainer)` check would fail on the
// original ID. The sidekick relies on its own compose-engine preflight
// (`preflightExternalNetworks` / `preflightExternalVolumes`) to catch
// the same problems.
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

	// Re-parse to inspect the external network/volume references. Parse
	// is pure — we've already checked it above — so the second call just
	// hands us a typed Config without repeating the work.
	cfg, err := compose.Parse("self-deploy-validate-daemon", job.ComposeYAML)
	if err != nil {
		return fmt.Errorf("self-deploy: rendered YAML is invalid: %w", err)
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

// validateExternalNetworks is the same spirit as
// compose.StandaloneEngine.preflightExternalNetworks but lives in biz so
// we can reuse it before the compose engine runs. The engine will redo
// its own preflight right before creating containers, but surfacing the
// error here lets the UI show it while the primary is still healthy.
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

// Docker label keys reserved for the self-deploy feature. Mirror the
// `com.swirl.compose.*` convention for continuity. Phase 4 uses them
// from the sidekick side to re-discover itself on a container restart
// and to keep the stack-teardown code path from accidentally removing
// the agent.
const (
	selfDeployLabelRole      = "com.swirl.self-deploy"
	selfDeployLabelRoleAgent = "agent"
	selfDeployLabelJob       = "com.swirl.self-deploy.job"
)
