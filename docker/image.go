package docker

import (
	"context"

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
