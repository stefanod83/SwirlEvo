package biz

import (
	"context"
	"time"

	"github.com/cuigh/auxo/data"
	"github.com/cuigh/auxo/net/web"
	"github.com/cuigh/swirl/dao"
	"github.com/cuigh/swirl/docker"
	"github.com/cuigh/swirl/misc"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/volume"
)

type VolumeBiz interface {
	Search(ctx context.Context, node, name string, pageIndex, pageSize int) ([]*Volume, int, error)
	Find(ctx context.Context, node, name string) (volume *Volume, raw string, err error)
	Delete(ctx context.Context, node, name string, user web.User) (err error)
	Create(ctx context.Context, volume *Volume, user web.User) (err error)
	Prune(ctx context.Context, node string, user web.User) (count int, size uint64, err error)
}

// NewVolume takes the host biz so exported methods can pre-resolve the
// target host and surface coded errors instead of bare 500s.
func NewVolume(d *docker.Docker, hb HostBiz, eb EventBiz) VolumeBiz {
	return &volumeBiz{d: d, hb: hb, eb: eb}
}

type volumeBiz struct {
	d  *docker.Docker
	hb HostBiz
	eb EventBiz
}

// preflight — see networkBiz.preflight.
func (b *volumeBiz) preflight(ctx context.Context, node string) (*dao.Host, error) {
	if !misc.IsStandalone() || node == "" || node == "-" {
		return nil, nil
	}
	_, host, err := resolveHostClient(ctx, b.d, b.hb, node)
	if err != nil {
		return nil, err
	}
	return host, nil
}

func wrapVolumeOpError(op, name string, host *dao.Host, err error) error {
	return wrapOpError(op, "volume", name, host, err, misc.ErrVolumeNotFound, misc.ErrVolumeOperationFailed)
}

func (b *volumeBiz) Find(ctx context.Context, node, name string) (vol *Volume, raw string, err error) {
	var (
		v volume.Volume
		r []byte
	)

	host, err := b.preflight(ctx, node)
	if err != nil {
		return nil, "", err
	}

	if v, r, err = b.d.VolumeInspect(ctx, node, name); err != nil {
		return nil, "", wrapVolumeOpError("inspect", name, host, err)
	}
	raw, err = indentJSON(r)
	if err == nil {
		vol = newVolume(&v)
	}
	return
}

func (b *volumeBiz) Search(ctx context.Context, node, name string, pageIndex, pageSize int) (volumes []*Volume, total int, err error) {
	host, err := b.preflight(ctx, node)
	if err != nil {
		return nil, 0, err
	}
	list, total, err := b.d.VolumeList(ctx, node, name, pageIndex, pageSize)
	if err != nil {
		return nil, 0, wrapVolumeOpError("list", "", host, err)
	}

	// Docker's volume.UsageData.RefCount is -1 unless computed explicitly by the
	// daemon. Recompute a reliable ref-count by scanning container mounts on the
	// same host and counting references to each named volume.
	usage := map[string]int64{}
	if containers, cErr := b.d.ContainerListAll(ctx, node); cErr == nil {
		for _, c := range containers {
			for _, m := range c.Mounts {
				if m.Type == mount.TypeVolume && m.Name != "" {
					usage[m.Name]++
				}
			}
		}
	}

	volumes = make([]*Volume, len(list))
	for i, v := range list {
		vol := newVolume(v)
		vol.RefCount = usage[vol.Name]
		volumes[i] = vol
	}
	return volumes, total, nil
}

func (b *volumeBiz) Delete(ctx context.Context, node, name string, user web.User) (err error) {
	host, err := b.preflight(ctx, node)
	if err != nil {
		return err
	}
	err = b.d.VolumeRemove(ctx, node, name)
	if err != nil {
		return wrapVolumeOpError("delete", name, host, err)
	}
	b.eb.CreateVolume(EventActionDelete, node, name, user)
	return nil
}

func (b *volumeBiz) Create(ctx context.Context, vol *Volume, user web.User) (err error) {
	host, err := b.preflight(ctx, vol.Node)
	if err != nil {
		return err
	}
	options := &volume.CreateOptions{
		Name:       vol.Name,
		Driver:     vol.Driver,
		DriverOpts: toMap(vol.Options),
		Labels:     toMap(vol.Labels),
	}
	if vol.Driver == "other" {
		options.Driver = vol.CustomDriver
	} else {
		options.Driver = vol.Driver
	}

	err = b.d.VolumeCreate(ctx, vol.Node, options)
	if err != nil {
		return wrapVolumeOpError("create", vol.Name, host, err)
	}
	b.eb.CreateVolume(EventActionCreate, vol.Node, vol.Name, user)
	return nil
}

func (b *volumeBiz) Prune(ctx context.Context, node string, user web.User) (count int, size uint64, err error) {
	host, err := b.preflight(ctx, node)
	if err != nil {
		return 0, 0, err
	}
	var report volume.PruneReport
	report, err = b.d.VolumePrune(ctx, node)
	if err != nil {
		return 0, 0, wrapVolumeOpError("prune", "", host, err)
	}
	count, size = len(report.VolumesDeleted), report.SpaceReclaimed
	b.eb.CreateVolume(EventActionPrune, node, "", user)
	return count, size, nil
}

type Volume struct {
	Node         string                 `json:"node"`
	Name         string                 `json:"name"`
	Driver       string                 `json:"driver,omitempty"`
	CustomDriver string                 `json:"customDriver,omitempty"`
	CreatedAt    string                 `json:"createdAt"`
	MountPoint   string                 `json:"mountPoint,omitempty"`
	Scope        string                 `json:"scope"`
	Labels       data.Options           `json:"labels,omitempty"`
	Options      data.Options           `json:"options,omitempty"`
	Status       map[string]interface{} `json:"status,omitempty"`
	RefCount     int64                  `json:"refCount"`
	Size         int64                  `json:"size"`
}

func newVolume(v *volume.Volume) *Volume {
	createdAt, _ := time.Parse(time.RFC3339Nano, v.CreatedAt)
	vol := &Volume{
		Name:       v.Name,
		Driver:     v.Driver,
		CreatedAt:  formatTime(createdAt),
		MountPoint: v.Mountpoint,
		Scope:      v.Scope,
		Status:     v.Status,
		Labels:     mapToOptions(v.Labels),
		Options:    mapToOptions(v.Options),
		RefCount:   -1,
		Size:       -1,
	}
	if v.UsageData != nil {
		vol.RefCount = v.UsageData.RefCount
		vol.Size = v.UsageData.Size
	}
	return vol
}
