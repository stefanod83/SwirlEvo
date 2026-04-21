package api

import (
	"errors"
	"net/http"

	auxoerrors "github.com/cuigh/auxo/errors"
	"github.com/cuigh/auxo/net/web"
	"github.com/cuigh/swirl/biz"
	"github.com/cuigh/swirl/dao"
	"github.com/cuigh/swirl/misc"
)

// respondHostError maps the biz-layer error types to the right HTTP
// status + response envelope. Centralised so hostSave and hostTest
// stay in sync.
//
//   - *biz.EndpointSuggestionError → 422 + suggestedEndpoint payload
//     (UI offers "apply and retry" dialog).
//   - *biz.WorkerRejectedError     → 422 + suggestedManagers payload
//     (UI offers "switch to manager").
//   - errors.Is ErrHostImmutable   → 403.
//   - CodedError validation codes  → 400 with the info preserved.
//   - CodedError ErrHostUnreachable→ 502 (bad gateway to the daemon)
//     so the UI distinguishes user-fixable vs connectivity issues.
//
// Returns (handled, result). When handled == false the caller should
// fall through to `ajax(c, err)` for the non-mapped case.
func respondHostError(c web.Context, err error) (bool, error) {
	if err == nil {
		return false, nil
	}
	if sugg, ok := err.(*biz.EndpointSuggestionError); ok {
		return true, c.Status(http.StatusUnprocessableEntity).Result(misc.ErrHostValidation, sugg.Error(), map[string]interface{}{
			"endpointSuggestion": true,
			"originalEndpoint":   sugg.Endpoint,
			"suggestedEndpoint":  sugg.SuggestedEndpoint,
			"authMethod":         sugg.AuthMethod,
		})
	}
	if werr, ok := err.(*biz.WorkerRejectedError); ok {
		return true, c.Status(http.StatusUnprocessableEntity).Result(1, werr.Error(), map[string]interface{}{
			"suggestedManagers": werr.SuggestedManagers,
			"workerRejected":    true,
		})
	}
	if errors.Is(err, biz.ErrHostImmutable) {
		return true, web.NewError(http.StatusForbidden, err.Error())
	}
	if ce, ok := err.(*auxoerrors.CodedError); ok {
		switch ce.Code {
		case misc.ErrHostValidation, misc.ErrHostEndpointFormat, misc.ErrHostEndpointScheme:
			return true, c.Status(http.StatusBadRequest).Result(ce.Code, ce.Error(), nil)
		case misc.ErrHostUnreachable:
			return true, c.Status(http.StatusBadGateway).Result(ce.Code, ce.Error(), nil)
		}
	}
	return false, nil
}

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
		// Bind-time validation (struct-tag `valid:"required"`) → wrap
		// as ErrHostValidation so it emits 400 with a readable body
		// instead of leaking through ajax() as a bare 500.
		if err != nil {
			err = misc.Error(misc.ErrHostValidation, err)
		} else {
			ctx, cancel := misc.Context(defaultTimeout)
			defer cancel()

			if h.ID == "" {
				err = b.Create(ctx, h, c.User())
			} else {
				err = b.Update(ctx, h, c.User())
			}
		}
		if handled, res := respondHostError(c, err); handled {
			return res
		}
		return ajax(c, err)
	}
}

func hostTest(b biz.HostBiz) web.HandlerFunc {
	type Args struct {
		Endpoint   string `json:"endpoint"`
		AuthMethod string `json:"authMethod"`
	}
	return func(c web.Context) error {
		args := &Args{}
		if err := c.Bind(args); err != nil {
			return err
		}

		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()

		info, err := b.Test(ctx, args.Endpoint, args.AuthMethod)
		if handled, res := respondHostError(c, err); handled {
			return res
		}
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
