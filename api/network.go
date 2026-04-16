package api

import (
	"github.com/cuigh/auxo/data"
	"github.com/cuigh/auxo/net/web"
	"github.com/cuigh/swirl/biz"
	"github.com/cuigh/swirl/misc"
)

// NetworkHandler encapsulates network related handlers.
type NetworkHandler struct {
	Search     web.HandlerFunc `path:"/search" auth:"network.view" desc:"search networks"`
	Find       web.HandlerFunc `path:"/find" auth:"network.view" desc:"find network by name"`
	Delete     web.HandlerFunc `path:"/delete" method:"post" auth:"network.delete" desc:"delete network"`
	Save       web.HandlerFunc `path:"/save" method:"post" auth:"network.edit" desc:"create or update network"`
	Disconnect web.HandlerFunc `path:"/disconnect" method:"post" auth:"network.disconnect" desc:"disconnect container from network"`
	Topology   web.HandlerFunc `path:"/topology" auth:"network.view" desc:"network topology graph"`
}

// NewNetwork creates an instance of NetworkHandler
func NewNetwork(nb biz.NetworkBiz) *NetworkHandler {
	return &NetworkHandler{
		Search:     networkSearch(nb),
		Find:       networkFind(nb),
		Delete:     networkDelete(nb),
		Save:       networkSave(nb),
		Disconnect: networkDisconnect(nb),
		Topology:   networkTopology(nb),
	}
}

func networkSearch(nb biz.NetworkBiz) web.HandlerFunc {
	return func(c web.Context) error {
		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()

		node := c.Query("node")
		networks, err := nb.Search(ctx, node)
		if err != nil {
			return err
		}
		return success(c, networks)
	}
}

func networkFind(nb biz.NetworkBiz) web.HandlerFunc {
	return func(c web.Context) error {
		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()

		node := c.Query("node")
		name := c.Query("name")
		network, raw, err := nb.Find(ctx, node, name)
		if err != nil {
			return err
		}
		return success(c, data.Map{"network": network, "raw": raw})
	}
}

func networkDelete(nb biz.NetworkBiz) web.HandlerFunc {
	type Args struct {
		Node string `json:"node"`
		ID   string `json:"id"`
		Name string `json:"name"`
	}

	return func(c web.Context) (err error) {
		args := &Args{}
		if err = c.Bind(args); err == nil {
			ctx, cancel := misc.Context(defaultTimeout)
			defer cancel()

			err = nb.Delete(ctx, args.Node, args.ID, args.Name, c.User())
		}
		return ajax(c, err)
	}
}

func networkSave(nb biz.NetworkBiz) web.HandlerFunc {
	type Args struct {
		Node string `json:"node"`
		biz.Network
	}
	return func(c web.Context) error {
		a := &Args{}
		err := c.Bind(a, true)
		if err == nil {
			ctx, cancel := misc.Context(defaultTimeout)
			defer cancel()

			err = nb.Create(ctx, a.Node, &a.Network, c.User())
		}
		return ajax(c, err)
	}
}

func networkTopology(nb biz.NetworkBiz) web.HandlerFunc {
	return func(c web.Context) error {
		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()

		hostID := c.Query("hostId")
		if hostID == "" {
			// Fallback to the legacy `node` param used elsewhere, to keep the
			// handler drop-in friendly.
			hostID = c.Query("node")
		}
		all := c.Query("all") == "true" || c.Query("all") == "1"
		topo, err := nb.Topology(ctx, hostID, all)
		if err != nil {
			return err
		}
		return success(c, topo)
	}
}

func networkDisconnect(nb biz.NetworkBiz) web.HandlerFunc {
	type Args struct {
		NetworkID   string `json:"networkId"`
		NetworkName string `json:"networkName"`
		Container   string `json:"container"`
	}

	return func(c web.Context) error {
		args := &Args{}
		err := c.Bind(args, true)
		if err == nil {
			ctx, cancel := misc.Context(defaultTimeout)
			defer cancel()

			err = nb.Disconnect(ctx, args.NetworkID, args.NetworkName, args.Container, c.User())
		}
		return ajax(c, err)
	}
}
