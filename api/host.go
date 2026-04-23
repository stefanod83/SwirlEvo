package api

import (
	"errors"
	"net/http"
	"time"

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
	// Addon config extract — JSON blob with lists parsed from addon
	// config files uploaded by the operator (e.g. traefik.yml). The
	// file itself is parsed client-side; only the resulting lists
	// travel here.
	AddonExtractGet   web.HandlerFunc `path:"/addon-extract-get" auth:"host.view" desc:"read addon config extract for a host"`
	AddonExtractSave  web.HandlerFunc `path:"/addon-extract-save" method:"post" auth:"host.edit" desc:"save addon config extract for a host"`
	AddonExtractClear web.HandlerFunc `path:"/addon-extract-clear" method:"post" auth:"host.edit" desc:"clear addon config extract for a host (optionally a single addon)"`
	// Registry Cache addon: per-host opt-in + live-generated daemon.json
	// snippet + bootstrap script. Separate auth scope (registry_cache.*)
	// from the generic addon-extract endpoints so ops can grant access to
	// this feature without touching the rest of the host configuration.
	RegistryCacheGet  web.HandlerFunc `path:"/registry-cache-get" auth:"registry_cache.view" desc:"read registry cache state + snippet for a host"`
	RegistryCacheSave web.HandlerFunc `path:"/registry-cache-save" method:"post" auth:"registry_cache.edit" desc:"save registry cache opt-in state for a host"`
}

// NewHost creates an instance of HostHandler.
func NewHost(b biz.HostBiz) *HostHandler {
	return &HostHandler{
		Search:            hostSearch(b),
		Find:              hostFind(b),
		Delete:            hostDelete(b),
		Save:              hostSave(b),
		Test:              hostTest(b),
		Sync:              hostSync(b),
		AddonExtractGet:   hostAddonExtractGet(b),
		AddonExtractSave:  hostAddonExtractSave(b),
		AddonExtractClear: hostAddonExtractClear(b),
		RegistryCacheGet:  hostRegistryCacheGet(b),
		RegistryCacheSave: hostRegistryCacheSave(b),
	}
}

func hostAddonExtractGet(b biz.HostBiz) web.HandlerFunc {
	return func(c web.Context) error {
		hostID := c.Query("hostId")
		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()
		extract, err := b.GetAddonConfigExtract(ctx, hostID)
		if err != nil {
			return err
		}
		return success(c, extract)
	}
}

func hostAddonExtractSave(b biz.HostBiz) web.HandlerFunc {
	type Args struct {
		HostID  string                   `json:"hostId"`
		Extract *biz.AddonConfigExtract  `json:"extract"`
	}
	return func(c web.Context) error {
		args := &Args{}
		if err := c.Bind(args, true); err != nil {
			return err
		}
		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()
		return ajax(c, b.UpdateAddonConfigExtract(ctx, args.HostID, args.Extract, c.User()))
	}
}

func hostAddonExtractClear(b biz.HostBiz) web.HandlerFunc {
	type Args struct {
		HostID string `json:"hostId"`
		Addon  string `json:"addon"`
	}
	return func(c web.Context) error {
		args := &Args{}
		if err := c.Bind(args, true); err != nil {
			return err
		}
		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()
		return ajax(c, b.ClearAddonConfigExtract(ctx, args.HostID, args.Addon))
	}
}

// hostRegistryCacheGet returns the persisted per-host registry cache
// opt-in state plus a LIVE-generated daemon.json snippet and bootstrap
// script derived from the current Setting.RegistryCache. Regenerating
// on every read keeps the UI in sync with config rotations (hostname,
// port, CA cert) without the operator having to re-click anything.
//
// Never echoes CACertPEM in cleartext beyond what the snippet/script
// embeds (needed for the copy-paste bootstrap to install the CA).
func hostRegistryCacheGet(b biz.HostBiz) web.HandlerFunc {
	return func(c web.Context) error {
		hostID := c.Query("hostId")
		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()
		extract, err := b.GetAddonConfigExtract(ctx, hostID)
		if err != nil {
			return err
		}
		rc := extract.RegistryCache
		if rc == nil {
			rc = &biz.RegistryCacheExtract{}
		}
		out := map[string]interface{}{
			"enabled":             rc.Enabled,
			"insecureMode":        rc.InsecureMode,
			"appliedAt":           rc.AppliedAt,
			"appliedBy":           rc.AppliedBy,
			"appliedFingerprint":  rc.AppliedFingerprint,
			"lastSyncAt":          rc.LastSyncAt,
			"lastSyncBy":          rc.LastSyncBy,
			"lastSyncFingerprint": rc.LastSyncFingerprint,
			"mirrorEnabled":       false,
		}
		if live := biz.LiveRegistryCacheParams(); live != nil {
			out["mirrorEnabled"] = true
			out["mirrorHostname"] = live.Hostname
			out["mirrorPort"] = live.Port
			out["mirrorFingerprint"] = live.Fingerprint
			out["daemonSnippet"] = biz.BuildDaemonSnippet(live, rc.InsecureMode)
			out["bootstrapScript"] = biz.BuildBootstrapScript(live, rc.InsecureMode)
		}
		return success(c, out)
	}
}

// hostRegistryCacheSave persists the per-host registry cache opt-in
// state. When `markApplied` is set, stamps AppliedAt=now and captures
// the current mirror CA fingerprint so Phase 5 can flag drift after a
// CA rotation. Never touches daemon.json on the target host — that
// remains a manual copy-paste step.
func hostRegistryCacheSave(b biz.HostBiz) web.HandlerFunc {
	type Args struct {
		HostID       string `json:"hostId"`
		Enabled      bool   `json:"enabled"`
		InsecureMode bool   `json:"insecureMode"`
		MarkApplied  bool   `json:"markApplied"`
	}
	return func(c web.Context) error {
		args := &Args{}
		if err := c.Bind(args, true); err != nil {
			return err
		}
		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()
		ext := &biz.AddonConfigExtract{
			RegistryCache: &biz.RegistryCacheExtract{
				Enabled:      args.Enabled,
				InsecureMode: args.InsecureMode,
			},
		}
		if args.MarkApplied {
			ext.RegistryCache.AppliedAt = time.Now()
			if live := biz.LiveRegistryCacheParams(); live != nil {
				ext.RegistryCache.AppliedFingerprint = live.Fingerprint
			}
		}
		return ajax(c, b.UpdateAddonConfigExtract(ctx, args.HostID, ext, c.User()))
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
