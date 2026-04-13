package docker

import (
	"context"
	"sort"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
)

// NetworkCreate create a network.
func (d *Docker) NetworkCreate(ctx context.Context, name string, options *network.CreateOptions) error {
	return d.call(func(client *client.Client) error {
		resp, err := client.NetworkCreate(ctx, name, *options)
		if err == nil && resp.Warning != "" {
			d.logger.Warnf("network '%s' was created but got warning: %s", name, resp.Warning)
		}
		return err
	})
}

// NetworkList return all networks.
func (d *Docker) NetworkList(ctx context.Context) (networks []network.Inspect, err error) {
	err = d.call(func(c *client.Client) (err error) {
		networks, err = c.NetworkList(ctx, network.ListOptions{})
		if err == nil {
			sort.Slice(networks, func(i, j int) bool {
				return networks[i].Name < networks[j].Name
			})
		}
		return
	})
	return
}

// NetworkCount return number of networks.
func (d *Docker) NetworkCount(ctx context.Context) (count int, err error) {
	err = d.call(func(c *client.Client) (err error) {
		var networks []network.Inspect
		networks, err = c.NetworkList(ctx, network.ListOptions{})
		if err == nil {
			count = len(networks)
		}
		return
	})
	return
}

// NetworkRemove remove a network.
func (d *Docker) NetworkRemove(ctx context.Context, name string) error {
	return d.call(func(c *client.Client) (err error) {
		return c.NetworkRemove(ctx, name)
	})
}

// NetworkDisconnect Disconnect a container from a network.
func (d *Docker) NetworkDisconnect(ctx context.Context, net, ctr string) error {
	return d.call(func(c *client.Client) (err error) {
		return c.NetworkDisconnect(ctx, net, ctr, false)
	})
}

// NetworkInspect return network information.
func (d *Docker) NetworkInspect(ctx context.Context, name string) (net network.Inspect, raw []byte, err error) {
	var c *client.Client
	if c, err = d.client(); err == nil {
		net, raw, err = c.NetworkInspectWithRaw(ctx, name, network.InspectOptions{})
	}
	return
}

// NetworkNames return network names by id list.
func (d *Docker) NetworkNames(ctx context.Context, ids ...string) (names map[string]string, err error) {
	var (
		c   *client.Client
		net network.Inspect
		lookup = func(id string) (n network.Inspect, e error) {
			if c == nil {
				if c, e = d.client(); e != nil {
					return
				}
			}
			n, e = c.NetworkInspect(ctx, id, network.InspectOptions{})
			return
		}
	)

	names = make(map[string]string)
	for _, id := range ids {
		name, ok := d.networks.Load(id)
		if ok {
			names[id] = name.(string)
		} else {
			net, err = lookup(id)
			if err != nil {
				return nil, err
			}
			names[id] = net.Name
			d.networks.Store(id, net.Name)
		}
	}
	return
}
