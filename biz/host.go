package biz

import (
	"context"
	"time"

	"github.com/cuigh/auxo/net/web"
	"github.com/cuigh/swirl/dao"
	"github.com/cuigh/swirl/docker"
)

type HostBiz interface {
	Search(ctx context.Context, name, status string, pageIndex, pageSize int) ([]*dao.Host, int, error)
	Find(ctx context.Context, id string) (*dao.Host, error)
	Create(ctx context.Context, host *dao.Host, user web.User) error
	Update(ctx context.Context, host *dao.Host, user web.User) error
	Delete(ctx context.Context, id, name string, user web.User) error
	Test(ctx context.Context, endpoint string) (*HostInfo, error)
	Sync(ctx context.Context, id string) error
	GetAll(ctx context.Context) ([]*dao.Host, error)
}

type HostInfo struct {
	EngineVer string `json:"engineVersion"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	CPUs      int    `json:"cpus"`
	Memory    int64  `json:"memory"`
	Hostname  string `json:"hostname"`
}

func NewHost(d *docker.Docker, di dao.Interface, eb EventBiz) HostBiz {
	return &hostBiz{d: d, di: di, eb: eb}
}

type hostBiz struct {
	d  *docker.Docker
	di dao.Interface
	eb EventBiz
}

func (b *hostBiz) Search(ctx context.Context, name, status string, pageIndex, pageSize int) ([]*dao.Host, int, error) {
	args := &dao.HostSearchArgs{
		Name:      name,
		Status:    status,
		PageIndex: pageIndex,
		PageSize:  pageSize,
	}
	return b.di.HostSearch(ctx, args)
}

func (b *hostBiz) Find(ctx context.Context, id string) (*dao.Host, error) {
	return b.di.HostGet(ctx, id)
}

func (b *hostBiz) GetAll(ctx context.Context) ([]*dao.Host, error) {
	return b.di.HostGetAll(ctx)
}

func (b *hostBiz) Create(ctx context.Context, host *dao.Host, user web.User) error {
	host.ID = createId()
	host.Status = "disconnected"
	host.CreatedAt = now()
	host.UpdatedAt = host.CreatedAt
	host.CreatedBy = newOperator(user)
	host.UpdatedBy = host.CreatedBy

	err := b.di.HostCreate(ctx, host)
	if err == nil {
		b.eb.CreateHost(EventActionCreate, host.ID, host.Name, user)
		// Try initial connection
		go b.syncHost(host.ID, host.Endpoint)
	}
	return err
}

func (b *hostBiz) Update(ctx context.Context, host *dao.Host, user web.User) error {
	host.UpdatedAt = now()
	host.UpdatedBy = newOperator(user)

	err := b.di.HostUpdate(ctx, host)
	if err == nil {
		b.eb.CreateHost(EventActionUpdate, host.ID, host.Name, user)
		// Refresh client with new endpoint
		b.d.Hosts.RemoveClient(host.ID)
		go b.syncHost(host.ID, host.Endpoint)
	}
	return err
}

func (b *hostBiz) Delete(ctx context.Context, id, name string, user web.User) error {
	b.d.Hosts.RemoveClient(id)
	err := b.di.HostDelete(ctx, id)
	if err == nil {
		b.eb.CreateHost(EventActionDelete, id, name, user)
	}
	return err
}

func (b *hostBiz) Test(ctx context.Context, endpoint string) (*HostInfo, error) {
	info, err := b.d.Hosts.TestConnection(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	return &HostInfo{
		EngineVer: info.ServerVersion,
		OS:        info.OSType,
		Arch:      info.Architecture,
		CPUs:      info.NCPU,
		Memory:    info.MemTotal,
		Hostname:  info.Name,
	}, nil
}

func (b *hostBiz) Sync(ctx context.Context, id string) error {
	host, err := b.di.HostGet(ctx, id)
	if err != nil || host == nil {
		return err
	}
	b.syncHost(host.ID, host.Endpoint)
	return nil
}

func (b *hostBiz) syncHost(id, endpoint string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	info, err := b.d.Hosts.TestConnection(ctx, endpoint)
	if err != nil {
		_ = b.di.HostUpdateStatus(context.Background(), id, "error", err.Error(), "")
		return
	}

	_ = b.di.HostUpdateStatus(context.Background(), id, "connected", "", info.ServerVersion)
	// Ensure client is cached for future operations
	_, _ = b.d.Hosts.GetClient(id, endpoint)
}
