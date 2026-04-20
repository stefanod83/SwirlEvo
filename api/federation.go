package api

import (
	"net/http"

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
}

// NewFederationAPI wires the handler against the federation biz.
func NewFederationAPI(b biz.FederationBiz) *FederationHandler {
	return &FederationHandler{
		Capabilities: federationCapabilities(b),
		ListPeers:    federationListPeers(b),
		CreatePeer:   federationCreatePeer(b),
		RotatePeer:   federationRotatePeer(b),
		RevokePeer:   federationRevokePeer(b),
		RotateSelf:   federationRotateSelf(b),
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
