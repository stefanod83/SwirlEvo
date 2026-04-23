package biz

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/cuigh/auxo/net/web"
	"github.com/cuigh/swirl/dao"
	swirldocker "github.com/cuigh/swirl/docker"
	"github.com/docker/docker/api/types/registry"
)

// ErrRegistryLinked is returned by Delete when a Registry is still
// referenced as the Registry Cache source (Setting.RegistryCache.
// RegistryID). The API handler maps this to HTTP 409 so the UI can
// surface an actionable message ("unlink before deleting").
var ErrRegistryLinked = errors.New("registry is linked as the Registry Cache source — unlink first in Settings")

// RegistryBrowseResult is a paginated slice of repository names returned
// by Browse. `Next` is the opaque cursor for the follow-up call (empty
// when the catalog is exhausted).
type RegistryBrowseResult struct {
	Repos []string `json:"repos"`
	Next  string   `json:"next,omitempty"`
}

type RegistryBiz interface {
	Search(ctx context.Context) ([]*dao.Registry, error)
	Find(ctx context.Context, id string) (*dao.Registry, error)
	GetAuth(ctx context.Context, url string) (auth string, err error)
	Delete(ctx context.Context, id, name string, user web.User) (err error)
	Create(ctx context.Context, registry *dao.Registry, user web.User) (err error)
	Update(ctx context.Context, registry *dao.Registry, user web.User) (err error)
	// Browse lists repositories on the remote registry via its v2 API.
	Browse(ctx context.Context, id string, pageSize int, last string) (*RegistryBrowseResult, error)
	// Tags lists the tag names for a single repository on the registry.
	Tags(ctx context.Context, id, repo string) ([]string, error)
	// Ping checks if the registry is reachable and authenticated.
	Ping(ctx context.Context, id string) error
}

func NewRegistry(d dao.Interface, eb EventBiz, sb SettingBiz) RegistryBiz {
	return &registryBiz{d: d, eb: eb, sb: sb, rc: swirldocker.NewRegistryClient()}
}

type registryBiz struct {
	d  dao.Interface
	eb EventBiz
	sb SettingBiz
	rc *swirldocker.RegistryClient
}

// refreshLinkedRegistryCache re-saves Setting.RegistryCache when this
// Registry is referenced as its source. Copies fresh field values via
// the overlay in Save. Best-effort: a failure here does not roll back
// the Registry update — operators can re-Save Setting.RegistryCache
// manually to recover.
func (b *registryBiz) refreshLinkedRegistryCache(ctx context.Context, registryID string, user web.User) {
	if registryID == "" || b.sb == nil {
		return
	}
	live := LiveSettingsSnapshot()
	if live == nil || live.RegistryCache.RegistryID != registryID {
		return
	}
	// Round-trip the current RegistryCache blob through Save so the
	// overlay in settingBiz.Save re-fetches the Registry and rewrites
	// the cached fields. Marshalling the live struct keeps every
	// existing value (including preserve_digests, rewrite_mode) intact
	// — only Hostname/Port/Username/Password/CA* are overlayed.
	buf, err := json.Marshal(live.RegistryCache)
	if err != nil {
		return
	}
	_ = b.sb.Save(ctx, "registry_cache", json.RawMessage(buf), user)
}

func (b *registryBiz) Create(ctx context.Context, r *dao.Registry, user web.User) (err error) {
	r.ID = createId()
	r.CreatedAt = now()
	r.UpdatedAt = r.CreatedAt
	r.CreatedBy = newOperator(user)
	r.UpdatedBy = r.CreatedBy
	// Derive CA fingerprint from the PEM so the UI can show it
	// without recomputing client-side. Empty PEM → empty fingerprint.
	if r.CACertPEM != "" {
		if fp, fpErr := ComputeCAFingerprint(r.CACertPEM); fpErr == nil {
			r.CAFingerprint = fp
		} else {
			r.CAFingerprint = ""
		}
	} else {
		r.CAFingerprint = ""
	}

	err = b.d.RegistryCreate(ctx, r)
	if err == nil {
		b.eb.CreateRegistry(EventActionCreate, r.ID, r.Name, user)
	}
	return
}

func (b *registryBiz) Update(ctx context.Context, r *dao.Registry, user web.User) (err error) {
	r.UpdatedAt = now()
	r.UpdatedBy = newOperator(user)
	if r.CACertPEM != "" {
		if fp, fpErr := ComputeCAFingerprint(r.CACertPEM); fpErr == nil {
			r.CAFingerprint = fp
		} else {
			r.CAFingerprint = ""
		}
	} else {
		r.CAFingerprint = ""
	}
	err = b.d.RegistryUpdate(ctx, r)
	if err == nil {
		b.eb.CreateRegistry(EventActionUpdate, r.ID, r.Name, user)
		// Refresh the linked RegistryCache snapshot so the updated
		// URL / credentials / CA propagate to the rewriter without
		// an explicit re-save from the operator.
		b.refreshLinkedRegistryCache(ctx, r.ID, user)
	}
	return
}

func (b *registryBiz) Search(ctx context.Context) (registries []*dao.Registry, err error) {
	registries, err = b.d.RegistryGetAll(ctx)
	if err == nil {
		for _, r := range registries {
			r.Password = ""
		}
	}
	return
}

func (b *registryBiz) Find(ctx context.Context, id string) (registry *dao.Registry, err error) {
	registry, err = b.d.RegistryGet(ctx, id)
	if err == nil {
		registry.Password = ""
	}
	return
}

func (b *registryBiz) GetAuth(ctx context.Context, url string) (auth string, err error) {
	var (
		r   *dao.Registry
		buf []byte
	)
	if r, err = b.d.RegistryGetByURL(ctx, url); err == nil && r != nil {
		cfg := &registry.AuthConfig{
			ServerAddress: r.URL,
			Username:      r.Username,
			Password:      r.Password,
		}
		if buf, err = json.Marshal(cfg); err == nil {
			auth = base64.URLEncoding.EncodeToString(buf)
		}
	}
	return
}

func (b *registryBiz) Delete(ctx context.Context, id, name string, user web.User) (err error) {
	// Refuse deletion when this Registry is the configured source of
	// the Registry Cache — pulling the rug would silently break every
	// deploy that relies on the rewriter. Operators can unlink the
	// reference in Settings first and then retry.
	if live := LiveSettingsSnapshot(); live != nil && live.RegistryCache.RegistryID == id {
		return ErrRegistryLinked
	}
	err = b.d.RegistryDelete(ctx, id)
	if err == nil {
		b.eb.CreateRegistry(EventActionDelete, id, name, user)
	}
	return
}

func (b *registryBiz) Browse(ctx context.Context, id string, pageSize int, last string) (*RegistryBrowseResult, error) {
	// Browse uses the full registry row (password included) — the
	// sanitisation applied by Search/Find would strip the credentials.
	r, err := b.d.RegistryGet(ctx, id)
	if err != nil {
		return nil, err
	}
	if r == nil {
		return nil, nil
	}
	repos, next, err := b.rc.CatalogList(ctx, r, pageSize, last)
	if err != nil {
		return nil, err
	}
	return &RegistryBrowseResult{Repos: repos, Next: next}, nil
}

func (b *registryBiz) Tags(ctx context.Context, id, repo string) ([]string, error) {
	r, err := b.d.RegistryGet(ctx, id)
	if err != nil {
		return nil, err
	}
	if r == nil {
		return nil, nil
	}
	return b.rc.TagsList(ctx, r, repo)
}

func (b *registryBiz) Ping(ctx context.Context, id string) error {
	r, err := b.d.RegistryGet(ctx, id)
	if err != nil {
		return err
	}
	if r == nil {
		return fmt.Errorf("registry not found")
	}
	return b.rc.Ping(ctx, r)
}
