package api

import (
	"github.com/cuigh/auxo/data"
	"github.com/cuigh/auxo/net/web"
	"github.com/cuigh/swirl/biz"
	"github.com/cuigh/swirl/misc"
)

// RegistryCacheHandler exposes utility endpoints for the Registry Cache
// feature (the pull-through mirror that Swirl points remote hosts at).
//
// The main configuration surface is /api/setting/{load,save} with id
// "registry_cache" — this handler only exists for side actions that do
// not fit the generic Setting CRUD: generating a self-signed CA pair
// (Phase 1), live ping (Phase 5).
type RegistryCacheHandler struct {
	GenCA web.HandlerFunc `path:"/gen-ca" method:"post" auth:"registry_cache.edit" desc:"generate a self-signed CA pair"`
	// Ping probes the live mirror URL (GET /v2/) and returns latency
	// + status. Read-only + idempotent, so the view permission is
	// sufficient. Used by the Settings tab badge + the per-host
	// bootstrap panel to reassure operators that the mirror is
	// actually reachable before they copy-paste the script.
	Ping  web.HandlerFunc `path:"/ping" method:"post" auth:"registry_cache.view" desc:"probe the configured mirror URL"`
}

// NewRegistryCache wires a new RegistryCacheHandler. Nothing is injected
// because GenerateCAPair is a pure function with no DI dependencies.
func NewRegistryCache() *RegistryCacheHandler {
	return &RegistryCacheHandler{
		GenCA: registryCacheGenCA(),
		Ping:  registryCachePing(),
	}
}

func registryCacheGenCA() web.HandlerFunc {
	type Args struct {
		CommonName string `json:"commonName"`
	}
	return func(c web.Context) error {
		args := &Args{}
		// Body is optional — a missing/unparseable payload falls
		// through to the default CN in GenerateCAPair.
		_ = c.Bind(args)
		certPEM, keyPEM, err := biz.GenerateCAPair(args.CommonName)
		if err != nil {
			return err
		}
		return success(c, data.Map{
			"certPEM": certPEM,
			"keyPEM":  keyPEM,
		})
	}
}

func registryCachePing() web.HandlerFunc {
	return func(c web.Context) error {
		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()
		return success(c, biz.PingRegistryCache(ctx))
	}
}
