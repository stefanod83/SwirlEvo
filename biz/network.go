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
	Topology(ctx context.Context, node string, all bool) (*NetworkTopology, error)
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
		b.eb.CreateNetwork(EventActionCreate, node, n.Name, n.Name, user)
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

	// Scan all containers once to build the set of networks actually in use.
	// Docker's List response populates c.NetworkSettings.Networks even for
	// stopped containers, so any configured attachment counts as "used".
	usedByID := make(map[string]bool)
	usedByName := make(map[string]bool)
	if containers, cErr := b.d.ContainerListAll(ctx, node); cErr == nil {
		for _, c := range containers {
			if c.NetworkSettings == nil {
				continue
			}
			for name, ep := range c.NetworkSettings.Networks {
				if name != "" {
					usedByName[name] = true
				}
				if ep != nil && ep.NetworkID != "" {
					usedByID[ep.NetworkID] = true
				}
			}
		}
	}

	networks := make([]*Network, len(list))
	for i, nr := range list {
		n := newNetwork(&nr)
		n.Unused = !usedByID[n.ID] && !usedByName[n.Name] && !isSystemNetwork(n)
		networks[i] = n
	}
	return networks, nil
}

// isSystemNetwork returns true for Docker's built-in networks which are
// always present regardless of usage. They should never be flagged as unused.
func isSystemNetwork(n *Network) bool {
	switch n.Name {
	case "bridge", "host", "none", "docker_gwbridge", "ingress":
		return true
	}
	return false
}

func (b *networkBiz) Delete(ctx context.Context, node, id, name string, user web.User) (err error) {
	err = b.d.NetworkRemoveOnNode(ctx, node, name)
	if err == nil {
		b.eb.CreateNetwork(EventActionDelete, node, id, name, user)
	}
	return
}

func (b *networkBiz) Disconnect(ctx context.Context, networkId, networkName, container string, user web.User) (err error) {
	err = b.d.NetworkDisconnect(ctx, networkName, container)
	if err == nil {
		b.eb.CreateNetwork(EventActionDisconnect, "", networkId, networkName, user)
	}
	return
}

// Topology builds a host-networks-containers graph. When `all` is false
// (default) only running containers are included; set it to true to also show
// stopped/exited ones — they get an "inactive" flag so the UI can dim them.
//
// The traversal uses container.Summary.NetworkSettings.Networks (populated on
// List responses) as the primary source of container→network edges, so we
// don't need a per-network Inspect round-trip; the only Inspect call is to
// pull IPAM config for the network-node tooltip. Unused networks (no
// attachments, non-system) get an "unused" flag.
func (b *networkBiz) Topology(ctx context.Context, node string, all bool) (*NetworkTopology, error) {
	nets, err := b.d.NetworkListOnNode(ctx, node)
	if err != nil {
		return nil, err
	}
	containers, _ := b.d.ContainerListAll(ctx, node)

	topo := &NetworkTopology{
		HostID: node,
		Nodes:  make([]NetworkTopologyNode, 0, 1+len(nets)+len(containers)),
		Edges:  make([]NetworkTopologyEdge, 0),
	}

	// 1. Host node.
	hostLabel := node
	if hostLabel == "" {
		hostLabel = "host"
	}
	hostNodeID := "host:" + node

	// 2. Network nodes + lookup maps (by ID and name).
	netNodeByID := make(map[string]string)
	netNodeByName := make(map[string]string)
	netNameByNodeID := make(map[string]string) // node-id → network name (for container-side lookup)
	netUsage := make(map[string]int)
	netStackByNodeID := make(map[string]string) // node-id → compose project (may be empty)
	directExposedNets := make(map[string]bool)
	for i := range nets {
		nr := nets[i]
		flags := []string{}
		if nr.Internal {
			flags = append(flags, "isolated")
		}
		if nr.Ingress {
			flags = append(flags, "ingress")
		}
		switch nr.Driver {
		case "macvlan", "ipvlan", "host":
			flags = append(flags, "exposed-direct")
			directExposedNets[nr.ID] = true
		}
		stack := nr.Labels["com.docker.compose.project"]
		meta := map[string]any{
			"driver":     nr.Driver,
			"scope":      nr.Scope,
			"internal":   nr.Internal,
			"ingress":    nr.Ingress,
			"attachable": nr.Attachable,
			"ipv6":       nr.EnableIPv6,
		}
		if stack != "" {
			meta["stack"] = stack
		}
		if len(nr.IPAM.Config) > 0 {
			ipams := make([]map[string]string, 0, len(nr.IPAM.Config))
			for _, c := range nr.IPAM.Config {
				ipams = append(ipams, map[string]string{
					"subnet":  c.Subnet,
					"gateway": c.Gateway,
					"range":   c.IPRange,
				})
			}
			meta["ipam"] = ipams
		}
		nodeID := "net:" + nr.ID
		netNodeByID[nr.ID] = nodeID
		netNodeByName[nr.Name] = nodeID
		netNameByNodeID[nodeID] = nr.Name
		netStackByNodeID[nodeID] = stack
		topo.Nodes = append(topo.Nodes, NetworkTopologyNode{
			ID:    nodeID,
			Type:  "network",
			Label: nr.Name,
			Meta:  meta,
			Flags: flags,
		})
	}

	// 3. Container nodes + edges. Source of truth is NetworkSettings.Networks
	// from ContainerList (includes stopped containers' configured networks).
	seenContainers := make(map[string]bool)
	seenEdges := make(map[string]bool) // "src|dst" pairs — prevents duplicate edges
	totalRunning := 0
	totalIncluded := 0
	for i := range containers {
		c := &containers[i]
		isRunning := c.State == "running"
		if isRunning {
			totalRunning++
		}
		if !all && !isRunning {
			continue
		}
		totalIncluded++

		data := buildTopologyContainer(c)
		ctrStack := c.Labels["com.docker.compose.project"]

		// Resolve every network attachment up-front so we can both compute the
		// exposure flags and populate the container's per-network IP list for
		// the details panel.
		type attachment struct {
			netNodeID   string
			netName     string
			ip, ipv6    string
			mac         string
		}
		attachments := []attachment{}
		if c.HostConfig.NetworkMode == "host" {
			data.exposedPublic = true
			data.localOnly = false
		}
		if c.NetworkSettings != nil {
			for netName, ep := range c.NetworkSettings.Networks {
				if ep == nil {
					continue
				}
				var netNodeID string
				if ep.NetworkID != "" {
					netNodeID = netNodeByID[ep.NetworkID]
				}
				if netNodeID == "" {
					netNodeID = netNodeByName[netName]
				}
				if netNodeID == "" {
					continue
				}
				if directExposedNets[ep.NetworkID] {
					data.exposedPublic = true
					data.localOnly = false
				}
				resolvedName := netNameByNodeID[netNodeID]
				if resolvedName == "" {
					resolvedName = netName
				}
				attachments = append(attachments, attachment{
					netNodeID: netNodeID,
					netName:   resolvedName,
					ip:        ep.IPAddress,
					ipv6:      ep.GlobalIPv6Address,
					mac:       ep.MacAddress,
				})
			}
		}

		ctrID := "ct:" + c.ID
		if !seenContainers[c.ID] {
			flags := []string{}
			if data.exposedPublic {
				flags = append(flags, "exposed-public")
			} else if data.localOnly {
				flags = append(flags, "local-only")
			}
			if !isRunning {
				flags = append(flags, "inactive")
			}
			meta := map[string]any{
				"name":        data.name,
				"image":       data.image,
				"state":       data.state,
				"status":      data.status,
				"networkMode": c.HostConfig.NetworkMode,
			}
			if ctrStack != "" {
				meta["stack"] = ctrStack
			}
			if len(data.ports) > 0 {
				meta["ports"] = data.ports
			}
			if len(attachments) > 0 {
				nets := make([]map[string]any, 0, len(attachments))
				for _, a := range attachments {
					nets = append(nets, map[string]any{
						"name": a.netName,
						"ip":   a.ip,
						"ipv6": a.ipv6,
						"mac":  a.mac,
					})
				}
				meta["networks"] = nets
			}
			topo.Nodes = append(topo.Nodes, NetworkTopologyNode{
				ID:    ctrID,
				Type:  "container",
				Label: data.name,
				Meta:  meta,
				Flags: flags,
			})
			seenContainers[c.ID] = true
		}

		for _, a := range attachments {
			edgeKey := a.netNodeID + "|" + ctrID
			if seenEdges[edgeKey] {
				continue
			}
			seenEdges[edgeKey] = true
			netUsage[a.netNodeID]++
			// No label on network-container edges anymore — IPs moved to the
			// container's details panel.
			topo.Edges = append(topo.Edges, NetworkTopologyEdge{
				Source: a.netNodeID,
				Target: ctrID,
				Type:   "network-container",
			})
		}
		if len(attachments) == 0 {
			edgeKey := hostNodeID + "|" + ctrID
			if !seenEdges[edgeKey] {
				seenEdges[edgeKey] = true
				topo.Edges = append(topo.Edges, NetworkTopologyEdge{
					Source: hostNodeID,
					Target: ctrID,
					Type:   "host-container",
					Label:  c.HostConfig.NetworkMode,
				})
			}
		}
	}

	// 4. Host node goes first in the slice (prepend after we know the counters).
	hostNode := NetworkTopologyNode{
		ID:    hostNodeID,
		Type:  "host",
		Label: hostLabel,
		Meta: map[string]any{
			"networkCount":   len(nets),
			"runningCount":   totalRunning,
			"totalCount":     len(containers),
			"includedCount":  totalIncluded,
			"showInactive":   all,
		},
	}
	topo.Nodes = append([]NetworkTopologyNode{hostNode}, topo.Nodes...)

	// 5. Mark unused networks + add host→network edges (skipping isolated
	// networks — by definition they have no route to the host).
	for i, nd := range topo.Nodes {
		if nd.Type != "network" {
			continue
		}
		// Detect system defaults by name (same rule used in Search).
		systemByName := false
		switch nd.Label {
		case "bridge", "host", "none", "docker_gwbridge", "ingress":
			systemByName = true
		}
		if netUsage[nd.ID] == 0 && !systemByName {
			nd.Flags = append(nd.Flags, "unused")
			topo.Nodes[i] = nd
		}
		isIsolated := false
		for _, f := range nd.Flags {
			if f == "isolated" {
				isIsolated = true
				break
			}
		}
		if isIsolated {
			continue
		}
		// Label the host→network edge with the compose project (if any) so
		// the stack membership is visible without polluting the network node
		// label itself.
		topo.Edges = append(topo.Edges, NetworkTopologyEdge{
			Source: hostNodeID,
			Target: nd.ID,
			Type:   "host-network",
			Label:  netStackByNodeID[nd.ID],
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

// isLoopback reports whether a bind IP is part of 127.0.0.0/8 (IPv4) or ::1 (IPv6).
// Empty string and 0.0.0.0 / :: are NOT loopback — they bind to every interface.
func isLoopback(ip string) bool {
	if ip == "" {
		return false
	}
	if ip == "::1" {
		return true
	}
	// IPv4 loopback = 127.0.0.0/8 → any address starting with "127."
	return strings.HasPrefix(ip, "127.")
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
	// Analyse port bindings. A container is "exposed to the outside" when at
	// least one published port is bound to a non-loopback IP (0.0.0.0, "" =
	// all interfaces, or a specific public address). Loopback-only bindings
	// (127.0.0.0/8 or ::1) are tagged "local-only" — safe from external reach.
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
		if !isLoopback(p.IP) {
			allLoopback = false
			data.exposedPublic = true
		}
	}
	if hasPublished && allLoopback {
		data.localOnly = true
		data.exposedPublic = false
	}
	return data
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
	Unused     bool                `json:"unused"`
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
