package misc

import "time"

// Self-deploy default values. Kept in misc so both the biz layer
// and the sidekick consume the exact same constants without creating
// an import cycle.
//
// v3 paradigm: the template/placeholders machinery is gone. The YAML
// to deploy is read verbatim from a ComposeStack; the operator edits
// it through the normal Stack editor. Only runtime options that the
// sidekick itself consumes live here.
const (
	// SelfDeployExposePort is the default HTTP port the primary Swirl
	// listens on. The sidekick uses this to build the post-deploy
	// health-check URL (`http://127.0.0.1:<port>/api/system/mode`).
	SelfDeployExposePort = 8001

	// SelfDeployRecoveryPort is the default port the sidekick binds
	// the Recovery UI to when a deploy fails.
	SelfDeployRecoveryPort = 8002

	// SelfDeployDefaultTimeoutSec mirrors SelfDeployDefaultTimeout as a
	// plain number of seconds — convenient for JSON serialisation of
	// the persisted config, where a time.Duration would round-trip as a
	// large integer and confuse the UI.
	SelfDeployDefaultTimeoutSec = 300
)

// SelfDeployDefaultTimeout is the total deploy timeout (pull +
// start + health-check) when the operator has not customised it.
// 10 minutes covers large images on slow links without being so
// large that a truly stuck deploy stays unnoticed.
const SelfDeployDefaultTimeout = 10 * time.Minute

// SelfDeployDefaultRecoveryCIDR is the fallback allow-list entry the
// biz layer injects when the operator saves an empty list. Loopback
// only — safe by default.
const SelfDeployDefaultRecoveryCIDR = "127.0.0.1/32"
