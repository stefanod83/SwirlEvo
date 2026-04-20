package misc

import "time"

// Self-deploy default values. Kept in misc so both the biz layer
// and the sidekick consume the exact same constants without creating
// an import cycle.
//
// v3 paradigm: the template/placeholders machinery is gone and the
// recovery-UI server on the sidekick is gone too. The YAML to deploy
// is read verbatim from a ComposeStack; progress is tracked by the
// main Swirl UI via polling `/api/system/mode`. Only timing defaults
// consumed by both sides live here.
const (
	// SelfDeployExposePort is the default HTTP port the primary Swirl
	// listens on. The sidekick uses this to build the post-deploy
	// health-check URL when the new container's IP is resolved.
	SelfDeployExposePort = 8001

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
