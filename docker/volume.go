package docker

import (
	"context"
	"sort"

	"github.com/cuigh/swirl/misc"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
)

// VolumeList return volumes on the host.
func (d *Docker) VolumeList(ctx context.Context, node, name string, pageIndex, pageSize int) (volumes []*volume.Volume, total int, err error) {
	var (
		c    *client.Client
		resp volume.ListResponse
	)

	c, err = d.agent(node)
	if err != nil {
		return
	}

	opts := volume.ListOptions{Filters: filters.NewArgs()}
	if name != "" {
		opts.Filters.Add("name", name)
	}
	resp, err = c.VolumeList(ctx, opts)
	if err != nil {
		return
	}
	sort.Slice(resp.Volumes, func(i, j int) bool {
		return resp.Volumes[i].Name < resp.Volumes[j].Name
	})

	total = len(resp.Volumes)
	start, end := misc.Page(total, pageIndex, pageSize)
	volumes = resp.Volumes[start:end]
	return
}

// VolumeCreate create a volume.
func (d *Docker) VolumeCreate(ctx context.Context, node string, options *volume.CreateOptions) (err error) {
	var c *client.Client
	if c, err = d.agent(node); err == nil {
		_, err = c.VolumeCreate(ctx, *options)
	}
	return
}

// VolumeRemove remove a volume.
func (d *Docker) VolumeRemove(ctx context.Context, node, name string) (err error) {
	c, err := d.agent(node)
	if err == nil {
		err = c.VolumeRemove(ctx, name, false)
	}
	return err
}

// VolumePrune remove all unused volumes.
func (d *Docker) VolumePrune(ctx context.Context, node string) (report volume.PruneReport, err error) {
	var c *client.Client
	if c, err = d.agent(node); err == nil {
		report, err = c.VolumesPrune(ctx, filters.NewArgs())
	}
	return
}

// VolumeInspect return volume raw information.
func (d *Docker) VolumeInspect(ctx context.Context, node, name string) (vol volume.Volume, raw []byte, err error) {
	var c *client.Client
	if c, err = d.agent(node); err == nil {
		vol, raw, err = c.VolumeInspectWithRaw(ctx, name)
	}
	return
}
