package api

import (
	"runtime"

	"github.com/cuigh/auxo/app"
	"github.com/cuigh/auxo/data"
	"github.com/cuigh/auxo/errors"
	"github.com/cuigh/auxo/net/web"
	"github.com/cuigh/swirl/biz"
	"github.com/cuigh/swirl/dao"
	"github.com/cuigh/swirl/docker"
	"github.com/cuigh/swirl/docker/compose"
	"github.com/cuigh/swirl/misc"
)

// SystemHandler encapsulates system related handlers.
type SystemHandler struct {
	CheckState  web.HandlerFunc `path:"/check-state" auth:"*" desc:"check system state"`
	CreateAdmin web.HandlerFunc `path:"/create-admin" method:"post" auth:"*" desc:"initialize administrator account"`
	Version     web.HandlerFunc `path:"/version" auth:"*" desc:"fetch app version"`
	Summarize   web.HandlerFunc `path:"/summarize" auth:"?" desc:"fetch statistics data"`
	// Mode is intentionally public (auth:"*"). The UI calls /system/mode during
	// bootstrap — before the user logs in — to decide which menu (Swarm/Docker)
	// to render. Gating it behind auth would cause a 401 that the ajax interceptor
	// turns into a redirect to /login, breaking the login page rendering.
	Mode          web.HandlerFunc `path:"/mode" auth:"*" desc:"get operating mode"`
	AuthProviders web.HandlerFunc `path:"/auth-providers" auth:"*" desc:"list enabled external IdPs"`
}

// NewSystem creates an instance of SystemHandler
func NewSystem(d *docker.Docker, b biz.SystemBiz, ub biz.UserBiz, hb biz.HostBiz, setting *misc.Setting) *SystemHandler {
	return &SystemHandler{
		CheckState:    systemCheckState(b),
		CreateAdmin:   systemCreateAdmin(ub),
		Version:       systemVersion,
		Summarize:     systemSummarize(d, hb),
		Mode:          systemMode,
		AuthProviders: systemAuthProviders(setting),
	}
}

func systemAuthProviders(s *misc.Setting) web.HandlerFunc {
	return func(c web.Context) error {
		ldap := false
		keycloak := false
		if s != nil {
			ldap = s.LDAP.Enabled
			keycloak = s.Keycloak.Enabled && s.Keycloak.IssuerURL != "" && s.Keycloak.ClientID != ""
		}
		return success(c, data.Map{"ldap": ldap, "keycloak": keycloak})
	}
}

func systemCheckState(b biz.SystemBiz) web.HandlerFunc {
	return func(c web.Context) (err error) {
		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()

		state, err := b.CheckState(ctx)
		if err != nil {
			return err
		}
		return success(c, state)
	}
}

func systemVersion(c web.Context) (err error) {
	return success(c, data.Map{
		"version":   app.Version,
		"goVersion": runtime.Version(),
	})
}

func systemSummarize(d *docker.Docker, hb biz.HostBiz) web.HandlerFunc {
	return func(c web.Context) (err error) {
		summary := struct {
			NodeCount      int `json:"nodeCount"`
			NetworkCount   int `json:"networkCount"`
			ServiceCount   int `json:"serviceCount"`
			StackCount     int `json:"stackCount"`
			HostCount      int `json:"hostCount"`
			ContainerCount int `json:"containerCount"`
			ImageCount     int `json:"imageCount"`
		}{}

		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()

		if misc.IsStandalone() {
			hostID := c.Query("hostId")
			if hostID != "" {
				host, fErr := hb.Find(ctx, hostID)
				if fErr != nil {
					return fErr
				}
				if host == nil {
					return success(c, summary)
				}
				summary.HostCount = 1
				if host.Status == "connected" {
					if n, e := d.ContainerCount(ctx, host.ID); e == nil {
						summary.ContainerCount = n
					}
					if n, e := d.ImageCount(ctx, host.ID); e == nil {
						summary.ImageCount = n
					}
					if cli, e := d.Hosts.GetClient(host.ID, host.Endpoint); e == nil {
						if stacks, sErr := compose.NewStandaloneEngine(cli).List(ctx); sErr == nil {
							summary.StackCount = len(stacks)
						}
					}
				}
				return success(c, summary)
			}

			hosts, hErr := hb.GetAll(ctx)
			if hErr != nil {
				return hErr
			}
			summary.HostCount = len(hosts)
			for _, h := range hosts {
				if h.Status != "connected" {
					continue
				}
				if n, e := d.ContainerCount(ctx, h.ID); e == nil {
					summary.ContainerCount += n
				}
				if n, e := d.ImageCount(ctx, h.ID); e == nil {
					summary.ImageCount += n
				}
				if cli, e := d.Hosts.GetClient(h.ID, h.Endpoint); e == nil {
					if stacks, sErr := compose.NewStandaloneEngine(cli).List(ctx); sErr == nil {
						summary.StackCount += len(stacks)
					}
				}
			}
			return success(c, summary)
		}

		if summary.NodeCount, err = d.NodeCount(ctx); err != nil {
			return
		}
		if summary.NetworkCount, err = d.NetworkCount(ctx); err != nil {
			return
		}
		if summary.ServiceCount, err = d.ServiceCount(ctx); err != nil {
			return
		}
		if summary.StackCount, err = d.StackCount(ctx); err != nil {
			return
		}

		return success(c, summary)
	}
}

func systemMode(c web.Context) error {
	return success(c, data.Map{"mode": misc.Options.Mode})
}

func systemCreateAdmin(ub biz.UserBiz) web.HandlerFunc {
	return func(c web.Context) (err error) {
		args := &struct {
			Password string `json:"password"`
			*dao.User
		}{}
		if err = c.Bind(args, true); err != nil {
			return err
		}

		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()

		var count int
		if count, err = ub.Count(ctx); err == nil && count > 0 {
			return errors.Coded(misc.ErrSystemInitialized, "system was already initialized")
		}

		user := args.User
		user.Password = args.Password
		user.Admin = true
		user.Type = biz.UserTypeInternal
		_, err = ub.Create(ctx, user, nil)
		return ajax(c, err)
	}
}
