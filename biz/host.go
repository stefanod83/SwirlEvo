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

// EndpointSuggestionError is returned by validateAndNormalizeHost
// when the endpoint has no scheme but the operator has picked an
// AuthMethod that unambiguously implies one. The API handler maps
// this to HTTP 422 with `suggestedEndpoint` in the body so the UI
// can show an "apply and retry" dialog. Mirrors the pattern used by
// WorkerRejectedError.
type EndpointSuggestionError struct {
	Endpoint          string
	SuggestedEndpoint string
	AuthMethod        string
	Reason            string
}

func (e *EndpointSuggestionError) Error() string {
	if e.Reason != "" {
		return e.Reason
	}
	return "endpoint missing scheme; suggested: " + e.SuggestedEndpoint
}

// validateAndNormalizeHost trims text fields and enforces the required
// invariants at the biz layer so the API handler doesn't have to.
// Structured errors (`misc.Error` codes + `*EndpointSuggestionError`)
// are mapped to 4xx in api/host.go. Returning plain `errors.New`
// results in a bare 500 — avoid at all costs.
func validateAndNormalizeHost(host *dao.Host) error {
	host.Name = strings.TrimSpace(host.Name)
	host.Endpoint = strings.TrimSpace(host.Endpoint)
	host.SSHUser = strings.TrimSpace(host.SSHUser)

	if host.Name == "" {
		return misc.Error(misc.ErrHostValidation, errors.New("Name is required"))
	}
	if host.Endpoint == "" {
		return misc.Error(misc.ErrHostValidation, errors.New("Endpoint is required"))
	}

	// Scheme classification. Accepted set mirrors ProbeHost:
	// {http, https, tcp, unix, ssh}. `tcp+tls` on AuthMethod side
	// still resolves to scheme `tcp`.
	scheme := ""
	if idx := strings.Index(host.Endpoint, "://"); idx > 0 {
		scheme = strings.ToLower(host.Endpoint[:idx])
	}

	validSchemes := map[string]bool{
		"http": true, "https": true, "tcp": true, "unix": true, "ssh": true,
	}

	// No scheme at all: suggest one based on AuthMethod when we have
	// an unambiguous mapping, otherwise hard-fail with a listing.
	if scheme == "" {
		switch host.AuthMethod {
		case "socket":
			if strings.HasPrefix(host.Endpoint, "/") {
				return &EndpointSuggestionError{
					Endpoint:          host.Endpoint,
					SuggestedEndpoint: "unix://" + host.Endpoint,
					AuthMethod:        host.AuthMethod,
					Reason:            "Endpoint missing scheme. For Auth Method 'Docker Socket' the expected scheme is unix://",
				}
			}
			// socket without leading slash → no suggestion; fall through.
		case "tcp", "tcp+tls":
			return &EndpointSuggestionError{
				Endpoint:          host.Endpoint,
				SuggestedEndpoint: "tcp://" + host.Endpoint,
				AuthMethod:        host.AuthMethod,
				Reason:            "Endpoint missing scheme. For Auth Method 'TCP" + map[string]string{"tcp+tls": " + TLS"}[host.AuthMethod] + "' the expected scheme is tcp://",
			}
		case "ssh":
			return &EndpointSuggestionError{
				Endpoint:          host.Endpoint,
				SuggestedEndpoint: "ssh://" + host.Endpoint,
				AuthMethod:        host.AuthMethod,
				Reason:            "Endpoint missing scheme. For Auth Method 'SSH' the expected scheme is ssh://",
			}
		}
		return misc.Error(misc.ErrHostEndpointFormat, errors.New("Endpoint missing scheme. Pick an Auth Method to get a suggestion, or prefix the endpoint with one of: tcp://, unix://, ssh://, https:// (for Swarm federation)"))
	}

	if !validSchemes[scheme] {
		return misc.Error(misc.ErrHostEndpointFormat, errors.New("Endpoint scheme '"+scheme+"' is not supported. Valid schemes: tcp, unix, ssh, http, https"))
	}

	// Scheme vs AuthMethod consistency. Skipped for http/https which
	// take the federation path and ignore AuthMethod entirely.
	if scheme != "http" && scheme != "https" {
		expected := map[string]string{
			"unix": "socket",
			"tcp":  "tcp", // tcp+tls also valid
			"ssh":  "ssh",
		}[scheme]
		am := host.AuthMethod
		ok := false
		switch am {
		case expected:
			ok = true
		case "tcp+tls":
			ok = scheme == "tcp"
		case "":
			// Empty AuthMethod will be auto-filled by the classifier;
			// skip the mismatch check.
			ok = true
		}
		if !ok {
			return misc.Error(misc.ErrHostEndpointScheme, errors.New("Endpoint scheme '"+scheme+"://' does not match Auth Method '"+am+"'. Expected Auth Method: "+expected))
		}
	}

	// Conditional required fields per AuthMethod.
	if host.AuthMethod == "ssh" && host.SSHUser == "" {
		// SSHKey stays optional: operators can rely on agent auth.
		return misc.Error(misc.ErrHostValidation, errors.New("SSH User is required when Auth Method is SSH"))
	}
	if host.AuthMethod == "tcp+tls" && strings.TrimSpace(host.TLSCACert) == "" {
		// Client cert/key optional (server-verified TLS is valid).
		return misc.Error(misc.ErrHostValidation, errors.New("TLS CA Certificate is required when Auth Method is TCP + TLS"))
	}

	return nil
}

type HostBiz interface {
	Search(ctx context.Context, name, status string, pageIndex, pageSize int) ([]*dao.Host, int, error)
	Find(ctx context.Context, id string) (*dao.Host, error)
	Create(ctx context.Context, host *dao.Host, user web.User) error
	Update(ctx context.Context, host *dao.Host, user web.User) error
	Delete(ctx context.Context, id, name string, user web.User) error
	Test(ctx context.Context, endpoint, authMethod string) (*HostInfo, error)
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

	// GetAddonConfigExtract returns the decoded blob of lists extracted
	// from uploaded add-on config files (e.g. Traefik static config).
	// Consumed by AddonDiscoveryBiz to augment dropdowns of the stack-
	// editor wizard tabs.
	GetAddonConfigExtract(ctx context.Context, hostID string) (*AddonConfigExtract, error)
	// UpdateAddonConfigExtract replaces a given addon's subtree in the
	// host's extract JSON blob. Non-specified subtrees are preserved.
	UpdateAddonConfigExtract(ctx context.Context, hostID string, extract *AddonConfigExtract, user web.User) error
	// ClearAddonConfigExtract wipes a single addon subtree (e.g. "traefik")
	// from the host's extract. Passing an empty addon resets the whole blob.
	ClearAddonConfigExtract(ctx context.Context, hostID, addon string) error
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
	if err := validateAndNormalizeHost(host); err != nil {
		return err
	}
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
		// Immutable path: cosmetic-only update. Validate just the
		// cosmetic field (Name) — endpoint etc. come from the DB,
		// not from the payload, so skip the endpoint validator.
		host.Name = strings.TrimSpace(host.Name)
		if host.Name == "" {
			return misc.Error(misc.ErrHostValidation, errors.New("Name is required"))
		}
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
	if err := validateAndNormalizeHost(host); err != nil {
		return err
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

func (b *hostBiz) Test(ctx context.Context, endpoint, authMethod string) (*HostInfo, error) {
	// Reuse the host validator for the ad-hoc "Test Connection" button:
	// run it against a stub dao.Host so operators get the same
	// "scheme missing" / suggestion UX they'd see on save. Passing
	// the AuthMethod from the form lets the validator propose the
	// right scheme prefix.
	stub := &dao.Host{Endpoint: strings.TrimSpace(endpoint), Name: "test", AuthMethod: authMethod}
	if err := validateAndNormalizeHost(stub); err != nil {
		return nil, err
	}
	info, err := b.d.Hosts.TestConnection(ctx, stub.Endpoint)
	if err != nil {
		return nil, misc.Error(misc.ErrHostUnreachable, err)
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
		return nil, misc.Error(misc.ErrHostValidation, errors.New("Endpoint is required"))
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
		// Wrap the raw SDK error (which would otherwise leak through
		// api/host.go::ajax as a bare 500) with a coded unreachable
		// error so the UI can render the Docker daemon's cause.
		return nil, misc.Error(misc.ErrHostUnreachable, err)
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

// isLocalSocketEndpoint reports whether `ep` refers to the Docker unix
// socket that Swirl itself talks to. The canonical form is
// `unix:///var/run/docker.sock`, but we tolerate two common variants:
//
//	exact              unix:///var/run/docker.sock
//	trailing slash     unix:///var/run/docker.sock/
//	no scheme          /var/run/docker.sock
//
// Used by EnsureLocal to decide whether an operator-registered host
// already covers the local daemon — in which case we do NOT create the
// `local` auto-entry (dedup) and may clean up a stale one from a
// previous boot.
func isLocalSocketEndpoint(ep string) bool {
	s := strings.TrimSpace(ep)
	if s == "" {
		return false
	}
	// strip optional trailing slash for schemed URIs
	s = strings.TrimSuffix(s, "/")
	switch s {
	case "unix:///var/run/docker.sock":
		return true
	case "/var/run/docker.sock":
		return true
	}
	return false
}

// EnsureLocal creates (or cleans up) the `local` system-managed host
// entry depending on what the operator has already registered. Runs
// only in MODE=standalone. The decision matrix:
//
//	(A) `local` exists AND another non-immutable host points at the
//	    local socket → DELETE `local` (system-initiated cleanup —
//	    bypasses the ErrHostImmutable guard). One-shot self-repair
//	    for operators who upgrade to this build after having manually
//	    registered a local-socket host on an older version that
//	    created `local` unconditionally.
//	(B) `local` exists AND no other local-socket host exists →
//	    status quo, keep it. Idempotent re-boot.
//	(C) `local` does NOT exist AND another host already points at the
//	    local socket → do nothing. The operator's own record owns the
//	    local daemon; we never create a second one.
//	(D) `local` does NOT exist AND no other local-socket host exists
//	    → create `local` as before (zero-config onboarding).
func (b *hostBiz) EnsureLocal(ctx context.Context) error {
	if !misc.IsStandalone() {
		return nil
	}

	all, err := b.di.HostGetAll(ctx)
	if err != nil {
		log.Get("host").Warnf("EnsureLocal: could not list hosts: %v", err)
		return err
	}

	var (
		autoLocal      *dao.Host
		otherLocalHost *dao.Host // first non-immutable host that points at the local socket
	)
	for _, h := range all {
		if h == nil {
			continue
		}
		if h.ID == LocalHostID && h.Immutable {
			autoLocal = h
			continue
		}
		if !h.Immutable && isLocalSocketEndpoint(h.Endpoint) && otherLocalHost == nil {
			otherLocalHost = h
		}
	}

	// (A) auto-created `local` AND an operator-owned local host both exist.
	// The operator's record takes precedence — it carries the history,
	// imported stacks, chosen name, etc. Drop the dup.
	if autoLocal != nil && otherLocalHost != nil {
		// Direct DAO delete: the biz-level Delete would refuse because
		// the record is Immutable. This cleanup path is the documented
		// system-initiated exception.
		b.d.Hosts.RemoveClient(autoLocal.ID)
		if derr := b.di.HostDelete(ctx, autoLocal.ID); derr != nil {
			log.Get("host").Warnf("EnsureLocal: could not remove duplicate auto-created %q (kept operator host %q / %s): %v",
				autoLocal.ID, otherLocalHost.ID, otherLocalHost.Endpoint, derr)
			return derr
		}
		log.Get("host").Infof("EnsureLocal: removed duplicate auto-created host %q — operator host %q (%s) already covers the local socket",
			autoLocal.ID, otherLocalHost.ID, otherLocalHost.Endpoint)
		return nil
	}

	// (B) auto `local` already there, no operator dup → nothing to do.
	if autoLocal != nil {
		return nil
	}

	// (C) operator host already covers the local socket → no auto-creation.
	if otherLocalHost != nil {
		log.Get("host").Infof("EnsureLocal: skipping auto-registration — operator host %q (%s) already covers the local socket",
			otherLocalHost.ID, otherLocalHost.Endpoint)
		return nil
	}

	// (D) nothing covers the local socket → create it.
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
