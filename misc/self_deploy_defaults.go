package misc

import "time"

// Self-deploy default values. Kept in misc so both the biz layer
// (Phase 3+) and the sidekick (Phase 4+) consume the exact same
// constants without creating an import cycle.
const (
	// SelfDeployStackName is the docker-compose project name the
	// sidekick uses when invoking StandaloneEngine.Deploy. It MUST
	// be stable across versions — the sidekick identifies the project
	// to tear down by this name.
	SelfDeployStackName = "swirl"

	// SelfDeployContainerName is the default container name used for
	// the primary Swirl instance in the rendered compose file.
	SelfDeployContainerName = "swirl"

	// SelfDeployNetworkName is the default network name used when the
	// operator does not override it via placeholders. Matches the
	// name used by compose.standalone-bolt.yml for continuity.
	SelfDeployNetworkName = "swirl_net"

	// SelfDeployVolumeData is the default named volume that carries
	// Swirl's persistent data (BoltDB, backups, self-deploy state).
	// MUST be declared as an external named volume in the compose
	// template so it survives container re-creation — this is the
	// invariant that makes self-deploy safe.
	SelfDeployVolumeData = "swirl_data"

	// SelfDeployExposePort is the default HTTP port the primary Swirl
	// listens on.
	SelfDeployExposePort = 8001

	// SelfDeployRecoveryPort is the default port the sidekick binds
	// the Recovery UI to when a deploy fails.
	SelfDeployRecoveryPort = 8002

	// SelfDeployImageTag is the default image reference used when the
	// operator has not saved a preferred target yet. Kept generic so
	// that `Preview` against an empty config still renders a valid
	// YAML.
	SelfDeployImageTag = "cuigh/swirl:latest"

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
// only — safe by default, matches the planning document's decision
// (bind default 127.0.0.1:8002).
const SelfDeployDefaultRecoveryCIDR = "127.0.0.1/32"

// SelfDeployPlaceholders is the set of variables exposed to the
// self-deploy compose template. Every field has a sensible default
// (see DefaultSelfDeployPlaceholders) so an operator can hit Preview
// right after first boot without filling anything in.
//
// This type lives in misc (not biz) so misc.Setting can reference it
// directly without forming an import cycle (misc → biz → misc). The
// biz package re-exports it as biz.SelfDeployPlaceholders for backward
// compatibility with callers that already depend on that symbol.
type SelfDeployPlaceholders struct {
	// ImageTag is the target image, e.g. "cuigh/swirl:v2.1.0".
	// Required for a real deploy; Preview uses the default when empty.
	ImageTag string `json:"imageTag"`
	// ExposePort is the HTTP port the primary Swirl listens on.
	// Default: SelfDeployExposePort (8001).
	ExposePort int `json:"exposePort"`
	// RecoveryPort is the port the sidekick exposes the Recovery UI
	// on when a deploy fails. Default: SelfDeployRecoveryPort.
	// Anticipated by Phase 6; carried here so the template can
	// already reference it.
	RecoveryPort int `json:"recoveryPort"`
	// RecoveryAllow is the CIDR list that the Recovery UI consults
	// to gate incoming requests. Anticipated by Phase 6; biz layer
	// injects the default 127.0.0.1/32 when empty.
	RecoveryAllow []string `json:"recoveryAllow"`
	// TraefikLabels is a raw list of docker labels attached to the
	// primary service. Each entry is emitted verbatim — the operator
	// is responsible for syntactic correctness.
	TraefikLabels []string `json:"traefikLabels"`
	// VolumeData is the named external volume holding Swirl's
	// persistent data. MUST be external so self-deploy is safe
	// across container recreation. Default: SelfDeployVolumeData.
	VolumeData string `json:"volumeData"`
	// NetworkName is the external network the service attaches to.
	// Default: SelfDeployNetworkName.
	NetworkName string `json:"networkName"`
	// ContainerName is the `container_name:` emitted in the compose
	// file for the primary Swirl service. Default:
	// SelfDeployContainerName.
	ContainerName string `json:"containerName"`
	// ExtraEnv is a passthrough map of additional environment
	// variables merged into the service's `environment:` list. The
	// template iterates sorted by key so the rendered output is
	// deterministic.
	ExtraEnv map[string]string `json:"extraEnv"`
}
