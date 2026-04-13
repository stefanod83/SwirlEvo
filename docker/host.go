package docker

import (
	"context"
	"sync"
	"time"

	"github.com/cuigh/auxo/log"
	"github.com/docker/docker/api/types/system"
	"github.com/docker/docker/client"
)

// HostManager manages Docker clients for standalone hosts.
type HostManager struct {
	clients sync.Map // hostID → *client.Client
	logger  log.Logger
}

// NewHostManager creates a new HostManager.
func NewHostManager() *HostManager {
	return &HostManager{
		logger: log.Get("host-manager"),
	}
}

// GetClient returns a Docker client for the specified host.
// If a client already exists for this host, it is reused.
func (hm *HostManager) GetClient(hostID, endpoint string) (*client.Client, error) {
	if v, ok := hm.clients.Load(hostID); ok {
		return v.(*client.Client), nil
	}

	c, err := client.NewClientWithOpts(
		client.WithHost(endpoint),
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return nil, err
	}

	hm.clients.Store(hostID, c)
	return c, nil
}

// RemoveClient removes and closes the client for a host.
func (hm *HostManager) RemoveClient(hostID string) {
	if v, ok := hm.clients.LoadAndDelete(hostID); ok {
		if c, ok := v.(*client.Client); ok {
			c.Close()
		}
	}
}

// TestConnection tests connectivity to a Docker endpoint and returns system info.
func (hm *HostManager) TestConnection(ctx context.Context, endpoint string) (system.Info, error) {
	c, err := client.NewClientWithOpts(
		client.WithHost(endpoint),
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return system.Info{}, err
	}
	defer c.Close()

	testCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	return c.Info(testCtx)
}

// RefreshClient replaces an existing client with a new one (e.g., after endpoint change).
func (hm *HostManager) RefreshClient(hostID, endpoint string) error {
	hm.RemoveClient(hostID)
	_, err := hm.GetClient(hostID, endpoint)
	return err
}
