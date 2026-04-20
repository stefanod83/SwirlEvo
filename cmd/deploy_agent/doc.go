// Package deploy_agent hosts the sidekick entry point invoked as
// `swirl deploy-agent`. The sidekick is a one-shot sibling process that
// performs the stop-old → pull → start-new → health-check cycle for the
// primary Swirl container without racing against itself.
//
// The subcommand is dispatched in main.go by sniffing os.Args[1] before
// the auxo framework starts, so the default `./swirl` invocation keeps
// booting the web server as before.
//
// v3: the sidekick is intentionally minimal — no HTTP server, no
// recovery UI, no allow-list. Progress is surfaced via state.json on
// the shared volume; the main Swirl's UI polls `/api/system/mode` to
// detect when the new primary is online and then reads the terminal
// state via `/api/self-deploy/status`. Manual recovery (after a
// failed rollback) is documented in `docs/self-deploy.md`.
package deploy_agent

// Environment variable names consumed by the sidekick.
const (
	// EnvJobPath points at the JSON job descriptor written by the
	// primary Swirl before spawning the sidekick. REQUIRED. Early
	// exit 2 when missing or unreadable.
	EnvJobPath = "SWIRL_SELF_DEPLOY_JOB"
)

// Filesystem defaults. The DefaultStateDir is expected to live on a
// persistent volume (typically the same volume Swirl uses for /data)
// so the sidekick's state survives a container swap.
const (
	DefaultStateDir  = "/data/self-deploy"
	DefaultJobFile   = "job.json"
	DefaultStateFile = "state.json"
	DefaultLockFile  = ".lock"
)

// Exit codes used by Run().
const (
	ExitOK        = 0
	ExitUsage     = 2
	ExitRuntime   = 1
	ExitDeployErr = 3
)
