package api

import (
	"net/http"

	"github.com/cuigh/auxo/data"
	"github.com/cuigh/auxo/net/web"
	"github.com/cuigh/swirl/biz"
	"github.com/cuigh/swirl/misc"
)

// SelfDeployHandler exposes the self-deploy feature to the UI. Mounted
// at /api/self-deploy.
//
// v3 surface (simplified):
//   - /load-config  (view)    load the persisted config
//   - /save-config  (edit)    persist the config
//   - /deploy       (execute) trigger the sidekick against the source stack
//   - /status       (view)    poll the deploy state
//   - /reset        (edit)    clear a stuck .lock + abandoned state.json
//
// v2 endpoints removed: /preview, /import-from-stack. The YAML is now
// edited in the normal compose_stack pages; there is nothing to preview
// or import at the self-deploy layer.
type SelfDeployHandler struct {
	LoadConfig web.HandlerFunc `path:"/load-config" auth:"self_deploy.view" desc:"load self-deploy config"`
	SaveConfig web.HandlerFunc `path:"/save-config" method:"post" auth:"self_deploy.edit" desc:"save self-deploy config"`
	Deploy     web.HandlerFunc `path:"/deploy" method:"post" auth:"self_deploy.execute" desc:"trigger self-deploy"`
	Status     web.HandlerFunc `path:"/status" auth:"self_deploy.view" desc:"get last deploy status"`
	Reset      web.HandlerFunc `path:"/reset" method:"post" auth:"self_deploy.edit" desc:"clear a stuck self-deploy lock"`
}

// NewSelfDeploy wires the handler against the SelfDeployBiz singleton
// registered in biz/biz.go::init.
func NewSelfDeploy(b biz.SelfDeployBiz) *SelfDeployHandler {
	return &SelfDeployHandler{
		LoadConfig: selfDeployLoadConfig(b),
		SaveConfig: selfDeploySaveConfig(b),
		Deploy:     selfDeployDeploy(b),
		Status:     selfDeployStatus(b),
		Reset:      selfDeployReset(b),
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
			return web.NewError(http.StatusUnprocessableEntity, err.Error())
		}
		return success(c, data.Map{})
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
		payload := data.Map{
			"jobId":          job.ID,
			"targetImageTag": job.TargetImageTag,
			"stackName":      job.StackName,
		}
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
		return success(c, data.Map{
			"phase":             st.Phase,
			"jobId":             st.JobID,
			"error":             st.Error,
			"logTail":           st.LogTail,
			"sidekickContainer": st.SidekickContainer,
			"sidekickAlive":     st.SidekickAlive,
			"sidekickLogs":      st.SidekickLogs,
			"canReset":          st.CanReset,
		})
	}
}

func selfDeployReset(b biz.SelfDeployBiz) web.HandlerFunc {
	return func(c web.Context) error {
		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()

		reclaimed, err := b.ResetLock(ctx, c.User())
		if err != nil {
			return err
		}
		return success(c, data.Map{"reclaimed": reclaimed})
	}
}
