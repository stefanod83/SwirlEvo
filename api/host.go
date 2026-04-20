package api

import (
	"errors"
	"net/http"

	"github.com/cuigh/auxo/net/web"
	"github.com/cuigh/swirl/biz"
	"github.com/cuigh/swirl/dao"
	"github.com/cuigh/swirl/misc"
)

// HostHandler encapsulates host management endpoints.
type HostHandler struct {
	Search web.HandlerFunc `path:"/search" auth:"host.view" desc:"search hosts"`
	Find   web.HandlerFunc `path:"/find" auth:"host.view" desc:"find host by id"`
	Delete web.HandlerFunc `path:"/delete" method:"post" auth:"host.delete" desc:"delete host"`
	Save   web.HandlerFunc `path:"/save" method:"post" auth:"host.edit" desc:"create or update host"`
	Test   web.HandlerFunc `path:"/test" method:"post" auth:"host.edit" desc:"test host connection"`
	Sync   web.HandlerFunc `path:"/sync" method:"post" auth:"host.edit" desc:"sync host status"`
}

// NewHost creates an instance of HostHandler.
func NewHost(b biz.HostBiz) *HostHandler {
	return &HostHandler{
		Search: hostSearch(b),
		Find:   hostFind(b),
		Delete: hostDelete(b),
		Save:   hostSave(b),
		Test:   hostTest(b),
		Sync:   hostSync(b),
	}
}

func hostSearch(b biz.HostBiz) web.HandlerFunc {
	type Args struct {
		Name      string `bind:"name"`
		Status    string `bind:"status"`
		PageIndex int    `bind:"pageIndex"`
		PageSize  int    `bind:"pageSize"`
	}
	return func(c web.Context) error {
		args := &Args{}
		if err := c.Bind(args); err != nil {
			return err
		}

		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()

		hosts, total, err := b.Search(ctx, args.Name, args.Status, args.PageIndex, args.PageSize)
		if err != nil {
			return err
		}
		return success(c, map[string]interface{}{
			"items": hosts,
			"total": total,
		})
	}
}

func hostFind(b biz.HostBiz) web.HandlerFunc {
	return func(c web.Context) error {
		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()

		id := c.Query("id")
		host, err := b.Find(ctx, id)
		if err != nil {
			return err
		}
		return success(c, host)
	}
}

func hostDelete(b biz.HostBiz) web.HandlerFunc {
	type Args struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	return func(c web.Context) (err error) {
		args := &Args{}
		if err = c.Bind(args); err == nil {
			ctx, cancel := misc.Context(defaultTimeout)
			defer cancel()

			err = b.Delete(ctx, args.ID, args.Name, c.User())
		}
		if errors.Is(err, biz.ErrHostImmutable) {
			return web.NewError(http.StatusForbidden, err.Error())
		}
		return ajax(c, err)
	}
}

func hostSave(b biz.HostBiz) web.HandlerFunc {
	return func(c web.Context) error {
		h := &dao.Host{}
		err := c.Bind(h, true)
		if err == nil {
			ctx, cancel := misc.Context(defaultTimeout)
			defer cancel()

			if h.ID == "" {
				err = b.Create(ctx, h, c.User())
			} else {
				err = b.Update(ctx, h, c.User())
			}
		}
		if errors.Is(err, biz.ErrHostImmutable) {
			return web.NewError(http.StatusForbidden, err.Error())
		}
		// Surface worker-rejection as 422 with the manager suggestions
		// embedded — the UI reads `.data.suggestedManagers` to offer a
		// "switch to manager" action.
		if werr, ok := err.(*biz.WorkerRejectedError); ok {
			return c.Status(http.StatusUnprocessableEntity).Result(1, werr.Error(), map[string]interface{}{
				"suggestedManagers": werr.SuggestedManagers,
				"workerRejected":    true,
			})
		}
		return ajax(c, err)
	}
}

func hostTest(b biz.HostBiz) web.HandlerFunc {
	type Args struct {
		Endpoint string `json:"endpoint"`
	}
	return func(c web.Context) error {
		args := &Args{}
		if err := c.Bind(args); err != nil {
			return err
		}

		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()

		info, err := b.Test(ctx, args.Endpoint)
		if err != nil {
			return err
		}
		return success(c, info)
	}
}

func hostSync(b biz.HostBiz) web.HandlerFunc {
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

		err := b.Sync(ctx, args.ID)
		return ajax(c, err)
	}
}
