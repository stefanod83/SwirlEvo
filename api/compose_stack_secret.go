package api

import (
	"github.com/cuigh/auxo/data"
	"github.com/cuigh/auxo/net/web"
	"github.com/cuigh/swirl/biz"
	"github.com/cuigh/swirl/dao"
	"github.com/cuigh/swirl/misc"
)

// ComposeStackSecretHandler exposes CRUD on compose stack ↔ Vault secret
// bindings. These bindings are only meaningful in standalone mode (Swarm
// has native docker-secret support), but the endpoints are registered in
// both modes so the UI can be generic. The underlying biz layer does not
// assume a Swarm/standalone context.
type ComposeStackSecretHandler struct {
	List   web.HandlerFunc `path:"/list" auth:"stack.view" desc:"list secret bindings of a compose stack"`
	Find   web.HandlerFunc `path:"/find" auth:"stack.view" desc:"find binding by id"`
	Save   web.HandlerFunc `path:"/save" method:"post" auth:"stack.edit" desc:"create or update a compose stack secret binding"`
	Delete web.HandlerFunc `path:"/delete" method:"post" auth:"stack.edit" desc:"delete a compose stack secret binding"`
	Drift  web.HandlerFunc `path:"/drift" auth:"stack.view" desc:"compare bindings against current Vault values (read-only)"`
}

// NewComposeStackSecret is registered in api.init.
func NewComposeStackSecret(b biz.ComposeStackSecretBiz) *ComposeStackSecretHandler {
	return &ComposeStackSecretHandler{
		List:   composeStackSecretList(b),
		Find:   composeStackSecretFind(b),
		Save:   composeStackSecretSave(b),
		Delete: composeStackSecretDelete(b),
		Drift:  composeStackSecretDrift(b),
	}
}

func composeStackSecretList(b biz.ComposeStackSecretBiz) web.HandlerFunc {
	return func(c web.Context) error {
		stackID := c.Query("stackId")
		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()
		items, err := b.ListByStack(ctx, stackID)
		if err != nil {
			return err
		}
		return success(c, items)
	}
}

func composeStackSecretFind(b biz.ComposeStackSecretBiz) web.HandlerFunc {
	return func(c web.Context) error {
		id := c.Query("id")
		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()
		binding, err := b.Find(ctx, id)
		if err != nil {
			return err
		}
		return success(c, binding)
	}
}

func composeStackSecretSave(b biz.ComposeStackSecretBiz) web.HandlerFunc {
	return func(c web.Context) error {
		binding := &dao.ComposeStackSecretBinding{}
		if err := c.Bind(binding, true); err != nil {
			return ajax(c, err)
		}
		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()
		id, err := b.Upsert(ctx, binding, c.User())
		if err != nil {
			return ajax(c, err)
		}
		return success(c, data.Map{"id": id})
	}
}

func composeStackSecretDelete(b biz.ComposeStackSecretBiz) web.HandlerFunc {
	type Args struct {
		ID string `json:"id"`
	}
	return func(c web.Context) error {
		args := &Args{}
		if err := c.Bind(args); err != nil {
			return err
		}
		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()
		return ajax(c, b.Delete(ctx, args.ID, c.User()))
	}
}

func composeStackSecretDrift(b biz.ComposeStackSecretBiz) web.HandlerFunc {
	return func(c web.Context) error {
		stackID := c.Query("stackId")
		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()
		statuses, err := b.CheckDrift(ctx, stackID)
		if err != nil {
			return err
		}
		return success(c, statuses)
	}
}
