package api

import (
	"github.com/cuigh/auxo/data"
	"github.com/cuigh/auxo/net/web"
	"github.com/cuigh/swirl/biz"
	"github.com/cuigh/swirl/dao"
	"github.com/cuigh/swirl/misc"
)

// ComposeStackHandler exposes Portainer-style compose stack endpoints for standalone mode.
type ComposeStackHandler struct {
	Search     web.HandlerFunc `path:"/search" auth:"stack.view" desc:"search compose stacks"`
	Find       web.HandlerFunc `path:"/find" auth:"stack.view" desc:"find compose stack by id"`
	FindDetail web.HandlerFunc `path:"/find-detail" auth:"stack.view" desc:"find detail by hostId+name"`
	Save       web.HandlerFunc `path:"/save" method:"post" auth:"stack.edit" desc:"save compose stack without deploying"`
	Deploy     web.HandlerFunc `path:"/deploy" method:"post" auth:"stack.deploy" desc:"deploy compose stack"`
	Import     web.HandlerFunc `path:"/import" method:"post" auth:"stack.edit" desc:"import an external stack"`
	Start      web.HandlerFunc `path:"/start" method:"post" auth:"stack.deploy" desc:"start compose stack"`
	Stop       web.HandlerFunc `path:"/stop" method:"post" auth:"stack.shutdown" desc:"stop compose stack"`
	Remove     web.HandlerFunc `path:"/remove" method:"post" auth:"stack.delete" desc:"remove compose stack"`
}

// NewComposeStack is registered in api.init.
func NewComposeStack(b biz.ComposeStackBiz) *ComposeStackHandler {
	return &ComposeStackHandler{
		Search:     composeStackSearch(b),
		Find:       composeStackFind(b),
		FindDetail: composeStackFindDetail(b),
		Save:       composeStackSave(b),
		Deploy:     composeStackDeploy(b),
		Import:     composeStackImport(b),
		Start:      composeStackStart(b),
		Stop:       composeStackStop(b),
		Remove:     composeStackRemove(b),
	}
}

func composeStackSearch(b biz.ComposeStackBiz) web.HandlerFunc {
	return func(c web.Context) error {
		args := &dao.ComposeStackSearchArgs{}
		if err := c.Bind(args); err != nil {
			return err
		}
		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()

		items, total, err := b.Search(ctx, args)
		if err != nil {
			return err
		}
		return success(c, data.Map{"items": items, "total": total})
	}
}

func composeStackFind(b biz.ComposeStackBiz) web.HandlerFunc {
	return func(c web.Context) error {
		id := c.Query("id")
		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()
		stack, err := b.Find(ctx, id)
		if err != nil {
			return err
		}
		return success(c, stack)
	}
}

func composeStackSave(b biz.ComposeStackBiz) web.HandlerFunc {
	return func(c web.Context) error {
		stack := &dao.ComposeStack{}
		if err := c.Bind(stack, true); err != nil {
			return err
		}
		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()
		id, err := b.Save(ctx, stack, c.User())
		if err != nil {
			return err
		}
		return success(c, data.Map{"id": id})
	}
}

func composeStackDeploy(b biz.ComposeStackBiz) web.HandlerFunc {
	type Args struct {
		dao.ComposeStack
		PullImages bool `json:"pullImages"`
	}
	return func(c web.Context) error {
		args := &Args{}
		if err := c.Bind(args, true); err != nil {
			return err
		}
		// deploy may take longer than defaultTimeout due to image pulls
		ctx, cancel := misc.Context(5 * defaultTimeout)
		defer cancel()
		id, err := b.Deploy(ctx, &args.ComposeStack, args.PullImages, c.User())
		if err != nil {
			return err
		}
		return success(c, data.Map{"id": id})
	}
}

func composeStackStart(b biz.ComposeStackBiz) web.HandlerFunc {
	type Args struct {
		ID     string `json:"id"`
		HostID string `json:"hostId"`
		Name   string `json:"name"`
	}
	return func(c web.Context) error {
		args := &Args{}
		if err := c.Bind(args); err != nil {
			return err
		}
		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()
		if args.ID != "" {
			return ajax(c, b.Start(ctx, args.ID, c.User()))
		}
		return ajax(c, b.StartExternal(ctx, args.HostID, args.Name, c.User()))
	}
}

func composeStackStop(b biz.ComposeStackBiz) web.HandlerFunc {
	type Args struct {
		ID     string `json:"id"`
		HostID string `json:"hostId"`
		Name   string `json:"name"`
	}
	return func(c web.Context) error {
		args := &Args{}
		if err := c.Bind(args); err != nil {
			return err
		}
		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()
		if args.ID != "" {
			return ajax(c, b.Stop(ctx, args.ID, c.User()))
		}
		return ajax(c, b.StopExternal(ctx, args.HostID, args.Name, c.User()))
	}
}

func composeStackRemove(b biz.ComposeStackBiz) web.HandlerFunc {
	type Args struct {
		ID            string `json:"id"`
		HostID        string `json:"hostId"`
		Name          string `json:"name"`
		RemoveVolumes bool   `json:"removeVolumes"`
	}
	return func(c web.Context) error {
		args := &Args{}
		if err := c.Bind(args); err != nil {
			return err
		}
		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()
		if args.ID != "" {
			return ajax(c, b.Remove(ctx, args.ID, args.RemoveVolumes, c.User()))
		}
		return ajax(c, b.RemoveExternal(ctx, args.HostID, args.Name, args.RemoveVolumes, c.User()))
	}
}

func composeStackFindDetail(b biz.ComposeStackBiz) web.HandlerFunc {
	return func(c web.Context) error {
		hostID := c.Query("hostId")
		name := c.Query("name")
		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()
		detail, err := b.FindDetail(ctx, hostID, name)
		if err != nil {
			return err
		}
		return success(c, detail)
	}
}

func composeStackImport(b biz.ComposeStackBiz) web.HandlerFunc {
	type Args struct {
		dao.ComposeStack
		Redeploy   bool `json:"redeploy"`
		PullImages bool `json:"pullImages"`
	}
	return func(c web.Context) error {
		args := &Args{}
		if err := c.Bind(args, true); err != nil {
			return err
		}
		ctx, cancel := misc.Context(5 * defaultTimeout)
		defer cancel()
		id, err := b.Import(ctx, &args.ComposeStack, args.Redeploy, args.PullImages, c.User())
		if err != nil {
			return err
		}
		return success(c, data.Map{"id": id})
	}
}
