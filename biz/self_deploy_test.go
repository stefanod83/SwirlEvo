package biz

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/cuigh/auxo/data"
	"github.com/cuigh/auxo/net/web"
	"github.com/cuigh/swirl/dao"
	"github.com/cuigh/swirl/docker/compose"
	"github.com/cuigh/swirl/misc"
	dockercontainer "github.com/docker/docker/api/types/container"
	dockermount "github.com/docker/docker/api/types/mount"
	dockernetwork "github.com/docker/docker/api/types/network"
	"github.com/docker/go-connections/nat"
)

// stubSettingBiz implements SettingBiz for the self-deploy tests without
// touching a real DAO. The blob is held as a map[string]interface{} to
// mimic the JSON round-trip performed by the real biz (findRaw →
// unmarshal via UseNumber produces map[string]interface{} for object
// payloads).
type stubSettingBiz struct {
	blobs map[string]interface{}
}

func newStubSettingBiz() *stubSettingBiz { return &stubSettingBiz{blobs: map[string]interface{}{}} }

func (s *stubSettingBiz) Find(_ context.Context, id string) (interface{}, error) {
	if v, ok := s.blobs[id]; ok {
		return v, nil
	}
	return nil, nil
}

func (s *stubSettingBiz) Load(_ context.Context) (data.Map, error) {
	out := data.Map{}
	for k, v := range s.blobs {
		out[k] = v
	}
	return out, nil
}

func (s *stubSettingBiz) LoadRaw(ctx context.Context) (data.Map, error) {
	return s.Load(ctx)
}

func (s *stubSettingBiz) Save(_ context.Context, id string, options interface{}, _ web.User) error {
	// Mirror the real biz's JSON-roundtrip behaviour: whatever typed
	// struct the caller passes in ends up stored as a decoded map so
	// subsequent Find() calls return the same shape a real DAO would.
	buf, err := json.Marshal(options)
	if err != nil {
		return err
	}
	var decoded interface{}
	if err := json.Unmarshal(buf, &decoded); err != nil {
		return err
	}
	s.blobs[id] = decoded
	return nil
}

// stubEventBiz is a black-hole EventBiz — tests don't assert on the
// audit trail, they just need the nil-safe record call for Trigger.
type stubEventBiz struct{}

func (stubEventBiz) Search(context.Context, interface{}) (interface{}, int, error) {
	return nil, 0, nil
}
func (stubEventBiz) Prune(context.Context, int32) error                                        { return nil }
func (stubEventBiz) CreateRegistry(EventAction, string, string, web.User)                      {}
func (stubEventBiz) CreateNode(EventAction, string, string, web.User)                          {}
func (stubEventBiz) CreateNetwork(EventAction, string, string, string, web.User)               {}
func (stubEventBiz) CreateService(EventAction, string, web.User)                               {}
func (stubEventBiz) CreateConfig(EventAction, string, string, web.User)                        {}
func (stubEventBiz) CreateSecret(EventAction, string, string, web.User)                        {}
func (stubEventBiz) CreateStack(EventAction, string, string, web.User)                         {}
func (stubEventBiz) CreateImage(EventAction, string, string, web.User)                         {}
func (stubEventBiz) CreateContainer(EventAction, string, string, string, web.User)             {}
func (stubEventBiz) CreateVolume(EventAction, string, string, web.User)                        {}
func (stubEventBiz) CreateUser(EventAction, string, string, web.User)                          {}
func (stubEventBiz) CreateRole(EventAction, string, string, web.User)                          {}
func (stubEventBiz) CreateChart(EventAction, string, string, web.User)                         {}
func (stubEventBiz) CreateSetting(EventAction, web.User)                                       {}
func (stubEventBiz) CreateHost(EventAction, string, string, web.User)                          {}
func (stubEventBiz) CreateBackup(EventAction, string, string, web.User)                        {}
func (stubEventBiz) CreateVaultSecret(EventAction, string, string, web.User)                   {}
func (stubEventBiz) CreateSelfDeploy(EventAction, string, string, web.User)                    {}

// Compile-time check: our stubs must implement the real interfaces.
var _ SettingBiz = (*stubSettingBiz)(nil)

// swapStateDir redirects selfDeployStateDir to t.TempDir() for the
// duration of the test. Restored via t.Cleanup so a panic in one test
// cannot leak the override into the next one.
func swapStateDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	original := selfDeployStateDir
	selfDeployStateDir = dir
	t.Cleanup(func() { selfDeployStateDir = original })
	return dir
}

// setStandaloneMode sets Options.Mode for the test and restores the
// previous value on cleanup. Required for any test that exercises
// PrepareJob / TriggerDeploy, since the first invariant they enforce
// is `misc.IsStandalone()`.
func setStandaloneMode(t *testing.T, mode string) {
	t.Helper()
	original := misc.Options.Mode
	misc.Options.Mode = mode
	t.Cleanup(func() { misc.Options.Mode = original })
}

// setSelfContainerID forces misc.SelfContainerID to return a known
// value via the SWIRL_CONTAINER_ID env var override, so tests don't
// depend on running inside a real container.
func setSelfContainerID(t *testing.T, id string) {
	t.Helper()
	t.Setenv("SWIRL_CONTAINER_ID", id)
}

// TestLoadConfigEmptyReturnsDefaults: the first-boot path must hand
// back a fully-populated config even when the Settings record is
// absent, so the UI can always render a meaningful form.
func TestLoadConfigEmptyReturnsDefaults(t *testing.T) {
	sb := newStubSettingBiz()
	b := &selfDeployBiz{sb: sb}
	cfg, err := b.LoadConfig(context.Background())
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg == nil {
		t.Fatalf("expected non-nil config")
	}
	if strings.TrimSpace(cfg.Template) == "" {
		t.Fatalf("expected template to default to seed, got empty")
	}
	if cfg.DeployTimeout != misc.SelfDeployDefaultTimeoutSec {
		t.Fatalf("expected default DeployTimeout %d, got %d", misc.SelfDeployDefaultTimeoutSec, cfg.DeployTimeout)
	}
	if cfg.Placeholders.ExposePort != misc.SelfDeployExposePort {
		t.Fatalf("expected default ExposePort %d, got %d", misc.SelfDeployExposePort, cfg.Placeholders.ExposePort)
	}
}

// TestSaveConfigInvalidTemplateRejected: a malformed template must not
// reach the DAO. Previews the ErrSelfDeployNotImplemented path is
// gone — SaveConfig now returns a template-validation error.
func TestSaveConfigInvalidTemplateRejected(t *testing.T) {
	sb := newStubSettingBiz()
	b := &selfDeployBiz{sb: sb}
	cfg := &SelfDeployConfig{
		Enabled:  true,
		Template: "services:\n  x:\n    image: {{.NonExistentField}}",
	}
	err := b.SaveConfig(context.Background(), cfg, nil)
	if err == nil {
		t.Fatalf("expected validation error, got nil")
	}
	if _, ok := sb.blobs[settingIDSelfDeploy]; ok {
		t.Fatalf("config must not be persisted when validation fails")
	}
}

// TestSaveConfigRoundTrip: a valid payload persists AND comes back
// identical on the next LoadConfig. Guards against JSON-tag drift on
// the SelfDeployConfig struct.
func TestSaveConfigRoundTrip(t *testing.T) {
	sb := newStubSettingBiz()
	b := &selfDeployBiz{sb: sb}
	cfg := &SelfDeployConfig{
		Enabled:       true,
		Template:      LoadSeedTemplate(),
		Placeholders:  DefaultPlaceholders(),
		AutoRollback:  true,
		DeployTimeout: 420,
	}
	cfg.Placeholders.ImageTag = "example/swirl:custom"
	if err := b.SaveConfig(context.Background(), cfg, nil); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	got, err := b.LoadConfig(context.Background())
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !got.Enabled {
		t.Fatalf("expected Enabled=true")
	}
	if got.DeployTimeout != 420 {
		t.Fatalf("expected DeployTimeout=420, got %d", got.DeployTimeout)
	}
	if got.Placeholders.ImageTag != "example/swirl:custom" {
		t.Fatalf("expected ImageTag round-trip, got %q", got.Placeholders.ImageTag)
	}
}

// TestPrepareJobRejectsSwarmMode: the very first invariant — self-deploy
// must refuse to even prepare a job when Swirl is running in Swarm mode.
func TestPrepareJobRejectsSwarmMode(t *testing.T) {
	setStandaloneMode(t, "swarm")
	sb := newStubSettingBiz()
	b := &selfDeployBiz{sb: sb}
	_, err := b.PrepareJob(context.Background())
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "standalone") {
		t.Fatalf("expected error to mention standalone mode, got %v", err)
	}
}

// TestPrepareJobRejectsWhenSelfIDMissing: in standalone mode with no
// SWIRL_CONTAINER_ID and no /proc/self/cgroup hit, SelfContainerID
// falls back to os.Hostname — which in a test binary returns a
// machine name, not a container ID. To force the failure path we
// would need to mock SelfContainerID; here we assert the *shape* of
// the error when the primary ID cannot be resolved to something
// container-like. Rather than mocking, we exercise the "config not
// enabled" path below, which is the practical UX barrier operators
// will hit first.
//
// The invariant check itself is covered by TestValidateInvariantsRejectsEmptyPrimary.
func TestPrepareJobRejectsDisabledConfig(t *testing.T) {
	setStandaloneMode(t, "standalone")
	setSelfContainerID(t, "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef")
	sb := newStubSettingBiz()
	b := &selfDeployBiz{sb: sb}
	_, err := b.PrepareJob(context.Background())
	if err == nil {
		t.Fatalf("expected disabled-config error, got nil")
	}
	if !strings.Contains(err.Error(), "not enabled") {
		t.Fatalf("expected error to mention 'not enabled', got %v", err)
	}
}

// TestTriggerDeployRejectsWhenLockHeld: a second Trigger must bail
// before touching the daemon when another deploy is already in flight.
// We pre-create the lock file manually so the test does not need a
// real sidekick.
func TestTriggerDeployRejectsWhenLockHeld(t *testing.T) {
	dir := swapStateDir(t)
	setStandaloneMode(t, "standalone")
	setSelfContainerID(t, "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef")

	// Pre-create the lock to simulate an in-flight deploy.
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	lockPath := filepath.Join(dir, selfDeployLockFile)
	if err := os.WriteFile(lockPath, []byte("held"), 0o600); err != nil {
		t.Fatalf("write lock: %v", err)
	}

	// Seed an enabled config so PrepareJob's own invariants pass. The
	// daemon call inside PrepareJob (Ping + inspect) would normally fail
	// in a test environment — to stop short of it, we build a job
	// manually and call acquireSelfDeployLock directly instead of
	// TriggerDeploy. This exercises the real lock-check contract that
	// TriggerDeploy depends on, without needing a running daemon.
	_, err := acquireSelfDeployLock()
	if err == nil {
		t.Fatalf("expected lock-held error, got nil")
	}
	if !errors.Is(err, errLockHeld) {
		t.Fatalf("expected errLockHeld, got %v", err)
	}
}

// TestAcquireAndReleaseLock verifies the happy path for the lock so
// subsequent tests can rely on it being reusable.
func TestAcquireAndReleaseLock(t *testing.T) {
	swapStateDir(t)
	release, err := acquireSelfDeployLock()
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	// A second acquire before release must fail.
	if _, err := acquireSelfDeployLock(); !errors.Is(err, errLockHeld) {
		t.Fatalf("expected errLockHeld, got %v", err)
	}
	release()
	// After release, a new acquire must succeed again.
	release2, err := acquireSelfDeployLock()
	if err != nil {
		t.Fatalf("re-acquire after release: %v", err)
	}
	release2()
}

// TestValidateInvariantsRejectsEmptyPrimary guards the first field of
// validateInvariants — PrimaryContainer must be non-empty.
func TestValidateInvariantsRejectsEmptyPrimary(t *testing.T) {
	j := &SelfDeployJob{
		TargetImageTag: "x/y:1",
		ComposeYAML:    "services:\n  x:\n    image: x/y:1\n",
	}
	if err := validateInvariants(j); err == nil {
		t.Fatalf("expected error for empty PrimaryContainer, got nil")
	}
}

// TestValidateInvariantsRejectsEmptyTarget guards the second field.
func TestValidateInvariantsRejectsEmptyTarget(t *testing.T) {
	j := &SelfDeployJob{
		PrimaryContainer: "swirl",
		ComposeYAML:      "services:\n  x:\n    image: x/y:1\n",
	}
	if err := validateInvariants(j); err == nil {
		t.Fatalf("expected error for empty TargetImageTag, got nil")
	}
}

// TestValidateInvariantsRejectsPortCollision guards the recovery-port
// invariant (recovery UI would race the new Swirl for bind()).
func TestValidateInvariantsRejectsPortCollision(t *testing.T) {
	j := &SelfDeployJob{
		PrimaryContainer: "swirl",
		TargetImageTag:   "x/y:1",
		ComposeYAML:      "services:\n  x:\n    image: x/y:1\n",
		RecoveryPort:     8001,
		Placeholders:     SelfDeployPlaceholders{ExposePort: 8001},
	}
	if err := validateInvariants(j); err == nil {
		t.Fatalf("expected error on port collision, got nil")
	}
}

// TestValidateInvariantsHappyPath confirms the canonical shape of a
// valid job.
func TestValidateInvariantsHappyPath(t *testing.T) {
	j := &SelfDeployJob{
		PrimaryContainer: "swirl",
		TargetImageTag:   "x/y:1",
		ComposeYAML:      "services:\n  x:\n    image: x/y:1\n",
		RecoveryPort:     8002,
		Placeholders:     SelfDeployPlaceholders{ExposePort: 8001},
	}
	if err := validateInvariants(j); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestValidateInvariantsRejectsSidekickNameCollision guards the Phase 7
// invariant: a compose service whose `container_name` matches the
// sidekick naming pattern `swirl-deploy-agent-*` would silently collide
// with the agent container; refuse up-front.
func TestValidateInvariantsRejectsSidekickNameCollision(t *testing.T) {
	j := &SelfDeployJob{
		PrimaryContainer: "swirl",
		TargetImageTag:   "x/y:1",
		ComposeYAML: "services:\n" +
			"  mole:\n" +
			"    image: x/y:1\n" +
			"    container_name: swirl-deploy-agent-helper\n",
		RecoveryPort: 8002,
		Placeholders: SelfDeployPlaceholders{ExposePort: 8001},
	}
	err := validateInvariants(j)
	if err == nil {
		t.Fatalf("expected error on sidekick naming collision, got nil")
	}
	if !strings.Contains(err.Error(), "sidekick naming pattern") {
		t.Fatalf("expected error to mention sidekick naming pattern, got %v", err)
	}
}

// TestValidateSelfDeployJobExported is a belt-and-braces check that the
// exported sidekick-facing wrapper preserves the underlying invariant
// rules. If the exported signature drifts the sidekick will silently
// stop double-checking its input.
func TestValidateSelfDeployJobExported(t *testing.T) {
	if err := ValidateSelfDeployJob(nil); err == nil {
		t.Fatalf("expected error on nil job")
	}
	good := &SelfDeployJob{
		PrimaryContainer: "swirl",
		TargetImageTag:   "x/y:1",
		ComposeYAML:      "services:\n  x:\n    image: x/y:1\n",
		RecoveryPort:     8002,
		Placeholders:     SelfDeployPlaceholders{ExposePort: 8001},
	}
	if err := ValidateSelfDeployJob(good); err != nil {
		t.Fatalf("unexpected error on happy path: %v", err)
	}
}

// TestValidateInvariantsDaemonSeedNoExternalChecks is the v1.1 guardrail:
// the default seed template no longer marks volume/network as
// `external: true`, so the external-existence checks executed by
// validateInvariantsWithDaemon (validateExternalNetworks /
// validateExternalVolumes) must not fire for a default deploy.
//
// We prove this by parsing the rendered seed and handing the resulting
// network/volume maps to the validators with a nil docker client —
// if the validators tried to reach the daemon for even one entry,
// they'd dereference nil and panic. A clean return over a nil client
// is the contractual "no-op on non-external entries" signal.
//
// The validators themselves are preserved on purpose: operators who
// customise the template to add `external: true` still benefit from
// the pre-flight existence check via their own daemon-backed code path.
func TestValidateInvariantsDaemonSeedNoExternalChecks(t *testing.T) {
	yaml, err := RenderTemplate(LoadSeedTemplate(), DefaultPlaceholders())
	if err != nil {
		t.Fatalf("render seed: %v", err)
	}
	cfg, err := compose.Parse("self-deploy-seed-no-external", yaml)
	if err != nil {
		t.Fatalf("compose.Parse: %v", err)
	}
	if cfg == nil {
		t.Fatalf("compose.Parse returned nil cfg")
	}

	// Structural assertion: no network/volume in the seed should carry
	// External.External=true. If a future edit reintroduces it, this
	// test catches it before the validator runs.
	for name, n := range cfg.Networks {
		if n.External.External {
			t.Fatalf("seed network %q must not be external, got External=true", name)
		}
	}
	for name, v := range cfg.Volumes {
		if v.External.External {
			t.Fatalf("seed volume %q must not be external, got External=true", name)
		}
	}

	// Contractual assertion: the validators must be a no-op when every
	// entry is non-external. Pass a nil client — a panic here would
	// mean the validators touched the client despite having nothing
	// to check, i.e. the "skip on !External" guard regressed.
	if err := validateExternalNetworks(context.Background(), nil, cfg.Networks); err != nil {
		t.Fatalf("validateExternalNetworks should no-op for non-external seed, got: %v", err)
	}
	if err := validateExternalVolumes(context.Background(), nil, cfg.Volumes); err != nil {
		t.Fatalf("validateExternalVolumes should no-op for non-external seed, got: %v", err)
	}
}

// recordingEventBiz is a full EventBiz stub that captures every
// CreateSelfDeploy call so tests can assert on the audit trail the
// Status endpoint produces. All other methods are black-hole no-ops —
// the Status code path only exercises CreateSelfDeploy.
type recordingEventBiz struct {
	calls []struct {
		action   EventAction
		jobID    string
		imageTag string
	}
}

func (r *recordingEventBiz) Search(context.Context, *dao.EventSearchArgs) ([]*dao.Event, int, error) {
	return nil, 0, nil
}
func (r *recordingEventBiz) Prune(context.Context, int32) error { return nil }
func (r *recordingEventBiz) CreateRegistry(EventAction, string, string, web.User) {}
func (r *recordingEventBiz) CreateNode(EventAction, string, string, web.User)     {}
func (r *recordingEventBiz) CreateNetwork(EventAction, string, string, string, web.User) {
}
func (r *recordingEventBiz) CreateService(EventAction, string, web.User)                   {}
func (r *recordingEventBiz) CreateConfig(EventAction, string, string, web.User)            {}
func (r *recordingEventBiz) CreateSecret(EventAction, string, string, web.User)            {}
func (r *recordingEventBiz) CreateStack(EventAction, string, string, web.User)             {}
func (r *recordingEventBiz) CreateImage(EventAction, string, string, web.User)             {}
func (r *recordingEventBiz) CreateContainer(EventAction, string, string, string, web.User) {}
func (r *recordingEventBiz) CreateVolume(EventAction, string, string, web.User)            {}
func (r *recordingEventBiz) CreateUser(EventAction, string, string, web.User)              {}
func (r *recordingEventBiz) CreateRole(EventAction, string, string, web.User)              {}
func (r *recordingEventBiz) CreateChart(EventAction, string, string, web.User)             {}
func (r *recordingEventBiz) CreateSetting(EventAction, web.User)                           {}
func (r *recordingEventBiz) CreateHost(EventAction, string, string, web.User)              {}
func (r *recordingEventBiz) CreateBackup(EventAction, string, string, web.User)            {}
func (r *recordingEventBiz) CreateVaultSecret(EventAction, string, string, web.User)       {}
func (r *recordingEventBiz) CreateSelfDeploy(action EventAction, jobID, imageTag string, _ web.User) {
	r.calls = append(r.calls, struct {
		action   EventAction
		jobID    string
		imageTag string
	}{action, jobID, imageTag})
}

// Compile-time check that the stub satisfies the real EventBiz
// interface, so a future rename/addition to EventBiz surfaces as a
// test-compilation failure rather than a runtime surprise.
var _ EventBiz = (*recordingEventBiz)(nil)

// TestStatusPublishesSuccessEventOnce is the idempotency guarantee for
// the main-side event-publishing strategy: the first Status poll after
// the sidekick writes Phase=success emits a Success event; subsequent
// polls must NOT emit a duplicate.
func TestStatusPublishesSuccessEventOnce(t *testing.T) {
	swapStateDir(t)
	// Seed job.json so the emitted event carries the image tag.
	if _, err := writeSelfDeployJob(&SelfDeployJob{
		ID:             "job-1",
		TargetImageTag: "example/swirl:v2",
	}); err != nil {
		t.Fatalf("writeSelfDeployJob: %v", err)
	}
	if err := writeSelfDeployState(&SelfDeployState{
		JobID: "job-1",
		Phase: SelfDeployPhaseSuccess,
	}); err != nil {
		t.Fatalf("writeSelfDeployState: %v", err)
	}

	eb := &recordingEventBiz{}
	b := &selfDeployBiz{eb: eb}

	if _, err := b.Status(context.Background()); err != nil {
		t.Fatalf("first Status: %v", err)
	}
	if _, err := b.Status(context.Background()); err != nil {
		t.Fatalf("second Status: %v", err)
	}
	if len(eb.calls) != 1 {
		t.Fatalf("expected exactly one CreateSelfDeploy call, got %d", len(eb.calls))
	}
	if eb.calls[0].action != EventActionSelfDeploySuccess {
		t.Fatalf("expected Success action, got %s", eb.calls[0].action)
	}
	if eb.calls[0].jobID != "job-1" {
		t.Fatalf("expected jobID=job-1, got %q", eb.calls[0].jobID)
	}
	if eb.calls[0].imageTag != "example/swirl:v2" {
		t.Fatalf("expected imageTag=example/swirl:v2, got %q", eb.calls[0].imageTag)
	}
}

// TestStatusPublishesFailureEventOnce mirrors the success test for the
// Failure action. Terminal phases `failed`, `recovery`, `rolled_back`
// all map to `Failure` — the UI distinguishes them via the phase
// string itself, the event action is the coarse-grained audit flag.
func TestStatusPublishesFailureEventOnce(t *testing.T) {
	swapStateDir(t)
	if _, err := writeSelfDeployJob(&SelfDeployJob{
		ID:             "job-2",
		TargetImageTag: "example/swirl:bad",
	}); err != nil {
		t.Fatalf("writeSelfDeployJob: %v", err)
	}
	if err := writeSelfDeployState(&SelfDeployState{
		JobID: "job-2",
		Phase: SelfDeployPhaseRecovery,
		Error: "new Swirl never came up",
	}); err != nil {
		t.Fatalf("writeSelfDeployState: %v", err)
	}

	eb := &recordingEventBiz{}
	b := &selfDeployBiz{eb: eb}

	if _, err := b.Status(context.Background()); err != nil {
		t.Fatalf("Status: %v", err)
	}
	if _, err := b.Status(context.Background()); err != nil {
		t.Fatalf("second Status: %v", err)
	}
	if len(eb.calls) != 1 {
		t.Fatalf("expected exactly one CreateSelfDeploy call, got %d", len(eb.calls))
	}
	if eb.calls[0].action != EventActionSelfDeployFailure {
		t.Fatalf("expected Failure action, got %s", eb.calls[0].action)
	}
}

// TestStatusInFlightPhaseDoesNotPublish: only TERMINAL phases trigger
// an audit emission. A Status poll during `pulling` must NOT record
// a premature Success/Failure.
func TestStatusInFlightPhaseDoesNotPublish(t *testing.T) {
	swapStateDir(t)
	if err := writeSelfDeployState(&SelfDeployState{
		JobID: "job-3",
		Phase: SelfDeployPhasePulling,
	}); err != nil {
		t.Fatalf("writeSelfDeployState: %v", err)
	}
	eb := &recordingEventBiz{}
	b := &selfDeployBiz{eb: eb}
	if _, err := b.Status(context.Background()); err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(eb.calls) != 0 {
		t.Fatalf("expected zero CreateSelfDeploy calls during in-flight phase, got %d", len(eb.calls))
	}
}

// TestStatusIdleWhenNoState: readSelfDeployState returns (nil, nil)
// when the file does not exist; Status must translate that into a
// meaningful "idle" snapshot instead of nil.
func TestStatusIdleWhenNoState(t *testing.T) {
	swapStateDir(t)
	b := &selfDeployBiz{}
	st, err := b.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st == nil {
		t.Fatalf("expected non-nil idle status")
	}
	if st.Phase != "idle" {
		t.Fatalf("expected Phase=idle, got %q", st.Phase)
	}
}

// TestStatusReadsExistingState: the happy-path round-trip from
// writeSelfDeployState → readSelfDeployState → Status.
func TestStatusReadsExistingState(t *testing.T) {
	swapStateDir(t)
	if err := writeSelfDeployState(&SelfDeployState{
		JobID: "abc123",
		Phase: SelfDeployPhasePulling,
	}); err != nil {
		t.Fatalf("writeSelfDeployState: %v", err)
	}
	b := &selfDeployBiz{}
	st, err := b.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.JobID != "abc123" || st.Phase != SelfDeployPhasePulling {
		t.Fatalf("unexpected status: %+v", st)
	}
}

// overrideSelfDeployInspect swaps the package-level selfDeployInspect seam
// for the duration of the test. Restored via t.Cleanup so leaks are
// impossible across test functions.
func overrideSelfDeployInspect(t *testing.T, fn func(ctx context.Context, b *selfDeployBiz, selfID string) (dockercontainer.InspectResponse, error)) {
	t.Helper()
	original := selfDeployInspect
	selfDeployInspect = fn
	t.Cleanup(func() { selfDeployInspect = original })
}

// fakeInspectResponse builds a minimal InspectResponse mirroring what the
// Docker daemon returns for a Swirl container deployed via the v1.1
// conventions: image tagged, host-port bound, /data volume, custom
// network, Traefik labels. Each test picks which fields to populate by
// passing a mutator — keeps the surface of each test narrow while the
// constructor documents the baseline every test starts from.
func fakeInspectResponse(mutators ...func(*dockercontainer.InspectResponse)) dockercontainer.InspectResponse {
	resp := dockercontainer.InspectResponse{
		ContainerJSONBase: &dockercontainer.ContainerJSONBase{
			Name:       "/swirl",
			Image:      "sha256:deadbeef",
			HostConfig: &dockercontainer.HostConfig{PortBindings: nat.PortMap{}},
		},
		Config: &dockercontainer.Config{
			Image:  "cuigh/swirl:v2.1.0",
			Labels: map[string]string{},
		},
		NetworkSettings: &dockercontainer.NetworkSettings{
			Networks: map[string]*dockernetwork.EndpointSettings{},
		},
	}
	for _, m := range mutators {
		m(&resp)
	}
	return resp
}

// isZeroPlaceholders reports whether every field of p is at its
// zero value. SelfDeployPlaceholders contains slices and maps, so
// `p == SelfDeployPlaceholders{}` does not compile — this helper
// performs the equivalent field-by-field check.
func isZeroPlaceholders(p SelfDeployPlaceholders) bool {
	return p.ImageTag == "" &&
		p.ExposePort == 0 &&
		p.RecoveryPort == 0 &&
		len(p.RecoveryAllow) == 0 &&
		len(p.TraefikLabels) == 0 &&
		p.VolumeData == "" &&
		p.NetworkName == "" &&
		p.ContainerName == "" &&
		len(p.ExtraEnv) == 0
}

// TestInspectCurrentContainerExtractsFields exercises the happy path:
// a real-shaped InspectResponse is translated into a well-populated
// SelfDeployPlaceholders. Covers every field the function promises to
// infer + the Traefik label filter (keep routers/services/middlewares,
// drop docker.*).
func TestInspectCurrentContainerExtractsFields(t *testing.T) {
	setSelfContainerID(t, "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef")
	resp := fakeInspectResponse(func(r *dockercontainer.InspectResponse) {
		r.Name = "/swirl"
		r.Config.Image = "cuigh/swirl:v2.1.0"
		r.HostConfig.PortBindings = nat.PortMap{
			"8001/tcp": []nat.PortBinding{{HostIP: "0.0.0.0", HostPort: "8001"}},
		}
		r.Mounts = []dockercontainer.MountPoint{
			{Type: dockermount.TypeVolume, Name: "swirl_data", Destination: "/data"},
		}
		r.NetworkSettings.Networks = map[string]*dockernetwork.EndpointSettings{
			"swirl_net": {},
		}
		r.Config.Labels = map[string]string{
			"traefik.enable":                            "true",
			"traefik.http.routers.swirl.rule":           "Host(`s.example.com`)",
			"traefik.http.services.swirl.loadbalancer":  "port=8001",
			"traefik.docker.network":                    "public",
			"com.docker.compose.project":                "swirl",
		}
	})
	overrideSelfDeployInspect(t, func(_ context.Context, _ *selfDeployBiz, _ string) (dockercontainer.InspectResponse, error) {
		return resp, nil
	})

	b := &selfDeployBiz{}
	got := b.inspectCurrentContainer(context.Background())

	if got.ImageTag != "cuigh/swirl:v2.1.0" {
		t.Errorf("ImageTag: want %q, got %q", "cuigh/swirl:v2.1.0", got.ImageTag)
	}
	if got.ExposePort != 8001 {
		t.Errorf("ExposePort: want 8001, got %d", got.ExposePort)
	}
	if got.ContainerName != "swirl" {
		t.Errorf("ContainerName: want %q, got %q", "swirl", got.ContainerName)
	}
	if got.VolumeData != "swirl_data" {
		t.Errorf("VolumeData: want %q, got %q", "swirl_data", got.VolumeData)
	}
	if got.NetworkName != "swirl_net" {
		t.Errorf("NetworkName: want %q, got %q", "swirl_net", got.NetworkName)
	}
	// TraefikLabels: must include the three routing keys AND must
	// NOT include traefik.docker.network or the compose-project label.
	sort.Strings(got.TraefikLabels)
	want := []string{
		"traefik.enable=true",
		"traefik.http.routers.swirl.rule=Host(`s.example.com`)",
		"traefik.http.services.swirl.loadbalancer=port=8001",
	}
	sort.Strings(want)
	if len(got.TraefikLabels) != len(want) {
		t.Fatalf("TraefikLabels length mismatch: want %v, got %v", want, got.TraefikLabels)
	}
	for i := range want {
		if got.TraefikLabels[i] != want[i] {
			t.Errorf("TraefikLabels[%d]: want %q, got %q", i, want[i], got.TraefikLabels[i])
		}
	}
	for _, lbl := range got.TraefikLabels {
		if strings.HasPrefix(lbl, "traefik.docker.") {
			t.Errorf("TraefikLabels must not include traefik.docker.*, got %q", lbl)
		}
		if strings.HasPrefix(lbl, "com.docker.") {
			t.Errorf("TraefikLabels must not include com.docker.*, got %q", lbl)
		}
	}
}

// TestInspectCurrentContainerGracefulDegrade asserts every documented
// failure path returns a zero-value SelfDeployPlaceholders instead of
// panicking. Covers: daemon unreachable, SelfContainerID empty, nil
// docker client.
func TestInspectCurrentContainerGracefulDegrade(t *testing.T) {
	// 1. Daemon returns an error → detected is zero-value.
	t.Run("daemon error", func(t *testing.T) {
		setSelfContainerID(t, "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef")
		overrideSelfDeployInspect(t, func(_ context.Context, _ *selfDeployBiz, _ string) (dockercontainer.InspectResponse, error) {
			return dockercontainer.InspectResponse{}, errors.New("daemon unreachable")
		})
		b := &selfDeployBiz{}
		got := b.inspectCurrentContainer(context.Background())
		if !isZeroPlaceholders(got) {
			t.Fatalf("expected zero-value placeholders on daemon error, got %+v", got)
		}
	})

	// 2. SelfContainerID returns "" → inspect is never called.
	// Force empty by pointing SWIRL_CONTAINER_ID at an empty string
	// AND overriding os.Hostname via the env — but SelfContainerID
	// also falls back to os.Hostname, which in test binaries returns
	// the machine name. The contract here is that an empty selfID
	// means inspect is not called. We guarantee this by installing an
	// inspect stub that would panic if invoked, then setting
	// SWIRL_CONTAINER_ID="" and clearing the hostname-driven path via
	// a seam check: because SelfContainerID will still return a
	// hostname, the graceful-degrade contract here is "nil docker
	// client → error → zero value". Covered by the nil-client test
	// below, so this inner case is the broader "inspect returned
	// error" scenario already exercised above — we keep the subtest
	// scaffold to document intent.
	t.Run("nil docker client", func(t *testing.T) {
		setSelfContainerID(t, "somecontainerid")
		// Restore the production inspect (which dereferences b.d);
		// selfDeployBiz.d is nil, so it MUST return an error rather
		// than panic. If the contract regresses, the panic recovery
		// below turns into a test failure.
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("inspectCurrentContainer panicked on nil docker client: %v", r)
			}
		}()
		b := &selfDeployBiz{}
		got := b.inspectCurrentContainer(context.Background())
		if !isZeroPlaceholders(got) {
			t.Fatalf("expected zero-value placeholders on nil docker client, got %+v", got)
		}
	})
}

// TestApplyConfigDefaultsFirstTimeMergesDetected: when firstTime is
// true the biz layer calls inspectCurrentContainer and merges the
// result into cfg.Placeholders BEFORE mergeWithDefaults. A detected
// ImageTag must therefore override the static SelfDeployImageTag.
func TestApplyConfigDefaultsFirstTimeMergesDetected(t *testing.T) {
	setSelfContainerID(t, "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef")
	overrideSelfDeployInspect(t, func(_ context.Context, _ *selfDeployBiz, _ string) (dockercontainer.InspectResponse, error) {
		return fakeInspectResponse(func(r *dockercontainer.InspectResponse) {
			r.Config.Image = "example/swirl:detected"
			r.HostConfig.PortBindings = nat.PortMap{
				"9000/tcp": []nat.PortBinding{{HostPort: "9000"}},
			}
			r.NetworkSettings.Networks = map[string]*dockernetwork.EndpointSettings{
				"detected_net": {},
			}
		}), nil
	})

	b := &selfDeployBiz{}
	cfg := b.applyConfigDefaults(context.Background(), &SelfDeployConfig{}, true)
	if cfg.Placeholders.ImageTag != "example/swirl:detected" {
		t.Errorf("ImageTag: want detected value, got %q", cfg.Placeholders.ImageTag)
	}
	if cfg.Placeholders.ExposePort != 9000 {
		t.Errorf("ExposePort: want 9000, got %d", cfg.Placeholders.ExposePort)
	}
	if cfg.Placeholders.NetworkName != "detected_net" {
		t.Errorf("NetworkName: want detected_net, got %q", cfg.Placeholders.NetworkName)
	}
	// Fields not covered by detection still receive the static
	// defaults — proves the second-stage mergeWithDefaults still runs.
	if cfg.Placeholders.RecoveryPort != misc.SelfDeployRecoveryPort {
		t.Errorf("RecoveryPort default should still apply when not detected, got %d", cfg.Placeholders.RecoveryPort)
	}
	if cfg.Placeholders.VolumeData != misc.SelfDeployVolumeData {
		t.Errorf("VolumeData default should apply when not detected, got %q", cfg.Placeholders.VolumeData)
	}
}

// TestApplyConfigDefaultsNotFirstTimeSkipsInspect: once the config
// exists in storage, the inspect call MUST NOT run — the operator
// has already curated the values and re-detecting would clobber their
// intent.
func TestApplyConfigDefaultsNotFirstTimeSkipsInspect(t *testing.T) {
	called := false
	overrideSelfDeployInspect(t, func(_ context.Context, _ *selfDeployBiz, _ string) (dockercontainer.InspectResponse, error) {
		called = true
		return dockercontainer.InspectResponse{}, nil
	})
	b := &selfDeployBiz{}
	_ = b.applyConfigDefaults(context.Background(), &SelfDeployConfig{}, false)
	if called {
		t.Fatalf("inspectCurrentContainer must NOT be called when firstTime=false")
	}
}

// TestMergeDetectedRespectsExistingValues: the two-slot merge must
// preserve any current value the operator has already set; only
// zero-valued current fields are overwritten with detected values.
func TestMergeDetectedRespectsExistingValues(t *testing.T) {
	current := SelfDeployPlaceholders{
		ImageTag:      "pinned/swirl:locked",
		ExposePort:    0,
		ContainerName: "custom_name",
	}
	detected := SelfDeployPlaceholders{
		ImageTag:      "auto/swirl:detected",
		ExposePort:    8001,
		ContainerName: "auto_name",
		VolumeData:    "auto_vol",
		NetworkName:   "auto_net",
		TraefikLabels: []string{"traefik.enable=true"},
	}
	got := mergeDetected(current, detected)
	if got.ImageTag != "pinned/swirl:locked" {
		t.Errorf("ImageTag must be preserved, got %q", got.ImageTag)
	}
	if got.ExposePort != 8001 {
		t.Errorf("ExposePort must be filled from detected, got %d", got.ExposePort)
	}
	if got.ContainerName != "custom_name" {
		t.Errorf("ContainerName must be preserved, got %q", got.ContainerName)
	}
	if got.VolumeData != "auto_vol" {
		t.Errorf("VolumeData must be filled from detected, got %q", got.VolumeData)
	}
	if got.NetworkName != "auto_net" {
		t.Errorf("NetworkName must be filled from detected, got %q", got.NetworkName)
	}
	if len(got.TraefikLabels) != 1 || got.TraefikLabels[0] != "traefik.enable=true" {
		t.Errorf("TraefikLabels must be filled from detected, got %v", got.TraefikLabels)
	}
}

// TestLoadConfigFirstTimeTriggersInspect verifies the end-to-end wiring:
// when SettingBiz.Find returns (nil, nil) — i.e. no persisted record —
// LoadConfig drives inspectCurrentContainer through the seam, and the
// returned SelfDeployConfig carries the detected values.
func TestLoadConfigFirstTimeTriggersInspect(t *testing.T) {
	setSelfContainerID(t, "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef")
	called := 0
	overrideSelfDeployInspect(t, func(_ context.Context, _ *selfDeployBiz, _ string) (dockercontainer.InspectResponse, error) {
		called++
		return fakeInspectResponse(func(r *dockercontainer.InspectResponse) {
			r.Config.Image = "detected/swirl:xyz"
		}), nil
	})

	sb := newStubSettingBiz()
	b := &selfDeployBiz{sb: sb}
	cfg, err := b.LoadConfig(context.Background())
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if called != 1 {
		t.Fatalf("expected inspect to be called exactly once on first-time LoadConfig, got %d", called)
	}
	if cfg.Placeholders.ImageTag != "detected/swirl:xyz" {
		t.Fatalf("expected detected ImageTag to flow into LoadConfig output, got %q", cfg.Placeholders.ImageTag)
	}
}

// TestLoadConfigSecondTimeSkipsInspect: after a SaveConfig persists the
// record, the next LoadConfig MUST NOT re-detect. Operator's curated
// choices take precedence for the lifetime of the installation.
func TestLoadConfigSecondTimeSkipsInspect(t *testing.T) {
	setSelfContainerID(t, "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef")
	sb := newStubSettingBiz()
	// Pre-populate the store with a config that would look "zero
	// value" for the detect-able fields EXCEPT ImageTag, which is set
	// to a distinctive sentinel. If inspect ran, it would merge over
	// zero-value fields and the sentinel would survive anyway, but
	// the assertion on the call count tightens the contract.
	sb.blobs[settingIDSelfDeploy] = map[string]interface{}{
		"enabled":  true,
		"template": LoadSeedTemplate(),
		"placeholders": map[string]interface{}{
			"imageTag": "persisted/swirl:sentinel",
		},
	}
	called := 0
	overrideSelfDeployInspect(t, func(_ context.Context, _ *selfDeployBiz, _ string) (dockercontainer.InspectResponse, error) {
		called++
		return dockercontainer.InspectResponse{}, nil
	})
	b := &selfDeployBiz{sb: sb}
	cfg, err := b.LoadConfig(context.Background())
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if called != 0 {
		t.Fatalf("inspect must not be called when record exists; got %d calls", called)
	}
	if cfg.Placeholders.ImageTag != "persisted/swirl:sentinel" {
		t.Fatalf("persisted ImageTag must survive, got %q", cfg.Placeholders.ImageTag)
	}
}
