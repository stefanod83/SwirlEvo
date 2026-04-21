package biz

import (
	"context"
	"errors"
	"time"

	"github.com/cuigh/auxo/data"
	"github.com/cuigh/auxo/net/web"
	"github.com/cuigh/swirl/dao"
	"github.com/cuigh/swirl/docker"
	"github.com/cuigh/swirl/misc"
	"github.com/docker/docker/api/types/image"
)

type ImageBiz interface {
	Search(ctx context.Context, node, name string, pageIndex, pageSize int) ([]*Image, int, error)
	Find(ctx context.Context, node, name string) (image *Image, raw string, err error)
	Delete(ctx context.Context, node, id string, force bool, user web.User) (err error)
	Prune(ctx context.Context, node string, user web.User) (count int, size uint64, err error)
	Tag(ctx context.Context, node, source, target string, user web.User) error
	// Push pushes an image ref to the registry identified by registryID
	// (authentication is resolved from the catalog entry). If registryID
	// is empty, the image is pushed anonymously (relying on whatever
	// the host daemon has in `~/.docker/config.json`).
	Push(ctx context.Context, node, ref, registryID string, user web.User) error
}

// NewImage takes the host biz so exported methods can pre-resolve the
// target host and surface coded errors instead of bare 500s.
func NewImage(d *docker.Docker, hb HostBiz, eb EventBiz, rb RegistryBiz) ImageBiz {
	return &imageBiz{d: d, hb: hb, eb: eb, rb: rb}
}

type imageBiz struct {
	d  *docker.Docker
	hb HostBiz
	eb EventBiz
	rb RegistryBiz
}

// preflight — see networkBiz.preflight.
func (b *imageBiz) preflight(ctx context.Context, node string) (*dao.Host, error) {
	if !misc.IsStandalone() || node == "" || node == "-" {
		return nil, nil
	}
	_, host, err := resolveHostClient(ctx, b.d, b.hb, node)
	if err != nil {
		return nil, err
	}
	return host, nil
}

func wrapImageOpError(op, name string, host *dao.Host, err error) error {
	return wrapOpError(op, "image", name, host, err, misc.ErrImageNotFound, misc.ErrImageOperationFailed)
}

func (b *imageBiz) Find(ctx context.Context, node, id string) (img *Image, raw string, err error) {
	var (
		i         image.InspectResponse
		r         []byte
		histories []image.HistoryResponseItem
	)

	host, err := b.preflight(ctx, node)
	if err != nil {
		return nil, "", err
	}

	if i, r, err = b.d.ImageInspect(ctx, node, id); err != nil {
		return nil, "", wrapImageOpError("inspect", id, host, err)
	}
	if raw, err = indentJSON(r); err != nil {
		return nil, "", err
	}

	if histories, err = b.d.ImageHistory(ctx, node, id); err != nil {
		return nil, raw, wrapImageOpError("history", id, host, err)
	}

	img = newImageDetail(&i, histories)
	return
}

func (b *imageBiz) Search(ctx context.Context, node, name string, pageIndex, pageSize int) (images []*Image, total int, err error) {
	host, err := b.preflight(ctx, node)
	if err != nil {
		return nil, 0, err
	}
	list, total, err := b.d.ImageList(ctx, node, name, pageIndex, pageSize)
	if err != nil {
		return nil, 0, wrapImageOpError("list", "", host, err)
	}

	// Docker's image.Summary.Containers is often -1 (expensive to compute on the
	// daemon). Recompute a reliable per-image reference count by enumerating all
	// containers on the same host and grouping by ImageID.
	usage := map[string]int64{}
	if containers, cErr := b.d.ContainerListAll(ctx, node); cErr == nil {
		for _, c := range containers {
			usage[c.ImageID]++
		}
	}

	images = make([]*Image, len(list))
	for i, nr := range list {
		img := newImageSummary(&nr)
		img.Containers = usage[img.ID]
		images[i] = img
	}
	return images, total, nil
}

func (b *imageBiz) Delete(ctx context.Context, node, id string, force bool, user web.User) (err error) {
	host, err := b.preflight(ctx, node)
	if err != nil {
		return err
	}
	err = b.d.ImageRemove(ctx, node, id, force)
	if err != nil {
		return wrapImageOpError("delete", id, host, err)
	}
	b.eb.CreateImage(EventActionDelete, node, id, user)
	return nil
}

func (b *imageBiz) Prune(ctx context.Context, node string, user web.User) (count int, size uint64, err error) {
	host, err := b.preflight(ctx, node)
	if err != nil {
		return 0, 0, err
	}
	var report image.PruneReport
	if report, err = b.d.ImagePrune(ctx, node); err != nil {
		return 0, 0, wrapImageOpError("prune", "", host, err)
	}
	count, size = len(report.ImagesDeleted), report.SpaceReclaimed
	b.eb.CreateImage(EventActionPrune, node, "", user)
	return count, size, nil
}

func (b *imageBiz) Tag(ctx context.Context, node, source, target string, user web.User) error {
	host, err := b.preflight(ctx, node)
	if err != nil {
		return err
	}
	if err := b.d.ImageTag(ctx, node, source, target); err != nil {
		return wrapImageOpError("tag", source+" -> "+target, host, err)
	}
	b.eb.CreateImage(EventActionUpdate, node, source+" -> "+target, user)
	return nil
}

func (b *imageBiz) Push(ctx context.Context, node, ref, registryID string, user web.User) error {
	host, err := b.preflight(ctx, node)
	if err != nil {
		return err
	}
	auth := ""
	if registryID != "" {
		// Registry resolution is biz-layer logic, not a daemon op —
		// errors here stay un-wrapped (validation-style) per the brief.
		r, err := b.rb.Find(ctx, registryID)
		if err != nil {
			return err
		}
		if r == nil {
			return errors.New("registry not found")
		}
		// Find() strips the password; re-fetch the raw auth via biz.
		// Use GetAuth keyed by URL (existing method) for the encoded
		// AuthConfig, or fall back to anonymous when absent.
		a, aerr := b.rb.GetAuth(ctx, r.URL)
		if aerr != nil {
			return aerr
		}
		auth = a
	}
	if err := b.d.ImagePush(ctx, node, ref, auth); err != nil {
		return wrapImageOpError("push", ref, host, err)
	}
	b.eb.CreateImage(EventActionUpdate, node, "push:"+ref, user)
	return nil
}

type Image struct {
	/* Summary */
	ID          string       `json:"id"`
	ParentID    string       `json:"pid,omitempty"`
	Created     string       `json:"created"`
	Containers  int64        `json:"containers"`
	Digests     []string     `json:"digests"`
	Tags        []string     `json:"tags"`
	Labels      data.Options `json:"labels"`
	Size        int64        `json:"size"`
	SharedSize  int64        `json:"sharedSize"`
	VirtualSize int64        `json:"virtualSize"`

	/* Detail */
	Comment       string           `json:"comment,omitempty"`
	Container     string           `json:"container,omitempty"`
	DockerVersion string           `json:"dockerVersion,omitempty"`
	Author        string           `json:"author,omitempty"`
	Architecture  string           `json:"arch,omitempty"`
	Variant       string           `json:"variant,omitempty"`
	OS            string           `json:"os,omitempty"`
	OSVersion     string           `json:"osVersion,omitempty"`
	GraphDriver   ImageGraphDriver `json:"graphDriver"`
	RootFS        ImageRootFS      `json:"rootFS"`
	LastTagTime   string           `json:"lastTagTime,omitempty"`
	Histories     []*ImageHistory  `json:"histories,omitempty"`
	//Config          *container.Config
	//ContainerConfig *container.Config
}

type ImageGraphDriver struct {
	Name string       `json:"name,omitempty"`
	Data data.Options `json:"data,omitempty"`
}

type ImageRootFS struct {
	Type      string   `json:"type"`
	Layers    []string `json:"layers,omitempty"`
	BaseLayer string   `json:"baseLayer,omitempty"`
}

type ImageHistory struct {
	ID        string   `json:"id,omitempty"`
	Comment   string   `json:"comment,omitempty"`
	Size      int64    `json:"size,omitempty"`
	Tags      []string `json:"tags,omitempty"`
	CreatedAt string   `json:"createdAt,omitempty"`
	CreatedBy string   `json:"createdBy,omitempty"`
}

func newImageSummary(is *image.Summary) *Image {
	i := &Image{
		ID:          is.ID,
		ParentID:    is.ParentID,
		Created:     formatTime(time.Unix(is.Created, 0)),
		Containers:  is.Containers,
		Digests:     is.RepoDigests,
		Tags:        is.RepoTags,
		Labels:      mapToOptions(is.Labels),
		SharedSize:  is.SharedSize,
		Size:        is.Size,
		VirtualSize: is.VirtualSize,
	}
	return i
}

func newImageDetail(is *image.InspectResponse, items []image.HistoryResponseItem) *Image {
	created, _ := time.Parse(time.RFC3339Nano, is.Created)
	histories := make([]*ImageHistory, len(items))
	for i, item := range items {
		histories[i] = &ImageHistory{
			ID:        item.ID,
			Comment:   item.Comment,
			Size:      item.Size,
			Tags:      item.Tags,
			CreatedAt: formatTime(time.Unix(item.Created, 0)),
			CreatedBy: item.CreatedBy,
		}
	}

	i := &Image{
		ID:       is.ID,
		ParentID: is.Parent,
		Created:  formatTime(created),
		Digests:  is.RepoDigests,
		Tags:     is.RepoTags,
		//Labels:      mapToOptions(is.Labels),
		Size:        is.Size,
		VirtualSize: is.VirtualSize,

		Comment:       is.Comment,
		Container:     is.Container,
		DockerVersion: is.DockerVersion,
		Author:        is.Author,
		Architecture:  is.Architecture,
		Variant:       is.Variant,
		OS:            is.Os,
		OSVersion:     is.OsVersion,
		LastTagTime:   formatTime(is.Metadata.LastTagTime),
		GraphDriver: ImageGraphDriver{
			Name: is.GraphDriver.Name,
			Data: mapToOptions(is.GraphDriver.Data),
		},
		RootFS: ImageRootFS{
			Type:   is.RootFS.Type,
			Layers: is.RootFS.Layers,
		},
		Histories: histories,
	}
	return i
}
