package biz

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
	"time"

	auxocontainer "github.com/cuigh/auxo/app/container"
	"github.com/cuigh/auxo/net/web"
	"github.com/cuigh/swirl/dao"
	"github.com/cuigh/swirl/docker/compose"
	"github.com/cuigh/swirl/misc"
	dockercontainer "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/volume"
	dockerclient "github.com/docker/docker/client"
)

const (
	// Target types
	BindingTargetFile = "file"
	BindingTargetEnv  = "env"

	// Storage modes
	BindingStorageTmpfs  = "tmpfs"
	BindingStorageVolume = "volume"
	BindingStorageInit   = "init"

	// Default file mode when binding.Mode is unset.
	defaultSecretFileMode = 0400

	// Helper image used to populate secret volumes. busybox is ubiquitous
	// and tiny — the helper container only lives long enough to accept a
	// CopyToContainer call.
	secretHelperImage = "busybox:stable"

	// Label applied to helper containers / volumes we create for secret
	// materialization. Makes cleanup and discovery trivial.
	LabelSecretBinding = "com.swirl.compose.secret-binding"
	LabelSecretStack   = "com.swirl.compose.secret-stack"
)

// ComposeStackSecretBiz manages the bindings between a compose stack and
// the VaultSecret catalog, and is responsible for materializing the
// resolved secret value inside the target container.
type ComposeStackSecretBiz interface {
	ListByStack(ctx context.Context, stackID string) ([]*dao.ComposeStackSecretBinding, error)
	Find(ctx context.Context, id string) (*dao.ComposeStackSecretBinding, error)
	Upsert(ctx context.Context, binding *dao.ComposeStackSecretBinding, user web.User) (string, error)
	Delete(ctx context.Context, id string, user web.User) error
	// NewHook returns a compose.DeployHook that materializes the current
	// bindings for the given stack at deploy time. The returned hook
	// captures a snapshot of the bindings + resolved values — callers must
	// build a new hook for each Deploy invocation.
	NewHook(ctx context.Context, stackID string) (compose.DeployHook, error)
	// NewCleanupHook returns a DeployHook that performs *only* cleanup —
	// no Vault lookup is required. Used by the Remove flow so that a broken
	// Vault connection never blocks tearing down a stack.
	NewCleanupHook() compose.DeployHook
}

type composeStackSecretBiz struct {
	di     dao.Interface
	eb     EventBiz
	loader func() *misc.Setting
}

// NewComposeStackSecret wires the biz. The Vault client is looked up lazily
// via the DI container (same trick as VaultSecretBiz) to avoid the
// biz <-> vault import cycle.
func NewComposeStackSecret(di dao.Interface, eb EventBiz, s *misc.Setting) ComposeStackSecretBiz {
	return &composeStackSecretBiz{di: di, eb: eb, loader: func() *misc.Setting { return s }}
}

func (b *composeStackSecretBiz) ListByStack(ctx context.Context, stackID string) ([]*dao.ComposeStackSecretBinding, error) {
	return b.di.ComposeStackSecretBindingGetByStack(ctx, stackID)
}

func (b *composeStackSecretBiz) Find(ctx context.Context, id string) (*dao.ComposeStackSecretBinding, error) {
	return b.di.ComposeStackSecretBindingGet(ctx, id)
}

func (b *composeStackSecretBiz) Upsert(ctx context.Context, binding *dao.ComposeStackSecretBinding, user web.User) (string, error) {
	if err := b.validate(binding); err != nil {
		return "", err
	}
	binding.UpdatedAt = now()
	binding.UpdatedBy = newOperator(user)
	if binding.ID == "" {
		binding.ID = createId()
		binding.CreatedAt = binding.UpdatedAt
		binding.CreatedBy = binding.UpdatedBy
	}
	if err := b.di.ComposeStackSecretBindingUpsert(ctx, binding); err != nil {
		return "", err
	}
	return binding.ID, nil
}

func (b *composeStackSecretBiz) Delete(ctx context.Context, id string, user web.User) error {
	return b.di.ComposeStackSecretBindingDelete(ctx, id)
}

// NewCleanupHook returns a DeployHook that only reacts to AfterRemove — no
// Vault client needed. Used by Remove so a broken Vault connection doesn't
// block stack teardown (the helper containers + labeled volumes can be
// discovered and dropped from the Docker daemon alone).
func (b *composeStackSecretBiz) NewCleanupHook() compose.DeployHook {
	return cleanupHook{}
}

// cleanupHook drops helper containers + secret volumes labeled for the
// project. It skips BeforeDeploy/Apply/AfterCreate since those are not
// meaningful in a Remove flow.
type cleanupHook struct{}

func (cleanupHook) BeforeDeploy(ctx context.Context, _ *dockerclient.Client, _ string) error {
	return nil
}
func (cleanupHook) ApplyToService(ctx context.Context, _, _ string, env []string, mounts []mount.Mount) ([]string, []mount.Mount, error) {
	return env, mounts, nil
}
func (cleanupHook) AfterCreate(ctx context.Context, _ *dockerclient.Client, _, _, _ string) error {
	return nil
}
func (cleanupHook) AfterRemove(ctx context.Context, cli *dockerclient.Client, project string) error {
	// Drop helper containers (may still be holding volumes).
	list, err := cli.ContainerList(ctx, dockercontainer.ListOptions{
		All: true,
		Filters: filters.NewArgs(
			filters.Arg("label", LabelSecretStack+"="+project),
		),
	})
	if err == nil {
		for _, c := range list {
			_ = cli.ContainerRemove(ctx, c.ID, dockercontainer.RemoveOptions{Force: true})
		}
	}
	// Drop secret volumes (always project-scoped, never shared).
	vols, err := cli.VolumeList(ctx, volume.ListOptions{
		Filters: filters.NewArgs(filters.Arg("label", LabelSecretStack+"="+project)),
	})
	if err == nil {
		for _, v := range vols.Volumes {
			_ = cli.VolumeRemove(ctx, v.Name, true)
		}
	}
	return nil
}

// validate applies defaults + sanity checks. A binding with invalid fields
// is rejected upfront so malformed state never reaches a Deploy call (where
// a failure would leave containers half-configured).
func (b *composeStackSecretBiz) validate(binding *dao.ComposeStackSecretBinding) error {
	if binding == nil {
		return errors.New("binding is nil")
	}
	if binding.StackID == "" {
		return errors.New("stackId is required")
	}
	if binding.VaultSecretID == "" {
		return errors.New("vaultSecretId is required")
	}
	binding.TargetType = strings.ToLower(strings.TrimSpace(binding.TargetType))
	binding.StorageMode = strings.ToLower(strings.TrimSpace(binding.StorageMode))
	switch binding.TargetType {
	case BindingTargetFile:
		if strings.TrimSpace(binding.TargetPath) == "" {
			return errors.New("targetPath is required for file bindings")
		}
		if !strings.HasPrefix(binding.TargetPath, "/") {
			return errors.New("targetPath must be absolute")
		}
		switch binding.StorageMode {
		case "", BindingStorageTmpfs:
			binding.StorageMode = BindingStorageTmpfs
		case BindingStorageVolume, BindingStorageInit:
			// ok
		default:
			return fmt.Errorf("unsupported storage mode %q", binding.StorageMode)
		}
		if binding.Mode != "" {
			if _, err := strconv.ParseUint(binding.Mode, 8, 32); err != nil {
				return fmt.Errorf("mode must be octal (e.g. 0400), got %q", binding.Mode)
			}
		}
	case BindingTargetEnv:
		if strings.TrimSpace(binding.EnvName) == "" {
			return errors.New("envName is required for env bindings")
		}
		// StorageMode is irrelevant for env; pin a value to keep the DB clean.
		binding.StorageMode = ""
	default:
		return fmt.Errorf("unsupported target type %q", binding.TargetType)
	}
	return nil
}

// NewHook resolves all bindings of a stack once — reading each referenced
// VaultSecret and fetching its value from Vault — and returns a
// compose.DeployHook that applies them at the right points of the Deploy
// lifecycle. A failure to resolve any binding aborts the Deploy.
func (b *composeStackSecretBiz) NewHook(ctx context.Context, stackID string) (compose.DeployHook, error) {
	bindings, err := b.di.ComposeStackSecretBindingGetByStack(ctx, stackID)
	if err != nil {
		return nil, err
	}
	if len(bindings) == 0 {
		return noopHook{}, nil
	}
	vc, err := lookupVaultClient()
	if err != nil {
		return nil, err
	}
	if !vc.IsEnabled() {
		return nil, errVaultDisabled
	}
	s := b.loader()
	if s == nil {
		return nil, errors.New("settings are not loaded")
	}

	resolved := make([]resolvedBinding, 0, len(bindings))
	for _, bind := range bindings {
		rec, err := b.di.VaultSecretGet(ctx, bind.VaultSecretID)
		if err != nil {
			return nil, err
		}
		if rec == nil {
			return nil, fmt.Errorf("vault secret %s not found", bind.VaultSecretID)
		}
		logicalPath := rec.Path
		if logicalPath == "" {
			logicalPath = rec.Name
		}
		full := resolvePrefixed(s, logicalPath)
		data, err := vc.ReadKVv2(ctx, full)
		if err != nil {
			return nil, fmt.Errorf("resolve %s: %w", rec.Name, err)
		}
		value, err := extractSecretValue(data, rec.Field)
		if err != nil {
			return nil, fmt.Errorf("resolve %s: %w", rec.Name, err)
		}
		resolved = append(resolved, resolvedBinding{
			binding: bind,
			secret:  rec,
			value:   value,
			hash:    sha256Hex(value),
		})
	}
	return &materializer{
		di:       b.di,
		bindings: resolved,
	}, nil
}

// resolvedBinding couples a binding with its Vault-resolved value and hash.
// The struct never leaks out of the hook — values are zeroed once the
// deploy finishes so they don't linger in memory longer than required.
type resolvedBinding struct {
	binding *dao.ComposeStackSecretBinding
	secret  *dao.VaultSecret
	value   []byte
	hash    string
}

// noopHook satisfies the interface when a stack has no bindings.
type noopHook struct{}

func (noopHook) BeforeDeploy(ctx context.Context, _ *dockerclient.Client, _ string) error {
	return nil
}
func (noopHook) ApplyToService(ctx context.Context, _, _ string, env []string, mounts []mount.Mount) ([]string, []mount.Mount, error) {
	return env, mounts, nil
}
func (noopHook) AfterCreate(ctx context.Context, _ *dockerclient.Client, _, _, _ string) error {
	return nil
}
func (noopHook) AfterRemove(ctx context.Context, _ *dockerclient.Client, _ string) error {
	return nil
}

// materializer is the real DeployHook. It carries the resolved bindings
// (including the secret values) for the duration of a single Deploy.
type materializer struct {
	di       dao.Interface
	bindings []resolvedBinding
}

// BeforeDeploy pre-creates helper volumes for volume/init storage modes
// and populates them via a short-lived helper container using
// CopyToContainer. tmpfs bindings are handled in AfterCreate instead.
func (m *materializer) BeforeDeploy(ctx context.Context, cli *dockerclient.Client, project string) error {
	// First pass: clean any stale helper containers from a previous Deploy.
	if err := m.purgeHelpers(ctx, cli, project); err != nil {
		return err
	}
	for _, rb := range m.bindings {
		if rb.binding.TargetType != BindingTargetFile {
			continue
		}
		switch rb.binding.StorageMode {
		case BindingStorageVolume, BindingStorageInit:
			if err := m.populateVolume(ctx, cli, project, rb); err != nil {
				return err
			}
		}
	}
	return nil
}

// ApplyToService injects env vars and/or mount entries into the service
// container so the resolved secret is visible at runtime.
func (m *materializer) ApplyToService(ctx context.Context, project, service string, env []string, mounts []mount.Mount) ([]string, []mount.Mount, error) {
	for _, rb := range m.bindings {
		if !m.appliesTo(rb.binding, service) {
			continue
		}
		switch rb.binding.TargetType {
		case BindingTargetEnv:
			env = append(env, rb.binding.EnvName+"="+string(rb.value))
		case BindingTargetFile:
			switch rb.binding.StorageMode {
			case BindingStorageTmpfs:
				// tmpfs on the parent dir; file written via CopyToContainer
				// in AfterCreate. Collapsing secrets sharing the same parent
				// into a single tmpfs mount keeps /run/secrets working the
				// same way Swarm does.
				parent := path.Dir(rb.binding.TargetPath)
				if !hasTmpfsMount(mounts, parent) {
					mounts = append(mounts, mount.Mount{
						Type:   mount.TypeTmpfs,
						Target: parent,
					})
				}
			case BindingStorageVolume, BindingStorageInit:
				mounts = append(mounts, mount.Mount{
					Type:     mount.TypeVolume,
					Source:   volumeName(project, rb.binding.ID),
					Target:   path.Dir(rb.binding.TargetPath),
					ReadOnly: true,
				})
			}
		}
	}
	return env, mounts, nil
}

// AfterCreate populates tmpfs-backed secret files by CopyToContainer'ing a
// tar stream with the single secret file. This runs between ContainerCreate
// and ContainerStart so the tmpfs is writable by Docker but the service has
// not yet started.
func (m *materializer) AfterCreate(ctx context.Context, cli *dockerclient.Client, project, service, containerID string) error {
	for _, rb := range m.bindings {
		if !m.appliesTo(rb.binding, service) {
			continue
		}
		if rb.binding.TargetType != BindingTargetFile {
			continue
		}
		if rb.binding.StorageMode != BindingStorageTmpfs {
			continue
		}
		if err := copySecretToContainer(ctx, cli, containerID, rb); err != nil {
			return err
		}
	}
	// Record the deployed hash so the drift check (Fase 5) can compare.
	return m.markDeployed(ctx)
}

// AfterRemove removes helper containers and volumes scoped to this stack.
func (m *materializer) AfterRemove(ctx context.Context, cli *dockerclient.Client, project string) error {
	// Drop helper containers first (they may be holding the volumes open).
	if err := m.purgeHelpers(ctx, cli, project); err != nil {
		return err
	}
	// Drop secret volumes (they are never shared across stacks).
	vols, err := cli.VolumeList(ctx, volume.ListOptions{
		Filters: filters.NewArgs(filters.Arg("label", LabelSecretStack+"="+project)),
	})
	if err != nil {
		return err
	}
	for _, v := range vols.Volumes {
		_ = cli.VolumeRemove(ctx, v.Name, true)
	}
	return nil
}

// markDeployed records the hash + timestamp for each binding after a
// successful materialization. Best-effort: a DAO failure here is logged
// upstream but does NOT fail the deploy — the containers are already up.
func (m *materializer) markDeployed(ctx context.Context) error {
	var firstErr error
	ts := now()
	for _, rb := range m.bindings {
		rb.binding.DeployedHash = rb.hash
		rb.binding.DeployedAt = ts
		rb.binding.UpdatedAt = ts
		if err := m.di.ComposeStackSecretBindingUpsert(ctx, rb.binding); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// populateVolume creates (or reuses) a named volume per binding and writes
// the secret file into it using a helper container. For "init" mode the
// helper container is left behind and visible in the project; for "volume"
// mode it is removed immediately after the copy.
func (m *materializer) populateVolume(ctx context.Context, cli *dockerclient.Client, project string, rb resolvedBinding) error {
	volName := volumeName(project, rb.binding.ID)
	// Ensure the volume exists. VolumeCreate is idempotent when the name
	// is already present — but we still want the labels applied on first
	// create, so we inspect first.
	if _, err := cli.VolumeInspect(ctx, volName); err != nil {
		if _, err := cli.VolumeCreate(ctx, volume.CreateOptions{
			Name: volName,
			Labels: map[string]string{
				compose.LabelProject: project,
				compose.LabelManaged: "true",
				LabelSecretStack:     project,
				LabelSecretBinding:   rb.binding.ID,
			},
		}); err != nil {
			return fmt.Errorf("create secret volume %s: %w", volName, err)
		}
	}
	// Make sure the helper image is present.
	if err := ensureImage(ctx, cli, secretHelperImage); err != nil {
		return fmt.Errorf("pull helper image: %w", err)
	}
	// Create a stopped helper container that mounts the volume at /secret.
	helperName := helperContainerName(project, rb.binding.ID)
	// Remove any previous helper so the copy is deterministic.
	_ = cli.ContainerRemove(ctx, helperName, dockercontainer.RemoveOptions{Force: true})

	resp, err := cli.ContainerCreate(ctx,
		&dockercontainer.Config{
			Image:      secretHelperImage,
			Entrypoint: []string{"sh"},
			// A long-running sleep keeps the container alive only when the
			// caller asked for "init" mode; "volume" mode removes it right
			// after the copy so the command doesn't matter.
			Cmd: []string{"-c", "true"},
			Labels: map[string]string{
				compose.LabelProject: project,
				compose.LabelManaged: "true",
				LabelSecretStack:     project,
				LabelSecretBinding:   rb.binding.ID,
			},
		},
		&dockercontainer.HostConfig{
			Mounts: []mount.Mount{{
				Type:   mount.TypeVolume,
				Source: volName,
				Target: "/secret",
			}},
		},
		nil, nil, helperName)
	if err != nil {
		return fmt.Errorf("create helper: %w", err)
	}
	// Populate the file. Docker allows CopyToContainer on a stopped
	// container, so we never start the helper.
	if err := copySecretToContainerAt(ctx, cli, resp.ID, "/secret", path.Base(rb.binding.TargetPath), rb); err != nil {
		_ = cli.ContainerRemove(ctx, resp.ID, dockercontainer.RemoveOptions{Force: true})
		return err
	}
	if rb.binding.StorageMode == BindingStorageVolume {
		// In "volume" mode the helper is throw-away.
		_ = cli.ContainerRemove(ctx, resp.ID, dockercontainer.RemoveOptions{Force: true})
	}
	// In "init" mode the helper is left as-is: exited but visible to the
	// operator as a record that the init step ran.
	return nil
}

// purgeHelpers removes any helper containers previously created for this
// project — invoked at the start of Deploy to guarantee a clean slate.
func (m *materializer) purgeHelpers(ctx context.Context, cli *dockerclient.Client, project string) error {
	list, err := cli.ContainerList(ctx, dockercontainer.ListOptions{
		All: true,
		Filters: filters.NewArgs(
			filters.Arg("label", LabelSecretStack+"="+project),
		),
	})
	if err != nil {
		return err
	}
	for _, c := range list {
		_ = cli.ContainerRemove(ctx, c.ID, dockercontainer.RemoveOptions{Force: true})
	}
	return nil
}

// appliesTo reports whether a binding should be materialized for the given
// service. An empty Service field means "all services".
func (m *materializer) appliesTo(b *dao.ComposeStackSecretBinding, service string) bool {
	return b.Service == "" || b.Service == service
}

// ---- helpers -----------------------------------------------------------

// extractSecretValue selects the value from a KVv2 payload. An explicit
// field name wins; otherwise, if there's a single field we return it;
// otherwise we marshal the whole object as compact JSON so consumers can
// still get every attribute.
func extractSecretValue(kv map[string]any, field string) ([]byte, error) {
	if field != "" {
		v, ok := kv[field]
		if !ok {
			return nil, fmt.Errorf("field %q not found", field)
		}
		return toBytes(v), nil
	}
	if len(kv) == 1 {
		for _, v := range kv {
			return toBytes(v), nil
		}
	}
	return json.Marshal(kv)
}

func toBytes(v any) []byte {
	switch t := v.(type) {
	case string:
		return []byte(t)
	case []byte:
		return t
	default:
		b, _ := json.Marshal(t)
		return b
	}
}

// copySecretToContainer writes the secret as a single tar entry under the
// configured TargetPath. Used for tmpfs mode (target container directly).
func copySecretToContainer(ctx context.Context, cli *dockerclient.Client, containerID string, rb resolvedBinding) error {
	return copySecretToContainerAt(ctx, cli, containerID,
		path.Dir(rb.binding.TargetPath), path.Base(rb.binding.TargetPath), rb)
}

// copySecretToContainerAt writes the secret into a specific directory of a
// container using a minimal tar archive. Applied mode/uid/gid come from
// the binding (defaults: 0400, 0, 0).
func copySecretToContainerAt(ctx context.Context, cli *dockerclient.Client, containerID, dir, name string, rb resolvedBinding) error {
	mode := int64(defaultSecretFileMode)
	if rb.binding.Mode != "" {
		if parsed, err := strconv.ParseInt(rb.binding.Mode, 8, 32); err == nil {
			mode = parsed
		}
	}
	buf := &bytes.Buffer{}
	tw := tar.NewWriter(buf)
	hdr := &tar.Header{
		Name:    name,
		Mode:    mode,
		Size:    int64(len(rb.value)),
		Uid:     rb.binding.UID,
		Gid:     rb.binding.GID,
		ModTime: time.Now(),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	if _, err := tw.Write(rb.value); err != nil {
		return err
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return cli.CopyToContainer(ctx, containerID, dir, buf, dockercontainer.CopyToContainerOptions{
		AllowOverwriteDirWithFile: false,
	})
}

// hasTmpfsMount checks whether the mounts slice already has a tmpfs
// mount at the given target — avoids duplicate mounts when several
// secrets share the same parent directory (e.g. /run/secrets).
func hasTmpfsMount(mounts []mount.Mount, target string) bool {
	for _, m := range mounts {
		if m.Type == mount.TypeTmpfs && m.Target == target {
			return true
		}
	}
	return false
}

// volumeName builds the Docker volume name for a binding. Project-scoped
// so multiple stacks with the same binding id never collide.
func volumeName(project, bindingID string) string {
	return project + "_secret_" + bindingID
}

func helperContainerName(project, bindingID string) string {
	return project + "_secret_init_" + bindingID
}

// ensureImage pulls an image if it's not already present locally.
// For the tiny helper image we don't bother streaming progress — a missing
// image at deploy time is a rare edge case.
func ensureImage(ctx context.Context, cli *dockerclient.Client, ref string) error {
	if _, _, err := cli.ImageInspectWithRaw(ctx, ref); err == nil {
		return nil
	}
	rc, err := cli.ImagePull(ctx, ref, image.PullOptions{})
	if err != nil {
		return err
	}
	defer rc.Close()
	_, err = io.Copy(io.Discard, rc)
	return err
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func init() {
	auxocontainer.Put(NewComposeStackSecret)
}
