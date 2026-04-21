package misc

import (
	"context"
	"time"

	"github.com/cuigh/auxo/errors"
)

const (
	ErrInvalidToken          = 1001
	ErrAccountDisabled       = 1002
	ErrOldPasswordIncorrect  = 1003
	ErrExternalStack         = 1004
	ErrSystemInitialized     = 1005
	ErrPasswordNotModifiable = 1006
	// ErrSelfDeployBlocked is raised when a compose stack deploy would
	// replace the very container Swirl is running inside. The operator
	// would lose the API mid-request — a classic self-destruct scenario.
	ErrSelfDeployBlocked = 1007
	// ErrVolumesContainData is raised when a Remove(removeVolumes=true)
	// would delete project volumes that carry data. The API response
	// includes the list of volumes so the UI can ask for a second
	// confirmation with force=true.
	ErrVolumesContainData = 1008
	// ErrMigrateRequiresStopped is raised when a stack migration is
	// attempted on a stack that is not in the "inactive" state.
	ErrMigrateRequiresStopped = 1009
	// ErrStackNameConflict is raised when a stack migration is attempted
	// but a stack with the same name already exists on the target host.
	ErrStackNameConflict = 1010
	// ErrHostNotFound is raised when a stack/container/etc. operation
	// references a host ID that doesn't exist in the Hosts registry.
	// Returned as HTTP 200 with `info` set so the UI can render a
	// specific "host no longer exists" message instead of a bare 500.
	ErrHostNotFound = 1011
	// ErrHostUnreachable is raised when Swirl can open a Docker client
	// for a host but the subsequent API call fails — connection refused,
	// TLS handshake error, DNS failure, etc. The `info` field embeds
	// the host ID + endpoint + the underlying cause so the operator can
	// fix the connectivity problem without digging through the server log.
	ErrHostUnreachable = 1012
	// ErrStackNotFound is raised when a stack ID (managed path) doesn't
	// resolve to a persisted record. Distinct from ErrHostNotFound so
	// the UI can tell the two apart (stale stack link vs deleted host).
	ErrStackNotFound = 1013
	// ErrStackOperationFailed is the catch-all for Docker errors bubbled
	// up from the standalone compose engine (Start/Stop/Remove). The
	// original daemon message is preserved verbatim in `info` so the
	// operator sees e.g. "No such container: ..." instead of a bare 500.
	ErrStackOperationFailed = 1014
	// ErrNetworkNotFound is raised when a network lookup (by name or id)
	// targets a host that's reachable but returns a 404. The UI can
	// tell this apart from a transport-level failure.
	ErrNetworkNotFound = 1015
	// ErrNetworkOperationFailed is the catch-all for Docker errors
	// returned by network ops (create/delete/disconnect/inspect). The
	// op name goes into `info` so the operator knows what actually
	// failed.
	ErrNetworkOperationFailed = 1016
	// ErrContainerNotFound is raised when a container lookup targets a
	// reachable host but the container id/name doesn't resolve.
	ErrContainerNotFound = 1017
	// ErrContainerOperationFailed is the catch-all for container ops
	// (start/stop/kill/restart/pause/unpause/rename/remove/exec/logs/
	// stats/prune). The op name goes into `info`.
	ErrContainerOperationFailed = 1018
	// ErrVolumeNotFound is raised when a volume lookup targets a
	// reachable host but the volume name doesn't resolve.
	ErrVolumeNotFound = 1019
	// ErrVolumeOperationFailed is the catch-all for volume ops
	// (create/delete/prune/inspect). The op name goes into `info`.
	ErrVolumeOperationFailed = 1020
	// ErrImageNotFound is raised when an image lookup targets a
	// reachable host but the image id/tag doesn't resolve.
	ErrImageNotFound = 1021
	// ErrImageOperationFailed is the catch-all for image ops
	// (remove/prune/tag/push/inspect/history). The op name goes into
	// `info`.
	ErrImageOperationFailed = 1022
	// ErrHostValidation is raised when a host form submission misses a
	// required field (name, endpoint, SSH user when auth=ssh, TLS CA
	// when auth=tcp+tls). The `info` field names the missing field so
	// the UI can surface a targeted alert.
	ErrHostValidation = 1023
	// ErrHostEndpointFormat is raised when the endpoint has no scheme
	// AND no AuthMethod is selected (no basis for a suggestion), or
	// the scheme is unknown (not in http/https/tcp/unix/ssh). `info`
	// lists the valid schemes.
	ErrHostEndpointFormat = 1024
	// ErrHostEndpointScheme is raised when the endpoint scheme
	// conflicts with the selected AuthMethod (e.g. unix:// endpoint
	// with auth=tcp). `info` describes the mismatch and the expected
	// AuthMethod for the given scheme.
	ErrHostEndpointScheme = 1025
	// ErrCrossModeMigrate is raised when a stack migration is attempted
	// between hosts whose Type differs (standalone ↔ swarm_via_swirl).
	// Label placement (labels vs deploy.labels) and resource shape
	// (cpus/mem_limit vs deploy.resources) are mode-specific, so moving
	// a stack across modes would silently break wizard-managed labels.
	ErrCrossModeMigrate = 1026
)

func Error(code int32, err error) error {
	return errors.Coded(code, err.Error())
}

func Page(count, pageIndex, pageSize int) (start, end int) {
	start = pageSize * (pageIndex - 1)
	end = pageSize * pageIndex
	if count < start {
		start, end = 0, 0
	} else if count < end {
		end = count
	}
	return
}

func Context(timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), timeout)
}
