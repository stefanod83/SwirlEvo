package api

import (
	"encoding/json"

	"github.com/cuigh/auxo/net/web"
	"github.com/cuigh/swirl/biz"
	"github.com/cuigh/swirl/misc"
	"github.com/cuigh/swirl/security"
)

// SettingHandler encapsulates setting related handlers.
type SettingHandler struct {
	Load         web.HandlerFunc `path:"/load" auth:"setting.view" desc:"load setting"`
	Save         web.HandlerFunc `path:"/save" method:"post" auth:"setting.edit" desc:"save setting"`
	KeycloakTest web.HandlerFunc `path:"/keycloak-test" auth:"setting.edit" desc:"diagnose Keycloak OIDC configuration"`
}

// NewSetting creates an instance of SettingHandler
func NewSetting(b biz.SettingBiz, kc *security.KeycloakClient) *SettingHandler {
	return &SettingHandler{
		Load:         settingLoad(b),
		Save:         settingSave(b),
		KeycloakTest: settingKeycloakTest(kc),
	}
}

func settingLoad(b biz.SettingBiz) web.HandlerFunc {
	return func(c web.Context) error {
		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()

		options, err := b.Load(ctx)
		if err != nil {
			return err
		}
		return success(c, options)
	}
}

func settingSave(b biz.SettingBiz) web.HandlerFunc {
	type Args struct {
		ID      string          `json:"id"`
		Options json.RawMessage `json:"options"`
	}

	return func(c web.Context) (err error) {
		args := &Args{}
		err = c.Bind(args)
		if err == nil {
			ctx, cancel := misc.Context(defaultTimeout)
			defer cancel()

			err = b.Save(ctx, args.ID, args.Options, c.User())
		}
		return ajax(c, err)
	}
}

func settingKeycloakTest(kc *security.KeycloakClient) web.HandlerFunc {
	return func(c web.Context) error {
		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()
		return success(c, kc.Diagnose(ctx))
	}
}
