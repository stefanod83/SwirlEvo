package biz

import (
	"context"
	"strings"

	"github.com/cuigh/auxo/data"
	"github.com/cuigh/auxo/net/web"
	"github.com/cuigh/swirl/docker"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
)

type NetworkBiz interface {
	Search(ctx context.Context, node string) ([]*Network, error)
	Find(ctx context.Context, node, name string) (network *Network, raw string, err error)
	Delete(ctx context.Context, node, id, name string, user web.User) (err error)
	Create(ctx context.Context, node string, n *Network, user web.User) (err error)
	Disconnect(ctx context.Context, networkId, networkName, container string, user web.User) (err error)
	Topology(ctx context.Context, node string) (*NetworkTopology, error)
}

func NewNetwork(d *docker.Docker, eb EventBiz) NetworkBiz {
	return &networkBiz{d: d, eb: eb}
}

type networkBiz struct {
	d  *docker.Docker
	eb EventBiz
}

func (b *networkBiz) Create(ctx context.Context, node string, n *Network, user web.User) (err error) {
	nc := &network.CreateOptions{
		Driver:     n.Driver,
		Scope:      n.Scope,
		Internal:   n.Internal,
		Attachable: n.Attachable,
		Ingress:    n.Ingress,
		EnableIPv6: &n.IPv6,
		IPAM:       &network.IPAM{},
		Options:    toMap(n.Options),
		Labels:     toMap(n.Labels),
		//ConfigOnly     bool
		//ConfigFrom     *network.ConfigReference
	}
	for _, c := range n.IPAM.Config {
		nc.IPAM.Config = append(nc.IPAM.Config, network.IPAMConfig{
			Subnet:  c.Subnet,
			Gateway: c.Gateway,
			IPRange: c.Range,
		})
	}
	err = b.d.NetworkCreateOnNode(ctx, node, n.Name, nc)
	if err == nil {
		b.eb.CreateNetwork(EventActionCreate, n.Name, n.Name, user)
	}
	return
}

func (b *networkBiz) Find(ctx context.Context, node, name string) (nw *Network, raw string, err error) {
	var (
		nr network.Inspect
		r  []byte
	)
	nr, r, err = b.d.NetworkInspectOnNode(ctx, node, name)
	if err == nil {
		nw = newNetwork(&nr)
		raw, err = indentJSON(r)
	}
	return
}

func (b *networkBiz) Search(ctx context.Context, node string) ([]*Network, error) {
	list, err := b.d.NetworkListOnNode(ctx, node)
	if err != nil {
		return nil, err
	}

	networks := make([]*Network, len(list))
	for i, nr := range list {
		networks[i] = newNetwork(&nr)
	}
	return networks, nil
}

func (b *networkBiz) Delete(ctx context.Context, node, id, name string, user web.User) (err error) {
	err = b.d.NetworkRemoveOnNode(ctx, node, name)
	if err == nil {
		b.eb.CreateNetwork(EventActionDelete, id, name, user)
	}
	return
}

func (b *networkBiz) Disconnect(ctx context.Context, networkId, networkName, container string, user web.User) (err error) {
	err = b.d.NetworkDisconnect(ctx, networkName, container)
	if err == nil {
		b.eb.CreateNetwork(EventActionDisconnect, networkId, networkName, user)
	}
	return
}

func (b *networkBiz) Topology(ctx context.Context, node string) (*NetworkTopology, error) {
	nets, err := b.d.NetworkListOnNode(ctx, node)
	if err != nil {
		return nil, err
	}
	containers, _ := b.d.ContainerListAll(ctx, node)

	// Index containers by short ID (first 12 chars) and full ID so we can look them
	// up from network.Inspect.Containers which keys on the full container ID.
	ctrByID := make(map[string]*networkTopologyContainerData, len(containers)*2)
	for i := range containers {
		c := &containers[i]
		data := buildTopologyContainer(c)
		ctrByID[c.ID] = data
		if len(c.ID) >= 12 {
			ctrByID[c.ID[:12]] = data
		}
	}

	topo := &NetworkTopology{
		HostID: node,
		Nodes:  make([]NetworkTopologyNode, 0, 1+len(nets)+len(containers)),
		Edges:  make([]NetworkTopologyEdge, 0),
	}

	// 1. Host node — anchor of the graph.
	hostLabel := node
	if hostLabel == "" {
		hostLabel = "host"
	}
	hostNodeID := "host:" + node
	topo.Nodes = append(topo.Nodes, NetworkTopologyNode{
		ID:    hostNodeID,
		Type:  "host",
		Label: hostLabel,
		Meta: map[string]any{
			"containerCount": len(containers),
			"networkCount":   len(nets),
		},
	})

	// 2. Walk networks, inspect each to fetch attached containers.
	attachedContainerIDs := make(map[string]bool)
	for _, nr := range nets {
		full, _, ierr := b.d.NetworkInspectOnNode(ctx, node, nr.Name)
		if ierr != nil {
			// Skip networks we cannot inspect but still render a stub node so the
			// operator sees they exist.
			full = nr
		}
		nodeID := "net:" + full.ID
		flags := []string{}
		if full.Internal {
			flags = append(flags, "isolated")
		}
		if full.Ingress {
			flags = append(flags, "ingress")
		}
		meta := map[string]any{
			"driver":     full.Driver,
			"scope":      full.Scope,
			"internal":   full.Internal,
			"ingress":    full.Ingress,
			"attachable": full.Attachable,
			"ipv6":       full.EnableIPv6,
		}
		if len(full.IPAM.Config) > 0 {
			ipams := make([]map[string]string, 0, len(full.IPAM.Config))
			for _, c := range full.IPAM.Config {
				ipams = append(ipams, map[string]string{
					"subnet":  c.Subnet,
					"gateway": c.Gateway,
					"range":   c.IPRange,
				})
			}
			meta["ipam"] = ipams
		}
		topo.Nodes = append(topo.Nodes, NetworkTopologyNode{
			ID:    nodeID,
			Type:  "network",
			Label: full.Name,
			Meta:  meta,
			Flags: flags,
		})
		topo.Edges = append(topo.Edges, NetworkTopologyEdge{
			Source: hostNodeID,
			Target: nodeID,
			Type:   "host-network",
		})

		// Fan out to attached containers.
		for cid, ep := range full.Containers {
			ctrID := "ct:" + cid
			attachedContainerIDs[cid] = true
			if _, exists := findNode(topo.Nodes, ctrID); !exists {
				data := ctrByID[cid]
				label := ep.Name
				if label == "" && data != nil {
					label = data.name
				}
				meta := map[string]any{
					"name": label,
				}
				flags := []string{}
				if data != nil {
					meta["image"] = data.image
					meta["state"] = data.state
					meta["status"] = data.status
					if len(data.ports) > 0 {
						meta["ports"] = data.ports
					}
					if data.exposedPublic {
						flags = append(flags, "exposed-public")
					} else if data.localOnly {
						flags = append(flags, "local-only")
					}
				}
				topo.Nodes = append(topo.Nodes, NetworkTopologyNode{
					ID:    ctrID,
					Type:  "container",
					Label: label,
					Meta:  meta,
					Flags: flags,
				})
			}

			// Edge carries IPv4 (fallback IPv6) as label.
			ipLabel := ep.IPv4Address
			if ipLabel == "" {
				ipLabel = ep.IPv6Address
			}
			topo.Edges = append(topo.Edges, NetworkTopologyEdge{
				Source: nodeID,
				Target: ctrID,
				Type:   "network-container",
				Label:  ipLabel,
			})
		}
	}

	// 3. Any container not attached to any known network (NetworkMode=host, none,
	// or container:xxx) is still worth showing — we link it directly to the host.
	for i := range containers {
		c := &containers[i]
		if attachedContainerIDs[c.ID] {
			continue
		}
		data := ctrByID[c.ID]
		ctrID := "ct:" + c.ID
		meta := map[string]any{
			"name":        data.name,
			"image":       data.image,
			"state":       data.state,
			"status":      data.status,
			"networkMode": c.HostConfig.NetworkMode,
		}
		if len(data.ports) > 0 {
			meta["ports"] = data.ports
		}
		flags := []string{}
		if data.exposedPublic {
			flags = append(flags, "exposed-public")
		} else if data.localOnly {
			flags = append(flags, "local-only")
		}
		topo.Nodes = append(topo.Nodes, NetworkTopologyNode{
			ID:    ctrID,
			Type:  "container",
			Label: data.name,
			Meta:  meta,
			Flags: flags,
		})
		topo.Edges = append(topo.Edges, NetworkTopologyEdge{
			Source: hostNodeID,
			Target: ctrID,
			Type:   "host-container",
			Label:  c.HostConfig.NetworkMode,
		})
	}

	return topo, nil
}

// networkTopologyContainerData is the minimal subset of container.Summary the
// topology aggregator needs to enrich nodes. Not exported.
type networkTopologyContainerData struct {
	name          string
	image         string
	state         string
	status        string
	ports         []map[string]any
	exposedPublic bool
	localOnly     bool
}

func buildTopologyContainer(c *container.Summary) *networkTopologyContainerData {
	name := ""
	if len(c.Names) > 0 {
		name = strings.TrimPrefix(c.Names[0], "/")
	}
	data := &networkTopologyContainerData{
		name:   name,
		image:  c.Image,
		state:  c.State,
		status: c.Status,
	}
	// Analyse port bindings.
	hasPublished := false
	allLoopback := true
	for _, p := range c.Ports {
		if p.PublicPort == 0 {
			continue
		}
		hasPublished = true
		entry := map[string]any{
			"ip":          p.IP,
			"privatePort": p.PrivatePort,
			"publicPort":  p.PublicPort,
			"type":        p.Type,
		}
		data.ports = append(data.ports, entry)
		switch p.IP {
		case "127.0.0.1", "::1":
			// loopback-only, do nothing to allLoopback
		default:
			// any other binding (0.0.0.0, empty, specific public IP) breaks loopback-only.
			allLoopback = false
		}
		switch p.IP {
		case "0.0.0.0", "", "::":
			data.exposedPublic = true
		}
	}
	if hasPublished && allLoopback {
		data.localOnly = true
	}
	return data
}

func findNode(nodes []NetworkTopologyNode, id string) (NetworkTopologyNode, bool) {
	for _, n := range nodes {
		if n.ID == id {
			return n, true
		}
	}
	return NetworkTopologyNode{}, false
}

type Network struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Created    string `json:"created"`
	Driver     string `json:"driver"`
	Scope      string `json:"scope"`
	Internal   bool   `json:"internal"`
	Attachable bool   `json:"attachable"`
	Ingress    bool   `json:"ingress"`
	IPv6       bool   `json:"ipv6"`
	IPAM       struct {
		Driver  string        `json:"driver"`
		Options data.Options  `json:"options"`
		Config  []*IPAMConfig `json:"config"`
	} `json:"ipam"`
	Options    data.Options        `json:"options"`
	Labels     data.Options        `json:"labels"`
	Containers []*NetworkContainer `json:"containers"`
}

type IPAMConfig struct {
	Subnet  string `json:"subnet,omitempty"`
	Gateway string `json:"gateway,omitempty"`
	Range   string `json:"range,omitempty"`
}

type NetworkContainer struct {
	ID   string `json:"id"`   // container id
	Name string `json:"name"` // container name
	Mac  string `json:"mac"`  // mac address
	IPv4 string `json:"ipv4"` // IPv4 address
	IPv6 string `json:"ipv6"` // IPv6 address
}

func newNetwork(nr *network.Inspect) *Network {
	n := &Network{
		ID:         nr.ID,
		Name:       nr.Name,
		Created:    formatTime(nr.Created),
		Driver:     nr.Driver,
		Scope:      nr.Scope,
		Internal:   nr.Internal,
		Attachable: nr.Attachable,
		Ingress:    nr.Ingress,
		IPv6:       nr.EnableIPv6,
		Options:    mapToOptions(nr.Options),
		Labels:     mapToOptions(nr.Labels),
	}
	n.IPAM.Driver = nr.IPAM.Driver
	n.IPAM.Options = mapToOptions(nr.IPAM.Options)
	n.IPAM.Config = make([]*IPAMConfig, len(nr.IPAM.Config))
	for i, c := range nr.IPAM.Config {
		n.IPAM.Config[i] = &IPAMConfig{
			Subnet:  c.Subnet,
			Gateway: c.Gateway,
			Range:   c.IPRange,
		}
	}
	n.Containers = make([]*NetworkContainer, 0, len(nr.Containers))
	for id, ep := range nr.Containers {
		n.Containers = append(n.Containers, &NetworkContainer{
			ID:   id,
			Name: ep.Name,
			Mac:  ep.MacAddress,
			IPv4: ep.IPv4Address,
			IPv6: ep.IPv6Address,
		})
	}
	return n
}

// NetworkTopology is an aggregate view of a host's networks, containers and
// their connectivity — consumed by the Topology tab in the Standalone UI.
type NetworkTopology struct {
	HostID string                `json:"hostId"`
	Nodes  []NetworkTopologyNode `json:"nodes"`
	Edges  []NetworkTopologyEdge `json:"edges"`
}

// NetworkTopologyNode is a generic graph node (host / network / container).
// IDs are namespaced ("host:", "net:", "ct:") to avoid collisions.
type NetworkTopologyNode struct {
	ID    string         `json:"id"`
	Type  string         `json:"type"`
	Label string         `json:"label"`
	Meta  map[string]any `json:"meta,omitempty"`
	Flags []string       `json:"flags,omitempty"`
}

// NetworkTopologyEdge links two nodes. Type carries the semantic.
type NetworkTopologyEdge struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Type   string `json:"type"`
	Label  string `json:"label,omitempty"`
}
