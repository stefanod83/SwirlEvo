package biz

import (
	"context"
	"strings"
	"time"

	"github.com/cuigh/auxo/data"
	"github.com/cuigh/auxo/net/web"
	"github.com/cuigh/swirl/dao"
	"github.com/cuigh/swirl/docker"
	"github.com/cuigh/swirl/misc"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
)

type ContainerBiz interface {
	Search(ctx context.Context, node, name, status, project string, pageIndex, pageSize int) ([]*Container, int, error)
	Find(ctx context.Context, node, id string) (ctr *Container, raw string, err error)
	Delete(ctx context.Context, node, id, name string, removeAnonymousVolumes bool, user web.User) (err error)
	FetchLogs(ctx context.Context, node, id string, lines int, timestamps bool) (stdout, stderr string, err error)
	ExecCreate(ctx context.Context, node, id string, cmd string) (resp container.ExecCreateResponse, err error)
	ExecAttach(ctx context.Context, node, id string) (resp types.HijackedResponse, err error)
	ExecStart(ctx context.Context, node, id string) error
	Prune(ctx context.Context, node string, user web.User) (count int, size uint64, err error)
	Start(ctx context.Context, node, id, name string, user web.User) error
	Stop(ctx context.Context, node, id, name string, timeoutSecs int, user web.User) error
	Restart(ctx context.Context, node, id, name string, timeoutSecs int, user web.User) error
	Kill(ctx context.Context, node, id, name, signal string, user web.User) error
	Pause(ctx context.Context, node, id, name string, user web.User) error
	Unpause(ctx context.Context, node, id, name string, user web.User) error
	Rename(ctx context.Context, node, id, name, newName string, user web.User) error
	Stats(ctx context.Context, node, id string) ([]byte, error)
}

// NewContainer takes the host biz so exported methods can pre-resolve the
// target host and surface coded errors instead of bare 500s.
func NewContainer(d *docker.Docker, hb HostBiz, eb EventBiz) ContainerBiz {
	return &containerBiz{d: d, hb: hb, eb: eb}
}

type containerBiz struct {
	d  *docker.Docker
	hb HostBiz
	eb EventBiz
}

// preflight resolves the target host in standalone mode so follow-up SDK
// calls have a coded host-not-found / host-unreachable error to return.
// See networkBiz.preflight for the full rationale.
func (b *containerBiz) preflight(ctx context.Context, node string) (*dao.Host, error) {
	if !misc.IsStandalone() || node == "" || node == "-" {
		return nil, nil
	}
	_, host, err := resolveHostClient(ctx, b.d, b.hb, node)
	if err != nil {
		return nil, err
	}
	return host, nil
}

// wrapContainerOpError fixes the error-code pair for the container
// resource and forwards to the shared wrapOpError.
func wrapContainerOpError(op, name string, host *dao.Host, err error) error {
	return wrapOpError(op, "container", name, host, err, misc.ErrContainerNotFound, misc.ErrContainerOperationFailed)
}

func (b *containerBiz) Find(ctx context.Context, node, id string) (c *Container, raw string, err error) {
	var (
		cj container.InspectResponse
		r  []byte
	)

	host, err := b.preflight(ctx, node)
	if err != nil {
		return nil, "", err
	}

	cj, r, err = b.d.ContainerInspect(ctx, node, id)
	if err != nil {
		// Preserve the historical nil-on-NotFound contract: the API
		// layer emits a 404 when container == nil and the existing
		// frontends rely on it. Connectivity / other failures are
		// still surfaced as coded errors.
		if docker.IsErrNotFound(err) {
			err = nil
			return
		}
		return nil, "", wrapContainerOpError("inspect", id, host, err)
	}

	if raw, err = indentJSON(r); err == nil {
		c = newContainerDetail(&cj)
	}
	return
}

func (b *containerBiz) Search(ctx context.Context, node, name, status, project string, pageIndex, pageSize int) (containers []*Container, total int, err error) {
	host, err := b.preflight(ctx, node)
	if err != nil {
		return nil, 0, err
	}
	list, total, err := b.d.ContainerList(ctx, node, name, status, project, pageIndex, pageSize)
	if err != nil {
		return nil, 0, wrapContainerOpError("list", "", host, err)
	}

	containers = make([]*Container, len(list))
	for i, nr := range list {
		containers[i] = newContainerSummary(&nr)
	}
	return containers, total, nil
}

func (b *containerBiz) Delete(ctx context.Context, node, id, name string, removeAnonymousVolumes bool, user web.User) (err error) {
	host, err := b.preflight(ctx, node)
	if err != nil {
		return err
	}
	err = b.d.ContainerRemove(ctx, node, id, removeAnonymousVolumes)
	if err != nil {
		return wrapContainerOpError("delete", containerLabel(id, name), host, err)
	}
	b.eb.CreateContainer(EventActionDelete, node, id, name, user)
	return nil
}

func (b *containerBiz) ExecCreate(ctx context.Context, node, id, cmd string) (resp container.ExecCreateResponse, err error) {
	host, err := b.preflight(ctx, node)
	if err != nil {
		return resp, err
	}
	resp, err = b.d.ContainerExecCreate(ctx, node, id, cmd)
	if err != nil {
		return resp, wrapContainerOpError("exec-create", id, host, err)
	}
	return resp, nil
}

func (b *containerBiz) ExecAttach(ctx context.Context, node, id string) (resp types.HijackedResponse, err error) {
	host, err := b.preflight(ctx, node)
	if err != nil {
		return resp, err
	}
	resp, err = b.d.ContainerExecAttach(ctx, node, id)
	if err != nil {
		return resp, wrapContainerOpError("exec-attach", id, host, err)
	}
	return resp, nil
}

func (b *containerBiz) ExecStart(ctx context.Context, node, id string) error {
	host, err := b.preflight(ctx, node)
	if err != nil {
		return err
	}
	if err := b.d.ContainerExecStart(ctx, node, id); err != nil {
		return wrapContainerOpError("exec-start", id, host, err)
	}
	return nil
}

func (b *containerBiz) FetchLogs(ctx context.Context, node, id string, lines int, timestamps bool) (string, string, error) {
	host, err := b.preflight(ctx, node)
	if err != nil {
		return "", "", err
	}
	stdout, stderr, err := b.d.ContainerLogs(ctx, node, id, lines, timestamps)
	if err != nil {
		return "", "", wrapContainerOpError("logs", id, host, err)
	}
	return stdout.String(), stderr.String(), nil
}

func (b *containerBiz) Prune(ctx context.Context, node string, user web.User) (count int, size uint64, err error) {
	host, err := b.preflight(ctx, node)
	if err != nil {
		return 0, 0, err
	}
	var report container.PruneReport
	if report, err = b.d.ContainerPrune(ctx, node); err != nil {
		return 0, 0, wrapContainerOpError("prune", "", host, err)
	}
	count, size = len(report.ContainersDeleted), report.SpaceReclaimed
	b.eb.CreateContainer(EventActionPrune, node, "", "", user)
	return count, size, nil
}

func (b *containerBiz) Start(ctx context.Context, node, id, name string, user web.User) error {
	host, err := b.preflight(ctx, node)
	if err != nil {
		return err
	}
	if err := b.d.ContainerStart(ctx, node, id); err != nil {
		return wrapContainerOpError("start", containerLabel(id, name), host, err)
	}
	b.eb.CreateContainer(EventActionStart, node, id, name, user)
	return nil
}

func (b *containerBiz) Stop(ctx context.Context, node, id, name string, timeoutSecs int, user web.User) error {
	host, err := b.preflight(ctx, node)
	if err != nil {
		return err
	}
	if err := b.d.ContainerStop(ctx, node, id, timeoutSecs); err != nil {
		return wrapContainerOpError("stop", containerLabel(id, name), host, err)
	}
	b.eb.CreateContainer(EventActionStop, node, id, name, user)
	return nil
}

func (b *containerBiz) Restart(ctx context.Context, node, id, name string, timeoutSecs int, user web.User) error {
	host, err := b.preflight(ctx, node)
	if err != nil {
		return err
	}
	if err := b.d.ContainerRestart(ctx, node, id, timeoutSecs); err != nil {
		return wrapContainerOpError("restart", containerLabel(id, name), host, err)
	}
	b.eb.CreateContainer(EventActionRestart, node, id, name, user)
	return nil
}

func (b *containerBiz) Kill(ctx context.Context, node, id, name, signal string, user web.User) error {
	host, err := b.preflight(ctx, node)
	if err != nil {
		return err
	}
	if err := b.d.ContainerKill(ctx, node, id, signal); err != nil {
		return wrapContainerOpError("kill", containerLabel(id, name), host, err)
	}
	b.eb.CreateContainer(EventActionKill, node, id, name, user)
	return nil
}

func (b *containerBiz) Pause(ctx context.Context, node, id, name string, user web.User) error {
	host, err := b.preflight(ctx, node)
	if err != nil {
		return err
	}
	if err := b.d.ContainerPause(ctx, node, id); err != nil {
		return wrapContainerOpError("pause", containerLabel(id, name), host, err)
	}
	b.eb.CreateContainer(EventActionPause, node, id, name, user)
	return nil
}

func (b *containerBiz) Unpause(ctx context.Context, node, id, name string, user web.User) error {
	host, err := b.preflight(ctx, node)
	if err != nil {
		return err
	}
	if err := b.d.ContainerUnpause(ctx, node, id); err != nil {
		return wrapContainerOpError("unpause", containerLabel(id, name), host, err)
	}
	b.eb.CreateContainer(EventActionUnpause, node, id, name, user)
	return nil
}

func (b *containerBiz) Rename(ctx context.Context, node, id, name, newName string, user web.User) error {
	host, err := b.preflight(ctx, node)
	if err != nil {
		return err
	}
	if err := b.d.ContainerRename(ctx, node, id, newName); err != nil {
		return wrapContainerOpError("rename", containerLabel(id, name), host, err)
	}
	b.eb.CreateContainer(EventActionRename, node, id, name, user)
	return nil
}

func (b *containerBiz) Stats(ctx context.Context, node, id string) ([]byte, error) {
	host, err := b.preflight(ctx, node)
	if err != nil {
		return nil, err
	}
	raw, err := b.d.ContainerStats(ctx, node, id)
	if err != nil {
		return nil, wrapContainerOpError("stats", id, host, err)
	}
	return raw, nil
}

// containerLabel prefers the human-readable name over the bare id when
// both are available. Used to fill the {name} slot in wrapOpError so the
// operator sees "container \"webapp\" stop failed on host ..." instead
// of a 64-char hash.
func containerLabel(id, name string) string {
	if name != "" {
		return name
	}
	return id
}

type Container struct {
	ID          string              `json:"id"`
	Name        string              `json:"name"`
	Image       string              `json:"image,omitempty"`
	Command     string              `json:"command,omitempty"`
	CreatedAt   string              `json:"createdAt"`
	Ports       []*ContainerPort    `json:"ports,omitempty"`
	SizeRw      int64               `json:"sizeRw"`
	SizeRootFs  int64               `json:"sizeRootFs"`
	Labels      data.Options        `json:"labels"`
	State       string              `json:"state"`
	Status      string              `json:"status"`
	NetworkMode string              `json:"networkMode"`
	Mounts      []*ContainerMount   `json:"mounts"`
	PID         int                 `json:"pid,omitempty"`
	StartedAt   string              `json:"startedAt,omitempty"`
	// Resources reflects the effective runtime limits/reservations the
	// Docker daemon enforces on this container. Populated for the
	// detail view only (Summary doesn't carry it). Nil when the
	// container has no explicit limits configured.
	Resources   *ContainerResources `json:"resources,omitempty"`
}

// ContainerResources surfaces the subset of container.HostConfig.Resources
// that operators care about when inspecting a container. Humanised
// strings are produced alongside the raw byte/nano values so the UI can
// show "2.0 CPU" / "2 GiB" without re-parsing Go numeric types.
type ContainerResources struct {
	// CPUs is the effective limit in CPU units (e.g. 2.0 = two full
	// cores). Derived from HostConfig.NanoCPUs (nano / 1e9). Zero
	// means "no limit".
	CPUs       float64 `json:"cpus,omitempty"`
	// CPUShares is the relative weight under contention (default 1024
	// = "one CPU share"). Populated from HostConfig.CPUShares — the
	// standalone engine sets this to approximate `deploy.resources.
	// reservations.cpus` since Docker run has no hard CPU-floor knob.
	CPUShares  int64  `json:"cpuShares,omitempty"`
	// Memory (bytes). Zero means "no limit".
	Memory     int64  `json:"memory,omitempty"`
	// MemoryReservation is Docker's soft memory limit (bytes). Under
	// contention the kernel tries to keep the working set near this
	// number but does not OOM-kill on breach.
	MemoryReservation int64 `json:"memoryReservation,omitempty"`
	// MemorySwap (bytes). -1 = swap unlimited, 0 = inherit memory
	// limit, >0 = explicit cap. Surface raw so the UI can render
	// "unlimited" / "disabled" as appropriate.
	MemorySwap int64 `json:"memorySwap,omitempty"`
	// PidsLimit (0 = unlimited).
	PidsLimit int64 `json:"pidsLimit,omitempty"`
}

type ContainerPort struct {
	IP          string `json:"ip,omitempty"`
	PrivatePort uint16 `json:"privatePort,omitempty"`
	PublicPort  uint16 `json:"publicPort,omitempty"`
	Type        string `json:"type,omitempty"`
}

type ContainerMount struct {
	Type        mount.Type        `json:"type,omitempty"`
	Name        string            `json:"name,omitempty"`
	Source      string            `json:"source,omitempty"`
	Destination string            `json:"destination,omitempty"`
	Driver      string            `json:"driver,omitempty"`
	Mode        string            `json:"mode,omitempty"`
	RW          bool              `json:"rw,omitempty"`
	Propagation mount.Propagation `json:"propagation,omitempty"`
}

func newContainerMount(m container.MountPoint) *ContainerMount {
	return &ContainerMount{
		Type:        m.Type,
		Name:        m.Name,
		Source:      m.Source,
		Destination: m.Destination,
		Driver:      m.Driver,
		Mode:        m.Mode,
		RW:          m.RW,
		Propagation: m.Propagation,
	}
}

// deriveContainerState promotes the healthcheck status over the raw
// container state so the UI's State column makes the active
// healthcheck visible at a glance. The Docker SDK's container.Summary
// does not expose State.Health directly; the health annotation is
// embedded in the human-readable Status text (e.g. "Up 5 seconds
// (healthy)"). We parse that to decide:
//
//   - "(healthy)" → "healthy" (the running container's healthcheck
//     is currently passing)
//   - "(unhealthy)" → "unhealthy" (healthcheck is failing)
//   - "(health: starting)" → "starting" (healthcheck warm-up window)
//
// When the container is not running or has no healthcheck marker we
// return the raw state unchanged.
func deriveContainerState(rawState, statusText string) string {
	if rawState != "running" {
		return rawState
	}
	switch {
	case strings.Contains(statusText, "(healthy)"):
		return "healthy"
	case strings.Contains(statusText, "(unhealthy)"):
		return "unhealthy"
	case strings.Contains(statusText, "(health: starting)"):
		return "starting"
	}
	return rawState
}

func newContainerSummary(c *container.Summary) *Container {
	ctr := &Container{
		ID:          c.ID,
		Name:        strings.TrimPrefix(c.Names[0], "/"),
		Image:       normalizeImage(c.Image),
		Command:     c.Command,
		CreatedAt:   formatTime(time.Unix(c.Created, 0)),
		SizeRw:      c.SizeRw,
		SizeRootFs:  c.SizeRootFs,
		Labels:      mapToOptions(c.Labels),
		State:       deriveContainerState(c.State, c.Status),
		Status:      c.Status,
		NetworkMode: c.HostConfig.NetworkMode,
	}
	for _, p := range c.Ports {
		ctr.Ports = append(ctr.Ports, &ContainerPort{
			IP:          p.IP,
			PrivatePort: p.PrivatePort,
			PublicPort:  p.PublicPort,
			Type:        p.Type,
		})
	}
	for _, m := range c.Mounts {
		ctr.Mounts = append(ctr.Mounts, newContainerMount(m))
	}
	return ctr
}

func newContainerDetail(c *container.InspectResponse) *Container {
	created, _ := time.Parse(time.RFC3339Nano, c.Created)
	startedAt, _ := time.Parse(time.RFC3339Nano, c.State.StartedAt)
	state := c.State.Status
	if c.State.Health != nil {
		// Promote the health status over the raw container state so
		// operators immediately see which containers are actively
		// passing/failing their healthcheck — matching `docker ps`'s
		// "(healthy)" / "(unhealthy)" annotation.
		switch c.State.Health.Status {
		case "healthy":
			state = "healthy"
		case "unhealthy":
			state = "unhealthy"
		case "starting":
			state = "starting"
		}
	}
	ctr := &Container{
		ID:          c.ID,
		Name:        strings.TrimPrefix(c.Name, "/"),
		Image:       c.Image,
		CreatedAt:   formatTime(created),
		Labels:      mapToOptions(c.Config.Labels),
		State:       state,
		NetworkMode: string(c.HostConfig.NetworkMode),
		PID:         c.State.Pid,
		StartedAt:   formatTime(startedAt),
	}
	if c.SizeRw != nil {
		ctr.SizeRw = *c.SizeRw
	}
	if c.SizeRootFs != nil {
		ctr.SizeRootFs = *c.SizeRootFs
	}
	for _, m := range c.Mounts {
		ctr.Mounts = append(ctr.Mounts, newContainerMount(m))
	}
	if c.HostConfig != nil {
		r := c.HostConfig.Resources
		if r.NanoCPUs != 0 || r.CPUShares != 0 || r.Memory != 0 ||
			r.MemoryReservation != 0 || r.MemorySwap != 0 ||
			(r.PidsLimit != nil && *r.PidsLimit != 0) {
			out := &ContainerResources{
				CPUShares:         r.CPUShares,
				Memory:            r.Memory,
				MemoryReservation: r.MemoryReservation,
				MemorySwap:        r.MemorySwap,
			}
			if r.NanoCPUs != 0 {
				// Human-friendly: two decimals is enough for
				// the values a compose wizard emits (0.5, 1.0,
				// 2.0, …) and preserves enough precision for
				// edge cases like 1.25.
				out.CPUs = float64(r.NanoCPUs) / 1e9
			}
			if r.PidsLimit != nil {
				out.PidsLimit = *r.PidsLimit
			}
			ctr.Resources = out
		}
	}
	return ctr
}
