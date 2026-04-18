// Package deploy_agent hosts the sidekick entry point invoked as
// `swirl deploy-agent`. The sidekick is a one-shot sibling process that
// performs the stop-old → pull → start-new → health-check cycle for the
// primary Swirl container without racing against itself (the primary may
// be torn down mid-deploy).
//
// The subcommand is dispatched in main.go by sniffing os.Args[1] before
// the auxo framework starts, so the default `./swirl` invocation keeps
// booting the web server as before.
//
// This file only carries shared constants. The lifecycle logic lives in
// agent.go (Phase 4 in the implementation plan). Phase 1 provides a
// non-functional placeholder that exits cleanly so the dispatch path
// itself is testable end-to-end.
package deploy_agent

// Environment variable names consumed by the sidekick.
const (
	// EnvJobPath points at the JSON job descriptor written by the
	// primary Swirl before spawning the sidekick. REQUIRED. Early
	// exit 2 when missing or unreadable.
	EnvJobPath = "SWIRL_SELF_DEPLOY_JOB"

	// EnvRecoveryPort overrides the port the Recovery UI binds to on
	// failure. Default: the port already carried in the job file
	// (typically 8002). OPTIONAL.
	EnvRecoveryPort = "SWIRL_RECOVERY_PORT"

	// EnvRecoveryAllow overrides the comma-separated CIDR allow-list
	// for the Recovery UI. Default: the list in the job file (itself
	// defaulted to 127.0.0.1/32 by the primary biz). OPTIONAL.
	EnvRecoveryAllow = "SWIRL_RECOVERY_ALLOW"

	// EnvRecoveryTrustProxy, when set to "1" or "true", makes the
	// Recovery UI honour X-Forwarded-For when resolving the caller IP
	// against the allow-list. OPTIONAL.
	EnvRecoveryTrustProxy = "SWIRL_RECOVERY_TRUST_PROXY"
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

// Exit codes used by Run(). Kept distinct so the spawning process can
// distinguish "configuration error" (2) from "deploy failed, recovery
// active" (3) in the future.
const (
	ExitOK        = 0
	ExitUsage     = 2
	ExitRuntime   = 1
	ExitDeployErr = 3
)
