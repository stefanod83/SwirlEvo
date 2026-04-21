package api

import (
	"context"
	"net/http"
	"runtime"
	"time"

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
	Mode web.HandlerFunc `path:"/mode" auth:"*" desc:"get operating mode"`
	// Ready is the readiness probe (public — the self-deploy sidekick
	// and the UI both poll it unauthenticated). Returns 200 only when
	// the DB is reachable, a Docker client can be resolved and the
	// settings snapshot has been hydrated. Returns 503 with a
	// "failing" list otherwise. See api/system.go::systemReady for the
	// full check set + rationale. Used instead of /mode as the gating
	// signal for self-deploy redirect-to-home to avoid the
	// "broken home page, need F5" race.
	Ready         web.HandlerFunc `path:"/ready" auth:"*" desc:"readiness probe (DB + Docker client + settings)"`
	AuthProviders web.HandlerFunc `path:"/auth-providers" auth:"*" desc:"list enabled external IdPs"`
}

// NewSystem creates an instance of SystemHandler
func NewSystem(d *docker.Docker, b biz.SystemBiz, ub biz.UserBiz, hb biz.HostBiz, di dao.Interface, setting *misc.Setting) *SystemHandler {
	return &SystemHandler{
		CheckState:    systemCheckState(b),
		CreateAdmin:   systemCreateAdmin(ub),
		Version:       systemVersion,
		Summarize:     systemSummarize(d, hb),
		Mode:          systemMode,
		Ready:         systemReady(d, di, setting),
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

// readinessCheckTimeout is the per-check budget. Deliberately tight: the
// endpoint is polled at 3s intervals by the self-deploy UI, so every
// individual check must answer well under that cadence even when the
// dependency is fully down.
const readinessCheckTimeout = 1 * time.Second

// systemReady is the readiness probe that both the self-deploy sidekick
// (cmd/deploy_agent/lifecycle.go::resolveHealthURL) and the UI progress
// modal (ui/src/composables/useAutoDeployProgress.ts) gate on before
// declaring the freshly-deployed Swirl "ready for users".
//
// Why a separate endpoint from /mode:
//   - /mode is the liveness probe — it answers as soon as the HTTP
//     server starts. That was the root cause of the "UI redirects too
//     early → broken home page → F5 to recover" race: the new process
//     was listening but the DB client / Docker client / settings
//     snapshot weren't wired up yet. /ready pushes the success signal
//     to the moment all of those are usable.
//   - /mode stays auth:"*" and cheap (no DB, no Docker) so the UI
//     bootstrap (login page before auth) still works when the DB is
//     down.
//
// Response contract:
//
//	200 OK   { "mode": "<swarm|standalone>", "ready": true }
//	503      { "mode": "...", "ready": false, "failing": [<names>] }
//
// All checks run SEQUENTIALLY with a 1s budget each — worst-case
// response time is bounded to ~3s even with all dependencies down.
// Sequential (not parallel) because the N=3 check set is tiny and
// sequential is easier to reason about + to add/remove checks.
func systemReady(d *docker.Docker, di dao.Interface, setting *misc.Setting) web.HandlerFunc {
	return func(c web.Context) error {
		failing := make([]string, 0, 3)

		// 1. DB ping. Always runs. The DAO interface hides the backend
		// choice — MongoDB pings the primary, BoltDB does a trivial
		// read transaction. The 1s deadline keeps both fast.
		if di == nil {
			failing = append(failing, "db")
		} else {
			ctx, cancel := context.WithTimeout(context.Background(), readinessCheckTimeout)
			if err := di.Ping(ctx); err != nil {
				failing = append(failing, "db")
			}
			cancel()
		}

		// 2. Docker client. We only assert that a client can be
		// resolved — no ping against the daemon. Rationale: creating
		// the client is what subsequent API calls need; a daemon that
		// is briefly unresponsive should not make the whole app
		// unready (the per-host Ping surface already handles that at
		// feature level). In standalone mode the client might be
		// resolved later per host; here we just need the primary
		// client construction to succeed.
		if d == nil {
			failing = append(failing, "docker")
		} else if _, err := d.Client(); err != nil {
			failing = append(failing, "docker")
		}

		// 3. Settings snapshot hydrated. The in-memory *misc.Setting
		// is populated by main.loadSetting at startup; subsystems
		// (Vault client, backup provider) hold that pointer through
		// closures. A nil pointer here means DI wiring hasn't
		// completed yet. We don't assert on any specific field value
		// (LDAP/Vault/Keycloak may all be legitimately disabled on a
		// fresh install) — only that the struct exists.
		if setting == nil {
			failing = append(failing, "settings")
		}

		if len(failing) > 0 {
			return c.Status(http.StatusServiceUnavailable).Result(1, "not ready", data.Map{
				"mode":    misc.Options.Mode,
				"ready":   false,
				"failing": failing,
			})
		}
		return success(c, data.Map{
			"mode":  misc.Options.Mode,
			"ready": true,
		})
	}
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
