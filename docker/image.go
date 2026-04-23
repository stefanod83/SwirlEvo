package docker

import (
	"context"
	"io"

	"github.com/cuigh/swirl/misc"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
)

// ImageCount returns the number of images on the given host.
func (d *Docker) ImageCount(ctx context.Context, node string) (count int, err error) {
	c, err := d.agent(node)
	if err != nil {
		return 0, err
	}
	list, err := c.ImageList(ctx, image.ListOptions{All: false})
	if err != nil {
		return 0, err
	}
	return len(list), nil
}

// ImageList return images on the host.
func (d *Docker) ImageList(ctx context.Context, node, name string, pageIndex, pageSize int) (images []image.Summary, total int, err error) {
	c, err := d.agent(node)
	if err != nil {
		return nil, 0, err
	}

	opts := image.ListOptions{}
	if name != "" {
		opts.Filters = filters.NewArgs()
		opts.Filters.Add("reference", name)
	}
	images, err = c.ImageList(ctx, opts)
	if err != nil {
		return nil, 0, err
	}

	total = len(images)
	start, end := misc.Page(total, pageIndex, pageSize)
	images = images[start:end]
	return
}

// ImageInspect returns image information.
func (d *Docker) ImageInspect(ctx context.Context, node, id string) (img image.InspectResponse, raw []byte, err error) {
	var c *client.Client
	if c, err = d.agent(node); err == nil {
		return c.ImageInspectWithRaw(ctx, id)
	}
	return
}

// ImageHistory returns the changes in an image in history format.
func (d *Docker) ImageHistory(ctx context.Context, node, id string) (histories []image.HistoryResponseItem, err error) {
	var c *client.Client
	if c, err = d.agent(node); err == nil {
		return c.ImageHistory(ctx, id)
	}
	return
}

// ImageRemove remove a image.
func (d *Docker) ImageRemove(ctx context.Context, node, id string, force bool) error {
	c, err := d.agent(node)
	if err == nil {
		_, err = c.ImageRemove(ctx, id, image.RemoveOptions{Force: force, PruneChildren: true})
	}
	return err
}

// ImagePrune remove all unused images.
func (d *Docker) ImagePrune(ctx context.Context, node string) (report image.PruneReport, err error) {
	var c *client.Client
	if c, err = d.agent(node); err == nil {
		report, err = c.ImagesPrune(ctx, filters.NewArgs(filters.Arg("dangling", "false")))
	}
	return
}

// ImageTag adds an additional reference to an existing image. The target
// reference must be a full `host/repository[:tag]`; Docker's daemon does
// not validate that the target host is reachable — that's Push's job.
func (d *Docker) ImageTag(ctx context.Context, node, source, target string) error {
	c, err := d.agent(node)
	if err != nil {
		return err
	}
	return c.ImageTag(ctx, source, target)
}

// ImagePull downloads an image reference from its origin registry. Symmetric
// to ImagePush: `authBase64` carries the encoded `registry.AuthConfig`, or
// "" for anonymous pulls. The progress stream is drained to completion so
// layer-pull errors surface via the final NDJSON frame.
//
// Timeout-sensitive: multi-GB images can take minutes. Callers must pass
// a generous context deadline.
func (d *Docker) ImagePull(ctx context.Context, node, ref, authBase64 string) error {
	c, err := d.agent(node)
	if err != nil {
		return err
	}
	rc, err := c.ImagePull(ctx, ref, image.PullOptions{RegistryAuth: authBase64})
	if err != nil {
		return err
	}
	defer rc.Close()
	_, err = io.Copy(io.Discard, rc)
	return err
}

// ImagePush pushes an image reference to its remote registry. `authBase64`
// is the base64-URL-encoded JSON `registry.AuthConfig` supplied by the
// caller (typically via `dao.Registry.GetEncodedAuth()`). The returned
// progress stream is drained to completion — errors embedded in the
// stream are surfaced via the final io.ReadAll.
//
// Timeout-sensitive: large images can push for minutes. The caller must
// pass a ctx with a generous deadline (or no deadline).
func (d *Docker) ImagePush(ctx context.Context, node, ref, authBase64 string) error {
	c, err := d.agent(node)
	if err != nil {
		return err
	}
	rc, err := c.ImagePush(ctx, ref, image.PushOptions{RegistryAuth: authBase64})
	if err != nil {
		return err
	}
	defer rc.Close()
	// Drain. Docker emits NDJSON with error objects on push failures; we
	// surface the last one by reading fully. Bytes go to /dev/null — we
	// don't stream progress back yet (could be added as SSE later).
	_, err = io.Copy(io.Discard, rc)
	return err
}
