package main

import (
	"context"
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/cuigh/auxo/app"
	"github.com/cuigh/auxo/app/container"
	"github.com/cuigh/auxo/app/flag"
	"github.com/cuigh/auxo/data"
	"github.com/cuigh/auxo/data/valid"
	"github.com/cuigh/auxo/errors"
	"github.com/cuigh/auxo/log"
	"github.com/cuigh/auxo/net/web"
	"github.com/cuigh/auxo/net/web/filter"
	"github.com/cuigh/auxo/util/run"
	_ "github.com/cuigh/swirl/api"
	"github.com/cuigh/swirl/backup"
	"github.com/cuigh/swirl/biz"
	deployagent "github.com/cuigh/swirl/cmd/deploy_agent"
	_ "github.com/cuigh/swirl/dao/bolt"
	_ "github.com/cuigh/swirl/dao/mongo"
	"github.com/cuigh/swirl/misc"
	"github.com/cuigh/swirl/scaler"
	"github.com/cuigh/swirl/vault"
)

var (
	//go:embed ui/dist
	webFS embed.FS
)

func main() {
	// Subcommand dispatch: intercept `./swirl deploy-agent ...` before
	// auxo's flag parser sees the arguments. The sidekick is a one-shot
	// sibling process that drives the self-deploy lifecycle (stop old →
	// pull → start new → health-check) without racing the primary Swirl.
	// Keeping the sniff in main() means the default `./swirl` invocation
	// path is unchanged — `os.Exit` below never runs for the server case.
	if len(os.Args) > 1 && os.Args[1] == "deploy-agent" {
		os.Exit(deployagent.Run())
	}

	app.Name = "Swirl"
	app.Version = "2.0.0rc1"
	app.Desc = "A web management UI for Docker, focused on swarm cluster"
	app.Action = func(ctx *app.Context) error {
		return run.Pipeline(misc.LoadOptions, initSystem, initBackupKeyProvider, initLocalHost, startFederationRotator, scaler.Start, backup.Start, startServer)
	}
	app.Flags.Register(flag.All)
	app.Start()
}

func startServer() (err error) {
	s := web.Auto()
	s.Validator = &valid.Validator{}
	s.ErrorHandler.Default = handleError
	s.Use(filter.NewRecover())
	s.Static("/", http.FS(loadWebFS()), "index.html")

	const prefix = "api."
	// Filter order: identifier (attach user) → federation_proxy
	// (short-circuit on federation hosts) → authorizer (permission
	// check; skipped for federation because the request flies off).
	g := s.Group("/api", findFilters("identifier", "federation_proxy", "authorizer")...)
	container.Range(func(name string, service interface{}) bool {
		if strings.HasPrefix(name, prefix) {
			g.Handle("/"+name[len(prefix):], service)
		}
		return true
	})

	app.Run(s)
	return
}

func loadWebFS() fs.FS {
	sub, err := fs.Sub(webFS, "ui/dist")
	if err != nil {
		panic(err)
	}
	return sub
}

func handleError(ctx web.Context, err error) {
	var (
		status       = http.StatusInternalServerError
		code   int32 = 1
	)

	if e, ok := err.(*web.Error); ok {
		status = e.Status()
	}
	if e, ok := err.(*errors.CodedError); ok {
		code = e.Code
	}

	err = ctx.Status(status).Result(code, err.Error(), nil)
	if err != nil {
		ctx.Logger().Error(err)
	}
}

func findFilters(names ...string) []web.Filter {
	var filters []web.Filter
	for _, name := range names {
		filters = append(filters, container.Find(name).(web.Filter))
	}
	return filters
}

func initSystem() error {
	return container.Call(func(b biz.SystemBiz) error {
		ctx, cancel := misc.Context(time.Minute)
		defer cancel()

		return b.Init(ctx)
	})
}

// initLocalHost auto-registers the system-managed `local` host entry
// in standalone mode so self-deploy and zero-config local daemon
// management work out of the box. No-op in swarm mode.
// A failure here is NOT fatal — the rest of the app runs without the
// pre-registered local entry (operator can create it manually).
func initLocalHost() error {
	return container.Call(func(hb biz.HostBiz) error {
		ctx, cancel := misc.Context(10 * time.Second)
		defer cancel()
		if err := hb.EnsureLocal(ctx); err != nil {
			// Degrade gracefully — log in the biz layer already.
			return nil
		}
		return nil
	})
}

// startFederationRotator launches the portal-side ticker that
// auto-rotates federation tokens before they expire. No-op in swarm
// mode (the rotator is a portal concern).
func startFederationRotator() error {
	return container.Call(func(r *biz.FederationRotator) {
		// Background context — the ticker runs for the lifetime of
		// the Swirl process. The auxo framework does not expose a
		// shutdown hook, so we don't bother plumbing cancellation:
		// process exit tears down the goroutine.
		r.Start(context.Background())
	})
}

// initBackupKeyProvider installs the Vault-backed fallback for
// SWIRL_BACKUP_KEY. Runs after Settings are loaded but before the backup
// scheduler starts, so the very first scheduler tick can already source the
// passphrase from Vault when env is empty. A missing/invalid Vault config is
// not fatal — masterKey() will simply fall back to errMissingKey.
func initBackupKeyProvider() error {
	return container.Call(func(c *vault.Client, s *misc.Setting) {
		if c == nil {
			return
		}
		biz.SetBackupKeyProvider(vault.NewBackupKeyProvider(c, func() *misc.Setting { return s }))
	})
}

func loadSetting(sb biz.SettingBiz) *misc.Setting {
	var (
		err  error
		opts data.Map
		b    []byte
		s    = &misc.Setting{}
	)

	ctx, cancel := misc.Context(30 * time.Second)
	defer cancel()

	// LoadRaw (not Load) — the bootstrap snapshot must carry real
	// values. Load sanitizes sensitive fields (vault.token, secret_id,
	// keycloak.client_secret) with the UI mask placeholder, and using
	// it here would leave liveSettings with "••••••••" as the actual
	// secret_id at boot, breaking any auth that depends on it.
	if opts, err = sb.LoadRaw(ctx); err == nil {
		if b, err = json.Marshal(opts); err == nil {
			err = json.Unmarshal(b, s)
		}
	}
	if err != nil {
		log.Get("misc").Error("failed to load setting: ", err)
	}
	// Hand the live pointer to the biz layer so subsequent settings
	// saves can refresh it in place — keeps the Vault client + backup
	// key provider closures up to date without a restart.
	biz.SetLiveSettings(s)
	return s
}

func init() {
	container.Put(loadSetting)
}
