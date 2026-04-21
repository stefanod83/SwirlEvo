package biz

import (
	"context"
	"fmt"
	"strings"

	"github.com/cuigh/swirl/dao"
	"github.com/cuigh/swirl/docker"
	"github.com/cuigh/swirl/misc"
	dockerclient "github.com/docker/docker/client"
)

// resolveHostClient turns a host ID into a (client, host, nil) triple, or
// a coded error the API layer can emit as a structured 200/info response
// instead of a bare 500.
//
// The coded error contract:
//
//   - hb.Find returns nil (or hostID is empty) → ErrHostNotFound.
//   - hb.Find returns an internal error → the raw error bubbles up
//     unchanged (DB failure is not a host-classification problem).
//   - d.Hosts.GetClient fails (TLS, socket, DNS, bad endpoint) →
//     ErrHostUnreachable with host ID + endpoint in info.
//
// Callers use the returned *dao.Host to feed wrapOpError when the
// follow-up engine call fails — the host reference is load-bearing for
// the operator's UX ("which daemon couldn't I reach?").
func resolveHostClient(ctx context.Context, d *docker.Docker, hb HostBiz, hostID string) (*dockerclient.Client, *dao.Host, error) {
	if hostID == "" {
		return nil, nil, misc.Error(misc.ErrHostNotFound,
			fmt.Errorf("host id is required"))
	}
	host, err := hb.Find(ctx, hostID)
	if err != nil {
		return nil, nil, err
	}
	if host == nil {
		return nil, nil, misc.Error(misc.ErrHostNotFound,
			fmt.Errorf("host %q is no longer registered", hostID))
	}
	cli, cErr := d.Hosts.GetClient(host.ID, host.Endpoint)
	if cErr != nil {
		return nil, host, misc.Error(misc.ErrHostUnreachable,
			fmt.Errorf("Docker client for host %q (%s) could not be created: %v", host.ID, host.Endpoint, cErr))
	}
	return cli, host, nil
}

// wrapOpError turns a raw daemon error bubbling up from a Docker engine
// call into a coded error the API layer can emit as a structured
// 200/info response. The classification:
//
//  1. nil err → nil (pass-through).
//  2. NotFound-class (docker.IsErrNotFound) → notFoundCode.
//  3. connectivity-class (see isConnectivityError) → ErrHostUnreachable.
//  4. everything else → opFailedCode.
//
// The notFoundCode + opFailedCode pair lets the helper stay
// resource-agnostic: stacks pass {ErrStackNotFound,
// ErrStackOperationFailed}, containers pass {ErrContainerNotFound,
// ErrContainerOperationFailed}, etc. The frontend doesn't branch on the
// specific code yet (it only displays `info`) but the codes exist so
// future UI work can key per-resource messages without breaking the
// contract.
//
// `host` may be nil — in that case the message degrades to "unknown host"
// rather than omitting the reference. resourceKind is a short label
// ("stack", "container", "image", ...) used only for the info string.
func wrapOpError(op, resourceKind, resourceName string, host *dao.Host, err error, notFoundCode, opFailedCode int32) error {
	if err == nil {
		return nil
	}
	hostRef := "unknown host"
	if host != nil {
		hostRef = fmt.Sprintf("host %q (%s)", host.ID, host.Endpoint)
	}
	label := strings.TrimSpace(resourceKind)
	if label == "" {
		label = "resource"
	}
	target := label
	if resourceName != "" {
		target = fmt.Sprintf("%s %q", label, resourceName)
	}
	if docker.IsErrNotFound(err) {
		return misc.Error(notFoundCode,
			fmt.Errorf("%s not found on %s (%s): %v", target, hostRef, op, err))
	}
	if isConnectivityError(err) {
		return misc.Error(misc.ErrHostUnreachable,
			fmt.Errorf("cannot reach %s while trying to %s %s: %v", hostRef, op, target, err))
	}
	return misc.Error(opFailedCode,
		fmt.Errorf("%s: %s failed on %s: %v", target, op, hostRef, err))
}

// isConnectivityError is a best-effort classifier of Docker SDK errors
// that indicate the daemon itself is unreachable rather than rejecting a
// specific operation. Kept as a substring match on the stringified error
// to stay resilient across SDK versions (errdefs doesn't cover every
// transport-layer failure).
func isConnectivityError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, needle := range []string{
		"connection refused",
		"no such host",
		"no such file or directory",
		"network is unreachable",
		"i/o timeout",
		"connect: timed out",
		"tls handshake",
		"cannot connect to the docker daemon",
	} {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}
