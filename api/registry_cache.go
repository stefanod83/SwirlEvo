package api

import (
	"github.com/cuigh/auxo/data"
	"github.com/cuigh/auxo/net/web"
	"github.com/cuigh/swirl/biz"
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
}

// NewRegistryCache wires a new RegistryCacheHandler. Nothing is injected
// because GenerateCAPair is a pure function with no DI dependencies.
func NewRegistryCache() *RegistryCacheHandler {
	return &RegistryCacheHandler{
		GenCA: registryCacheGenCA(),
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
