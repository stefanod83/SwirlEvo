package deploy_agent

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	dockercontainer "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	dockerimage "github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"github.com/docker/docker/errdefs"
)

// newDockerClient builds a client that talks to the daemon via the
// bind-mounted /var/run/docker.sock. Mirrors the primary's client setup
// in docker/docker.go so behavior (API negotiation) stays consistent.
//
// The sidekick NEVER uses DOCKER_ENDPOINT / DOCKER_API_VERSION from
// the process env directly — that would couple it to the primary's
// Swirl env, which is confusing. Client.FromEnv is still honored so
// an integration test can point the sidekick at a DinD daemon via
// DOCKER_HOST.
func newDockerClient() (*client.Client, error) {
	cli, err := client.NewClientWithOpts(
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return nil, fmt.Errorf("deploy-agent: init docker client: %w", err)
	}
	return cli, nil
}

// pingDocker probes the daemon socket at startup so a broken mount
// fails fast with a clear message instead of during the first real
// call deep inside runDeploy.
func pingDocker(ctx context.Context, cli *client.Client) error {
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if _, err := cli.Ping(pingCtx); err != nil {
		return fmt.Errorf("deploy-agent: docker daemon unreachable: %w", err)
	}
	return nil
}

// stopContainer gracefully stops a container by ID or name. If the
// container is already stopped (or missing) the call is treated as a
// no-op — the caller just wants a stopped state, not a specific
// transition.
func stopContainer(ctx context.Context, cli *client.Client, id string, grace time.Duration) error {
	if id == "" {
		return nil
	}
	secs := int(grace.Seconds())
	if secs <= 0 {
		secs = 30
	}
	opts := dockercontainer.StopOptions{Timeout: &secs}
	err := cli.ContainerStop(ctx, id, opts)
	if err == nil {
		return nil
	}
	if errdefs.IsNotFound(err) {
		return nil // already gone — success
	}
	return fmt.Errorf("deploy-agent: stop container %q: %w", id, err)
}

// renameContainer wraps ContainerRename. Returns a clear error if the
// destination name is already in use (the common "rename to previous
// but a stale one exists" scenario).
func renameContainer(ctx context.Context, cli *client.Client, id, newName string) error {
	if id == "" || newName == "" {
		return fmt.Errorf("deploy-agent: renameContainer needs both id and newName")
	}
	if err := cli.ContainerRename(ctx, id, newName); err != nil {
		return fmt.Errorf("deploy-agent: rename container %q to %q: %w", id, newName, err)
	}
	return nil
}

// removeContainer forcibly removes a container. Volumes are preserved —
// self-deploy MUST NEVER drop the swirl_data volume (Phase 7 invariant).
// Absent container = success (idempotent).
func removeContainer(ctx context.Context, cli *client.Client, id string) error {
	if id == "" {
		return nil
	}
	opts := dockercontainer.RemoveOptions{Force: true, RemoveVolumes: false}
	err := cli.ContainerRemove(ctx, id, opts)
	if err == nil {
		return nil
	}
	if errdefs.IsNotFound(err) {
		return nil
	}
	return fmt.Errorf("deploy-agent: remove container %q: %w", id, err)
}

// startContainer starts a container by ID or name. Absent container
// is a hard error here — by the time we start we should have an ID.
func startContainer(ctx context.Context, cli *client.Client, id string) error {
	if id == "" {
		return fmt.Errorf("deploy-agent: startContainer needs an id")
	}
	if err := cli.ContainerStart(ctx, id, dockercontainer.StartOptions{}); err != nil {
		return fmt.Errorf("deploy-agent: start container %q: %w", id, err)
	}
	return nil
}

// inspectContainer returns the container's full inspect output. Wraps
// the SDK call with a dedicated error prefix so trace logs are easy
// to grep.
func inspectContainer(ctx context.Context, cli *client.Client, id string) (dockercontainer.InspectResponse, error) {
	out, err := cli.ContainerInspect(ctx, id)
	if err != nil {
		return out, fmt.Errorf("deploy-agent: inspect container %q: %w", id, err)
	}
	return out, nil
}

// containerExists reports whether the given id/name is present on the
// daemon. Used as a guard before rollback rename-back operations so we
// never surprise the operator by recreating the wrong container.
func containerExists(ctx context.Context, cli *client.Client, id string) (bool, error) {
	_, err := cli.ContainerInspect(ctx, id)
	if err == nil {
		return true, nil
	}
	if errdefs.IsNotFound(err) {
		return false, nil
	}
	return false, err
}

// findSwirlContainerIP locates the running Swirl container that belongs
// to the given compose project and returns its IP address on the first
// non-empty network it is attached to. Looks up by compose labels so
// the resolution works even when the container name uses the v1
// underscore pattern (`<project>_<service>_1`) or a custom
// `container_name:`.
//
// Returns ("", nil) when no swirl-flavoured container is found — the
// caller falls back to the legacy localhost probe.
func findSwirlContainerIP(ctx context.Context, cli *client.Client, projectName string) (string, error) {
	list, err := cli.ContainerList(ctx, dockercontainer.ListOptions{
		All: true,
		Filters: filters.NewArgs(
			filters.Arg("label", "com.docker.compose.project="+projectName),
		),
	})
	if err != nil {
		return "", err
	}
	var candidate *dockercontainer.Summary
	for i, c := range list {
		svc := c.Labels["com.docker.compose.service"]
		if strings.Contains(strings.ToLower(svc), "swirl") {
			candidate = &list[i]
			break
		}
	}
	if candidate == nil {
		return "", nil
	}
	info, err := cli.ContainerInspect(ctx, candidate.ID)
	if err != nil {
		return "", err
	}
	if info.NetworkSettings != nil {
		for _, n := range info.NetworkSettings.Networks {
			if n != nil && n.IPAddress != "" {
				return n.IPAddress, nil
			}
		}
	}
	return "", nil
}

// pullImageRaw is the daemon-side pull. Drains the NDJSON stream to
// surface embedded errors (the Docker daemon returns HTTP 200 and
// reports errors in the stream body).
func pullImageRaw(ctx context.Context, cli *client.Client, ref string) error {
	if ref == "" {
		return fmt.Errorf("deploy-agent: pullImage needs a reference")
	}
	rc, err := cli.ImagePull(ctx, ref, dockerimage.PullOptions{})
	if err != nil {
		return fmt.Errorf("deploy-agent: pull image %q: %w", ref, err)
	}
	defer rc.Close()
	// Drain — errors in the stream come through as non-io.EOF errors.
	if _, err := io.Copy(io.Discard, rc); err != nil {
		return fmt.Errorf("deploy-agent: drain image pull stream for %q: %w", ref, err)
	}
	return nil
}
