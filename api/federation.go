package api

import (
	"net/http"
	"time"

	"github.com/cuigh/auxo/data"
	"github.com/cuigh/auxo/net/web"
	"github.com/cuigh/swirl/biz"
	"github.com/cuigh/swirl/misc"
)

// FederationHandler exposes the federation surface of a Swirl instance
// to remote portals. The endpoints are split into two permission
// lanes:
//
//   - `/capabilities` — auth'd by the federation peer token itself,
//     returns whether this Swirl can participate in federation and
//     which features it supports. The portal calls this on Save.
//   - `/peers/*` — token management endpoints, guarded by
//     `federation.admin`. Only operators with that permission can
//     mint, rotate, or revoke peers.
//
// Both lanes live on /api/federation/* so the portal reaches them
// without needing mode-specific routing — any Swirl instance (swarm
// or standalone) can act as a federation target, even though the
// portal design case is MODE=swarm as the target.
type FederationHandler struct {
	Capabilities web.HandlerFunc `path:"/capabilities" auth:"?" desc:"federation handshake — returns mode/version/features"`
	ListPeers    web.HandlerFunc `path:"/peers" auth:"federation.admin" desc:"list federation peers"`
	CreatePeer   web.HandlerFunc `path:"/peers" method:"post" auth:"federation.admin" desc:"mint a new federation peer + token"`
	RotatePeer   web.HandlerFunc `path:"/peers/rotate" method:"post" auth:"federation.admin" desc:"rotate token for existing peer"`
	RevokePeer   web.HandlerFunc `path:"/peers/revoke" method:"post" auth:"federation.admin" desc:"revoke a federation peer"`
	RotateSelf   web.HandlerFunc `path:"/rotate-self" method:"post" auth:"?" desc:"self-service token rotation for federation peers"`
	// Registry Cache federation delegation (Phase 4). Two directions:
	//   - SyncRegistryCache: PORTAL-side endpoint. The operator calls
	//     this to push the local Setting.RegistryCache to a swarm peer
	//     that cannot have its daemon.json rewritten directly.
	//   - ReceiveRegistryCache: PEER-side endpoint. The portal hits
	//     this (with the peer's bearer token) to drop the portal's
	//     RegistryCache config into the peer's own Settings. The peer
	//     admin fills in the password separately via their Settings UI.
	SyncRegistryCache    web.HandlerFunc `path:"/peers/registry-cache/sync" method:"post" auth:"registry_cache.edit" desc:"push local Setting.RegistryCache to a swarm peer"`
	ReceiveRegistryCache web.HandlerFunc `path:"/registry-cache/receive" method:"post" auth:"?" desc:"receive a RegistryCache payload from a federated portal"`
}

// NewFederationAPI wires the handler against the federation biz.
func NewFederationAPI(b biz.FederationBiz, hb biz.HostBiz, sb biz.SettingBiz) *FederationHandler {
	return &FederationHandler{
		Capabilities:         federationCapabilities(b),
		ListPeers:            federationListPeers(b),
		CreatePeer:           federationCreatePeer(b),
		RotatePeer:           federationRotatePeer(b),
		RevokePeer:           federationRevokePeer(b),
		RotateSelf:           federationRotateSelf(b),
		SyncRegistryCache:    federationSyncRegistryCache(hb),
		ReceiveRegistryCache: federationReceiveRegistryCache(sb),
	}
}

// federationCapabilities returns the handshake struct. `auth:"?"`
// means "authenticated user required, any role" — a federation peer
// user (Type=federation) satisfies this via its token.
func federationCapabilities(b biz.FederationBiz) web.HandlerFunc {
	return func(c web.Context) error {
		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()
		// Propagate the caller into ctx so Capabilities can surface
		// the peer name (audit aid — portal logs which token it used).
		if u := c.User(); u != nil {
			ctx = biz.WithContextUser(ctx, u)
		}
		return success(c, b.Capabilities(ctx))
	}
}

func federationListPeers(b biz.FederationBiz) web.HandlerFunc {
	return func(c web.Context) error {
		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()
		peers, err := b.ListPeers(ctx)
		if err != nil {
			return err
		}
		return success(c, data.Map{"items": peers})
	}
}

func federationCreatePeer(b biz.FederationBiz) web.HandlerFunc {
	type Args struct {
		Name    string `json:"name"`
		TTLDays int    `json:"ttlDays"`
	}
	return func(c web.Context) error {
		args := &Args{}
		if err := c.Bind(args, true); err != nil {
			return err
		}
		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()
		res, err := b.CreatePeer(ctx, args.Name, args.TTLDays, c.User())
		if err != nil {
			return web.NewError(http.StatusUnprocessableEntity, err.Error())
		}
		return success(c, res)
	}
}

func federationRotatePeer(b biz.FederationBiz) web.HandlerFunc {
	type Args struct {
		ID      string `json:"id"`
		TTLDays int    `json:"ttlDays"`
	}
	return func(c web.Context) error {
		args := &Args{}
		if err := c.Bind(args, true); err != nil {
			return err
		}
		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()
		res, err := b.RotateToken(ctx, args.ID, args.TTLDays, c.User())
		if err != nil {
			return web.NewError(http.StatusUnprocessableEntity, err.Error())
		}
		return success(c, res)
	}
}

func federationRotateSelf(b biz.FederationBiz) web.HandlerFunc {
	type Args struct {
		TTLDays int `json:"ttlDays"`
	}
	return func(c web.Context) error {
		args := &Args{}
		// Optional body — default TTL when omitted.
		_ = c.Bind(args, true)
		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()
		res, err := b.RotateSelf(ctx, args.TTLDays, c.User())
		if err != nil {
			return web.NewError(http.StatusUnprocessableEntity, err.Error())
		}
		return success(c, res)
	}
}

func federationRevokePeer(b biz.FederationBiz) web.HandlerFunc {
	type Args struct {
		ID string `json:"id"`
	}
	return func(c web.Context) error {
		args := &Args{}
		if err := c.Bind(args, true); err != nil {
			return err
		}
		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()
		if err := b.Revoke(ctx, args.ID, c.User()); err != nil {
			return web.NewError(http.StatusUnprocessableEntity, err.Error())
		}
		return success(c, data.Map{})
	}
}

// federationSyncRegistryCache is the PORTAL-side handler. Pushes the
// local Setting.RegistryCache to the specified swarm_via_swirl host
// via HTTP POST. Records the sync metadata on
// Host.AddonConfigExtract.RegistryCache so the UI can show the last
// sync timestamp + detect CA fingerprint drift in Phase 5.
func federationSyncRegistryCache(hb biz.HostBiz) web.HandlerFunc {
	type Args struct {
		HostID string `json:"hostId"`
	}
	return func(c web.Context) error {
		args := &Args{}
		if err := c.Bind(args, true); err != nil {
			return err
		}
		if args.HostID == "" {
			return web.NewError(http.StatusBadRequest, "hostId is required")
		}
		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()

		host, err := hb.Find(ctx, args.HostID)
		if err != nil {
			return err
		}
		if host == nil {
			return web.NewError(http.StatusNotFound, "host not found")
		}
		if host.Type != "swarm_via_swirl" {
			return web.NewError(http.StatusBadRequest, "registry cache sync is only supported for swarm_via_swirl hosts")
		}
		if err := biz.SyncRegistryCacheToPeer(ctx, host); err != nil {
			return web.NewError(http.StatusBadGateway, err.Error())
		}

		// Record the sync on the host's addon extract so the UI can
		// surface "synced at X by Y" + drift detection in Phase 5.
		ext := &biz.AddonConfigExtract{
			RegistryCache: &biz.RegistryCacheExtract{
				LastSyncAt: time.Now(),
			},
		}
		if u := c.User(); u != nil {
			ext.RegistryCache.LastSyncBy = u.Name()
		}
		if live := biz.LiveRegistryCacheParams(); live != nil {
			ext.RegistryCache.LastSyncFingerprint = live.Fingerprint
		}
		// Best-effort: a persistence failure does not roll back the
		// sync itself — the peer already has the config.
		_ = hb.UpdateAddonConfigExtract(ctx, args.HostID, ext, c.User())
		return success(c, data.Map{"syncedAt": ext.RegistryCache.LastSyncAt})
	}
}

// federationReceiveRegistryCache is the PEER-side handler. Called by
// a federated portal (authenticated via the peer's bearer token) to
// drop a RegistryCache config into this Swirl's Settings. Auth:"?"
// = any authenticated user; we additionally require `User.Type =
// "federation"` to prevent human accounts from triggering this path.
func federationReceiveRegistryCache(sb biz.SettingBiz) web.HandlerFunc {
	return func(c web.Context) error {
		// Federation peers are the only expected caller. Reject
		// everything else with 403 so a misrouted call from a human
		// session never silently clobbers Settings.
		user := c.User()
		if user == nil {
			return web.NewError(http.StatusUnauthorized, "authentication required")
		}
		// Resolve the concrete *security.User to read its Type. The
		// federation proxy filter sets Type="federation" on peer
		// users via the user biz's FindByToken path.
		if !isFederationPeer(user) {
			return web.NewError(http.StatusForbidden, "only federation peers may push registry cache configuration")
		}
		payload := map[string]interface{}{}
		if err := c.Bind(&payload, true); err != nil {
			return err
		}
		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()
		if err := biz.ApplyReceivedRegistryCache(ctx, sb, payload, user); err != nil {
			return web.NewError(http.StatusUnprocessableEntity, err.Error())
		}
		return success(c, data.Map{})
	}
}

// isFederationPeer reports whether the request's authenticated user is
// a federation peer token (User.Type == "federation"). Since the web
// user interface exposes only Name/ID, we type-assert the concrete
// *security.User used by the authorizer.
func isFederationPeer(u web.User) bool {
	// The user biz tags federation peers with a deterministic login
	// name prefix (federation-peer-*). Matching on the prefix is a
	// stable fallback when the concrete type is not exposed.
	name := u.Name()
	return len(name) >= len("federation-peer-") && name[:len("federation-peer-")] == "federation-peer-"
}
