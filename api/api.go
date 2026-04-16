package api

import (
	"net/http"
	"time"

	"github.com/cuigh/auxo/app/container"
	"github.com/cuigh/auxo/net/web"
	"github.com/cuigh/swirl/misc"
)

const defaultTimeout = 30 * time.Second

func ajax(ctx web.Context, err error) error {
	if err != nil {
		return err
	}
	return success(ctx, nil)
}

func success(ctx web.Context, data interface{}) error {
	return ctx.Result(0, "", data)
}

// swarmOnly wraps a handler so that it returns 404 when swirl runs in standalone mode.
func swarmOnly(h web.HandlerFunc) web.HandlerFunc {
	return func(c web.Context) error {
		if misc.IsStandalone() {
			return web.NewError(http.StatusNotFound)
		}
		return h(c)
	}
}

func init() {
	container.Put(NewSystem, container.Name("api.system"))
	container.Put(NewSetting, container.Name("api.setting"))
	container.Put(NewUser, container.Name("api.user"))
	container.Put(NewNode, container.Name("api.node"))
	container.Put(NewRegistry, container.Name("api.registry"))
	container.Put(NewNetwork, container.Name("api.network"))
	container.Put(NewService, container.Name("api.service"))
	container.Put(NewTask, container.Name("api.task"))
	container.Put(NewConfig, container.Name("api.config"))
	container.Put(NewSecret, container.Name("api.secret"))
	container.Put(NewStack, container.Name("api.stack"))
	container.Put(NewImage, container.Name("api.image"))
	container.Put(NewContainer, container.Name("api.container"))
	container.Put(NewVolume, container.Name("api.volume"))
	container.Put(NewUser, container.Name("api.user"))
	container.Put(NewRole, container.Name("api.role"))
	container.Put(NewEvent, container.Name("api.event"))
	container.Put(NewChart, container.Name("api.chart"))
	container.Put(NewDashboard, container.Name("api.dashboard"))
	container.Put(NewHost, container.Name("api.host"))
	container.Put(NewComposeStack, container.Name("api.compose-stack"))
	container.Put(NewBackup, container.Name("api.backup"))
	container.Put(NewVault, container.Name("api.vault"))
	container.Put(NewVaultSecret, container.Name("api.vault-secret"))
	container.Put(NewComposeStackSecret, container.Name("api.compose-stack-secret"))
}
