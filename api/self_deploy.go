package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/cuigh/auxo/data"
	"github.com/cuigh/auxo/net/web"
	"github.com/cuigh/swirl/biz"
	"github.com/cuigh/swirl/misc"
)

// SelfDeployHandler exposes the self-deploy feature to the UI. Mounted
// at /api/self-deploy by the auto-registration loop in main.go. The
// endpoints are deliberately conservative: LoadConfig / Preview are
// safe introspection, SaveConfig + Deploy require the higher-privilege
// self_deploy.edit and self_deploy.execute permissions respectively.
//
// Response shape is the standard Swirl {code, msg, data} envelope —
// Deploy returns HTTP 202 Accepted when the sidekick was spawned so
// the UI can distinguish "in flight" from "immediately done".
type SelfDeployHandler struct {
	LoadConfig web.HandlerFunc `path:"/load-config" auth:"self_deploy.view" desc:"load self-deploy config"`
	SaveConfig web.HandlerFunc `path:"/save-config" method:"post" auth:"self_deploy.edit" desc:"save self-deploy config"`
	Preview    web.HandlerFunc `path:"/preview" method:"post" auth:"self_deploy.view" desc:"render YAML preview"`
	Deploy     web.HandlerFunc `path:"/deploy" method:"post" auth:"self_deploy.execute" desc:"trigger self-deploy"`
	Status     web.HandlerFunc `path:"/status" auth:"self_deploy.view" desc:"get last deploy status"`
}

// NewSelfDeploy wires the handler against the SelfDeployBiz singleton
// registered in biz/biz.go::init.
func NewSelfDeploy(b biz.SelfDeployBiz) *SelfDeployHandler {
	return &SelfDeployHandler{
		LoadConfig: selfDeployLoadConfig(b),
		SaveConfig: selfDeploySaveConfig(b),
		Preview:    selfDeployPreview(b),
		Deploy:     selfDeployDeploy(b),
		Status:     selfDeployStatus(b),
	}
}

func selfDeployLoadConfig(b biz.SelfDeployBiz) web.HandlerFunc {
	return func(c web.Context) error {
		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()

		cfg, err := b.LoadConfig(ctx)
		if err != nil {
			return err
		}
		return success(c, cfg)
	}
}

func selfDeploySaveConfig(b biz.SelfDeployBiz) web.HandlerFunc {
	return func(c web.Context) error {
		cfg := &biz.SelfDeployConfig{}
		if err := c.Bind(cfg, true); err != nil {
			return err
		}
		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()

		if err := b.SaveConfig(ctx, cfg, c.User()); err != nil {
			// A template render/parse failure is a client error — surface
			// it as 422 so the UI can show a dedicated field-level
			// message instead of a generic 500.
			if isSelfDeployTemplateErr(err) {
				return web.NewError(http.StatusUnprocessableEntity, err.Error())
			}
			return err
		}
		return success(c, data.Map{})
	}
}

func selfDeployPreview(b biz.SelfDeployBiz) web.HandlerFunc {
	// The optional override body lets the UI render a dry-run of the
	// placeholders the operator just edited without persisting them
	// first. When the body is empty/omitted we fall through to Preview()
	// which uses the persisted config.
	type Args struct {
		Placeholders *biz.SelfDeployPlaceholders `json:"placeholders"`
	}
	return func(c web.Context) error {
		args := &Args{}
		// Body is optional — ignore a missing/invalid body so a bare
		// POST still works (the UI uses this for the "Preview" button
		// before any field is dirty).
		_ = c.Bind(args, true)

		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()

		var (
			yaml string
			err  error
		)
		if args.Placeholders != nil {
			// Override path: merge with saved template but render with
			// the supplied placeholders. We load the persisted config
			// to get the template; SaveConfig guarantees it's valid.
			cfg, lerr := b.LoadConfig(ctx)
			if lerr != nil {
				return lerr
			}
			tmpl := cfg.Template
			if strings.TrimSpace(tmpl) == "" {
				tmpl = biz.LoadSeedTemplate()
			}
			yaml, err = biz.RenderTemplate(tmpl, *args.Placeholders)
		} else {
			yaml, err = b.Preview(ctx)
		}
		if err != nil {
			if isSelfDeployTemplateErr(err) {
				return web.NewError(http.StatusUnprocessableEntity, err.Error())
			}
			return err
		}
		return success(c, data.Map{"yaml": yaml})
	}
}

func selfDeployDeploy(b biz.SelfDeployBiz) web.HandlerFunc {
	return func(c web.Context) error {
		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()

		job, err := b.TriggerDeploy(ctx, c.User())
		if err != nil {
			return err
		}
		// Build the recovery URL the UI will poll + display. We cannot
		// know the operator's public hostname from the backend (they
		// might be behind a reverse proxy), so we surface the bare
		// host+port combo and let the UI prefix the current
		// `window.location.host` with it.
		recoveryURL := ""
		if job.RecoveryPort > 0 {
			// The sidekick binds 127.0.0.1 by default — the UI must
			// render the URL relative to the operator's current origin,
			// not to the server's. Shipping just the port lets the UI
			// build `<scheme>//<host>:<port>` without guessing.
			recoveryURL = ":" + strconv.Itoa(job.RecoveryPort)
		}
		payload := data.Map{
			"jobId":          job.ID,
			"recoveryUrl":    recoveryURL,
			"targetImageTag": job.TargetImageTag,
		}
		// 202 Accepted — the sidekick was handed off, the actual
		// redeploy runs asynchronously in the sibling container.
		return c.Status(http.StatusAccepted).Result(0, "", payload)
	}
}

func selfDeployStatus(b biz.SelfDeployBiz) web.HandlerFunc {
	return func(c web.Context) error {
		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()

		st, err := b.Status(ctx)
		if err != nil {
			return err
		}
		// Attach a recoveryActive derived flag so the UI doesn't have to
		// translate the phase string itself. The sidekick writes
		// Phase == SelfDeployPhaseRecovery when it's serving the
		// fallback UI; every other phase is either in-flight or
		// terminal.
		recoveryActive := st.Phase == biz.SelfDeployPhaseRecovery
		return success(c, data.Map{
			"phase":          st.Phase,
			"jobId":          st.JobID,
			"error":          st.Error,
			"logTail":        st.LogTail,
			"recoveryActive": recoveryActive,
		})
	}
}

// isSelfDeployTemplateErr checks whether err originates from the
// template parser/renderer or the compose YAML validator. These are
// client-side mistakes — the operator passed a malformed template or
// an unparseable set of placeholders — so they warrant a 422 Unprocessable
// Entity instead of a 500.
func isSelfDeployTemplateErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "template") ||
		strings.Contains(msg, "rendered YAML is not a valid compose file") ||
		strings.Contains(msg, "rendered YAML is invalid")
}

