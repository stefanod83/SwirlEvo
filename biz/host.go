package biz

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/cuigh/auxo/log"
	"github.com/cuigh/auxo/net/web"
	"github.com/cuigh/swirl/dao"
	"github.com/cuigh/swirl/docker"
	"github.com/cuigh/swirl/misc"
)

// ErrHostImmutable is returned when the biz layer refuses to mutate
// a system-managed host (e.g. the auto-registered `local` host that
// points at Swirl's own docker socket). Mapped to HTTP 403 at the API
// boundary.
var ErrHostImmutable = errors.New("host: system-managed, cannot be edited or deleted")

// WorkerRejectedError is returned by ProbeHost when the candidate
// endpoint is a swarm *worker* node. Swirl federates with the
// *manager* — the `SuggestedManagers` list carries the manager
// addresses discovered via `Info().Swarm.RemoteManagers` so the UI
// can offer a one-click switch.
type WorkerRejectedError struct {
	SuggestedManagers []string
}

func (e *WorkerRejectedError) Error() string {
	if len(e.SuggestedManagers) > 0 {
		return "this node is a swarm worker — register the manager instead: " + strings.Join(e.SuggestedManagers, ", ")
	}
	return "this node is a swarm worker; register the cluster's manager instead"
}

// HostProbeResult is the classification output of ProbeHost. Consumed
// at Create/Update time to auto-populate `Host.Type` and surface
// warnings (stale federation token, offline manager, …) early.
type HostProbeResult struct {
	Type              string   // "standalone" | "swarm_via_swirl"
	SwarmNodeState    string   // raw Info().Swarm.LocalNodeState (for diagnostics)
	ControlAvailable  bool
	ClusterNodes      int
	SuggestedManagers []string
}

// LocalHostID is the reserved primary key of the system-managed host
// entry auto-created at boot in MODE=standalone. Pointing at
// `unix:///var/run/docker.sock`, it enables self-deploy and zero-
// config operator onboarding ("already managing the local daemon").
const LocalHostID = "local"

type HostBiz interface {
	Search(ctx context.Context, name, status string, pageIndex, pageSize int) ([]*dao.Host, int, error)
	Find(ctx context.Context, id string) (*dao.Host, error)
	Create(ctx context.Context, host *dao.Host, user web.User) error
	Update(ctx context.Context, host *dao.Host, user web.User) error
	Delete(ctx context.Context, id, name string, user web.User) error
	Test(ctx context.Context, endpoint string) (*HostInfo, error)
	Sync(ctx context.Context, id string) error
	GetAll(ctx context.Context) ([]*dao.Host, error)
	// EnsureLocal auto-registers the `local` host (immutable) pointing
	// at the Swirl process's own Docker socket. No-op if already
	// present. Invoked at boot in MODE=standalone so self-deploy and
	// local management work out of the box.
	EnsureLocal(ctx context.Context) error
	// ProbeHost classifies a candidate endpoint. Returns a
	// `*WorkerRejectedError` when the endpoint is a swarm worker.
	ProbeHost(ctx context.Context, endpoint string) (*HostProbeResult, error)
}

type HostInfo struct {
	EngineVer string `json:"engineVersion"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	CPUs      int    `json:"cpus"`
	Memory    int64  `json:"memory"`
	Hostname  string `json:"hostname"`
}

func NewHost(d *docker.Docker, di dao.Interface, eb EventBiz) HostBiz {
	return &hostBiz{d: d, di: di, eb: eb}
}

type hostBiz struct {
	d  *docker.Docker
	di dao.Interface
	eb EventBiz
}

func (b *hostBiz) Search(ctx context.Context, name, status string, pageIndex, pageSize int) ([]*dao.Host, int, error) {
	args := &dao.HostSearchArgs{
		Name:      name,
		Status:    status,
		PageIndex: pageIndex,
		PageSize:  pageSize,
	}
	return b.di.HostSearch(ctx, args)
}

func (b *hostBiz) Find(ctx context.Context, id string) (*dao.Host, error) {
	return b.di.HostGet(ctx, id)
}

func (b *hostBiz) GetAll(ctx context.Context) ([]*dao.Host, error) {
	return b.di.HostGetAll(ctx)
}

func (b *hostBiz) Create(ctx context.Context, host *dao.Host, user web.User) error {
	// Classify first so we can short-circuit workers (they never touch
	// the DB) and auto-populate Type/AuthMethod for the UI.
	probe, err := b.ProbeHost(ctx, host.Endpoint)
	if err != nil {
		return err
	}
	host.Type = probe.Type
	if host.Type == "swarm_via_swirl" {
		host.AuthMethod = "swirl"
		host.SwirlURL = strings.TrimRight(host.Endpoint, "/")
	}

	host.ID = createId()
	host.Status = "disconnected"
	host.CreatedAt = now()
	host.UpdatedAt = host.CreatedAt
	host.CreatedBy = newOperator(user)
	host.UpdatedBy = host.CreatedBy

	if cerr := b.di.HostCreate(ctx, host); cerr != nil {
		return cerr
	}
	b.eb.CreateHost(EventActionCreate, host.ID, host.Name, user)
	// Synchronous probe so the caller sees populated details on redirect.
	if host.Type != "swarm_via_swirl" {
		b.syncHost(host.ID, host.Endpoint)
	}
	return nil
}

func (b *hostBiz) Update(ctx context.Context, host *dao.Host, user web.User) error {
	// Immutable hosts (e.g. `local`) still accept cosmetic updates —
	// Name + Color — but Endpoint / AuthMethod / Type / SwirlURL are
	// preserved from the DB record so the system host can't be
	// repointed or reclassified from the UI.
	var existing *dao.Host
	if host.ID != "" {
		existing, _ = b.di.HostGet(ctx, host.ID)
	}
	if existing != nil && existing.Immutable {
		existing.Name = host.Name
		existing.Color = host.Color
		existing.UpdatedAt = now()
		existing.UpdatedBy = newOperator(user)
		if uerr := b.di.HostUpdate(ctx, existing); uerr != nil {
			return uerr
		}
		b.eb.CreateHost(EventActionUpdate, existing.ID, existing.Name, user)
		return nil
	}
	// Re-classify when the endpoint changes (or on every Update — the
	// probe is cheap, 10s timeout). Lets the UI reflect transitions
	// between standalone ↔ swarm_via_swirl without manual edits.
	probe, err := b.ProbeHost(ctx, host.Endpoint)
	if err != nil {
		return err
	}
	host.Type = probe.Type
	if host.Type == "swarm_via_swirl" {
		host.AuthMethod = "swirl"
		host.SwirlURL = strings.TrimRight(host.Endpoint, "/")
	}

	host.UpdatedAt = now()
	host.UpdatedBy = newOperator(user)

	if uerr := b.di.HostUpdate(ctx, host); uerr != nil {
		return uerr
	}
	b.eb.CreateHost(EventActionUpdate, host.ID, host.Name, user)
	b.d.Hosts.RemoveClient(host.ID)
	if host.Type != "swarm_via_swirl" {
		b.syncHost(host.ID, host.Endpoint)
	}
	return nil
}

func (b *hostBiz) Delete(ctx context.Context, id, name string, user web.User) error {
	existing, _ := b.di.HostGet(ctx, id)
	if existing != nil && existing.Immutable {
		return ErrHostImmutable
	}
	b.d.Hosts.RemoveClient(id)
	err := b.di.HostDelete(ctx, id)
	if err == nil {
		b.eb.CreateHost(EventActionDelete, id, name, user)
	}
	return err
}

func (b *hostBiz) Test(ctx context.Context, endpoint string) (*HostInfo, error) {
	info, err := b.d.Hosts.TestConnection(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	return &HostInfo{
		EngineVer: info.ServerVersion,
		OS:        info.OSType,
		Arch:      info.Architecture,
		CPUs:      info.NCPU,
		Memory:    info.MemTotal,
		Hostname:  info.Name,
	}, nil
}

func (b *hostBiz) Sync(ctx context.Context, id string) error {
	host, err := b.di.HostGet(ctx, id)
	if err != nil || host == nil {
		return err
	}
	b.syncHost(host.ID, host.Endpoint)
	return nil
}

// ProbeHost classifies a candidate endpoint by asking either the
// Docker daemon (for tcp/unix/ssh endpoints) or a remote Swirl swarm
// (for http/https URLs — the federation case, probed fully in Phase 3;
// accepted as-is here). Rejects swarm workers with a structured error
// carrying the manager IPs so the UI can offer a one-click switch.
func (b *hostBiz) ProbeHost(ctx context.Context, endpoint string) (*HostProbeResult, error) {
	trimmed := strings.TrimSpace(endpoint)
	if trimmed == "" {
		return nil, errors.New("host: empty endpoint")
	}
	// Federation candidate — capabilities probe lives in Phase 3. For
	// now, accept http/https as the federation marker so the rest of
	// the stack can wire around it. A later phase tightens validation.
	if strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://") {
		return &HostProbeResult{Type: "swarm_via_swirl"}, nil
	}

	testCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	info, err := b.d.Hosts.TestConnection(testCtx, endpoint)
	if err != nil {
		return nil, err
	}

	state := info.Swarm.LocalNodeState
	result := &HostProbeResult{
		SwarmNodeState:   string(state),
		ControlAvailable: info.Swarm.ControlAvailable,
		ClusterNodes:     info.Swarm.Nodes,
	}

	// Active swarm without manager privileges → worker node. Reject
	// with manager-suggestion list so the UI can bounce.
	if string(state) == "active" && !info.Swarm.ControlAvailable {
		managers := make([]string, 0, len(info.Swarm.RemoteManagers))
		for _, m := range info.Swarm.RemoteManagers {
			if m.Addr != "" {
				managers = append(managers, m.Addr)
			}
		}
		return nil, &WorkerRejectedError{SuggestedManagers: managers}
	}

	if string(state) == "active" && info.Swarm.ControlAvailable {
		// Direct swarm manager access is not supported in the
		// federation architecture. Operators must deploy Swirl inside
		// the cluster and federate via URL+token instead.
		managers := make([]string, 0, len(info.Swarm.RemoteManagers))
		for _, m := range info.Swarm.RemoteManagers {
			if m.Addr != "" {
				managers = append(managers, m.Addr)
			}
		}
		return nil, errors.New("direct swarm manager socket is not supported — deploy a Swirl instance inside the cluster (MODE=swarm) and register it via its https:// URL with a federation token")
	}

	// All other states (inactive, locked, error, pending, unknown) →
	// treat as a standalone daemon. `locked` and `error` will surface
	// via the follow-up syncHost / status refresh.
	result.Type = "standalone"
	return result, nil
}

// EnsureLocal creates the `local` system-managed host entry if it
// doesn't already exist. Idempotent. Intentionally swallows "exists"
// errors so concurrent boots (unlikely but possible) don't panic.
// Only runs when MODE=standalone — in swarm mode the concept of
// local-socket-host is irrelevant (swirl talks to its daemon
// directly without going through the Hosts registry).
func (b *hostBiz) EnsureLocal(ctx context.Context) error {
	if !misc.IsStandalone() {
		return nil
	}
	existing, err := b.di.HostGet(ctx, LocalHostID)
	if err == nil && existing != nil {
		return nil
	}
	host := &dao.Host{
		ID:         LocalHostID,
		Name:       "local",
		Endpoint:   "unix:///var/run/docker.sock",
		AuthMethod: "socket",
		Status:     "disconnected",
		Type:       "standalone",
		Immutable:  true,
		CreatedAt:  now(),
		UpdatedAt:  now(),
	}
	if cerr := b.di.HostCreate(ctx, host); cerr != nil {
		log.Get("host").Warnf("EnsureLocal: could not create system host %q: %v", LocalHostID, cerr)
		return cerr
	}
	log.Get("host").Infof("EnsureLocal: auto-registered system host %q (endpoint %s)", LocalHostID, host.Endpoint)
	// Fire-and-forget probe so EngineVer/OS/Arch are populated before
	// the first UI load.
	go b.syncHost(host.ID, host.Endpoint)
	return nil
}

func (b *hostBiz) syncHost(id, endpoint string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	info, err := b.d.Hosts.TestConnection(ctx, endpoint)
	if err != nil {
		_ = b.di.HostUpdateStatus(context.Background(), id, "error", err.Error(), "", "", "", 0, 0)
		return
	}

	_ = b.di.HostUpdateStatus(context.Background(), id, "connected", "",
		info.ServerVersion, info.OSType, info.Architecture, info.NCPU, info.MemTotal)
	// Ensure client is cached for future operations
	_, _ = b.d.Hosts.GetClient(id, endpoint)
}
