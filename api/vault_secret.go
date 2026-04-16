package api

import (
	"github.com/cuigh/auxo/data"
	"github.com/cuigh/auxo/net/web"
	"github.com/cuigh/swirl/biz"
	"github.com/cuigh/swirl/dao"
	"github.com/cuigh/swirl/misc"
)

// VaultSecretHandler exposes CRUD over the Vault secret catalog. These
// endpoints are shared between swarm and standalone mode: swarm users can
// still use them to manage external references, standalone users need them
// to back their docker-compose stacks.
type VaultSecretHandler struct {
	Search   web.HandlerFunc `path:"/search" auth:"vault_secret.view" desc:"search vault secret catalog"`
	Find     web.HandlerFunc `path:"/find" auth:"vault_secret.view" desc:"find vault secret by id"`
	List     web.HandlerFunc `path:"/list" auth:"vault_secret.view" desc:"list all vault secrets"`
	Delete   web.HandlerFunc `path:"/delete" method:"post" auth:"vault_secret.delete" desc:"delete vault secret"`
	Save     web.HandlerFunc `path:"/save" method:"post" auth:"vault_secret.edit" desc:"create or update vault secret"`
	Preview  web.HandlerFunc `path:"/preview" auth:"vault_secret.view" desc:"preview vault secret field names (never values)"`
	Write    web.HandlerFunc `path:"/write" method:"post" auth:"vault_secret.edit" desc:"write a new version of the secret value into Vault"`
	Statuses web.HandlerFunc `path:"/statuses" auth:"vault_secret.view" desc:"batch fetch per-entry metadata from Vault (versions, existence)"`
}

// NewVaultSecret creates an instance of VaultSecretHandler.
func NewVaultSecret(b biz.VaultSecretBiz) *VaultSecretHandler {
	return &VaultSecretHandler{
		Search:   vaultSecretSearch(b),
		Find:     vaultSecretFind(b),
		List:     vaultSecretList(b),
		Delete:   vaultSecretDelete(b),
		Save:     vaultSecretSave(b),
		Preview:  vaultSecretPreview(b),
		Write:    vaultSecretWrite(b),
		Statuses: vaultSecretStatuses(b),
	}
}

func vaultSecretSearch(b biz.VaultSecretBiz) web.HandlerFunc {
	type Args struct {
		Name      string `bind:"name"`
		PageIndex int    `bind:"pageIndex"`
		PageSize  int    `bind:"pageSize"`
	}
	return func(c web.Context) error {
		args := &Args{}
		if err := c.Bind(args); err != nil {
			return err
		}

		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()

		items, total, err := b.Search(ctx, args.Name, args.PageIndex, args.PageSize)
		if err != nil {
			return err
		}
		return success(c, data.Map{
			"items": items,
			"total": total,
		})
	}
}

func vaultSecretFind(b biz.VaultSecretBiz) web.HandlerFunc {
	return func(c web.Context) error {
		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()

		id := c.Query("id")
		secret, err := b.Find(ctx, id)
		if err != nil {
			return err
		}
		return success(c, secret)
	}
}

func vaultSecretList(b biz.VaultSecretBiz) web.HandlerFunc {
	return func(c web.Context) error {
		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()

		items, err := b.GetAll(ctx)
		if err != nil {
			return err
		}
		return success(c, items)
	}
}

func vaultSecretDelete(b biz.VaultSecretBiz) web.HandlerFunc {
	type Args struct {
		ID string `json:"id"`
	}
	return func(c web.Context) (err error) {
		args := &Args{}
		if err = c.Bind(args); err == nil {
			ctx, cancel := misc.Context(defaultTimeout)
			defer cancel()

			err = b.Delete(ctx, args.ID, c.User())
		}
		return ajax(c, err)
	}
}

func vaultSecretSave(b biz.VaultSecretBiz) web.HandlerFunc {
	return func(c web.Context) error {
		s := &dao.VaultSecret{}
		err := c.Bind(s, true)
		if err != nil {
			return ajax(c, err)
		}
		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()

		if s.ID == "" {
			id, createErr := b.Create(ctx, s, c.User())
			if createErr != nil {
				return ajax(c, createErr)
			}
			return success(c, data.Map{"id": id})
		}
		return ajax(c, b.Update(ctx, s, c.User()))
	}
}

func vaultSecretPreview(b biz.VaultSecretBiz) web.HandlerFunc {
	return func(c web.Context) error {
		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()

		id := c.Query("id")
		exists, fields, err := b.Preview(ctx, id)
		if err != nil {
			return err
		}
		return success(c, data.Map{
			"exists": exists,
			"fields": fields,
		})
	}
}

func vaultSecretWrite(b biz.VaultSecretBiz) web.HandlerFunc {
	type Args struct {
		ID      string         `json:"id"`
		Data    map[string]any `json:"data"`
		Replace bool           `json:"replace"`
	}
	return func(c web.Context) error {
		args := &Args{}
		if err := c.Bind(args); err != nil {
			return ajax(c, err)
		}
		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()
		return ajax(c, b.WriteValue(ctx, args.ID, args.Data, args.Replace, c.User()))
	}
}

func vaultSecretStatuses(b biz.VaultSecretBiz) web.HandlerFunc {
	return func(c web.Context) error {
		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()
		statuses, err := b.GetStatuses(ctx)
		if err != nil {
			return err
		}
		return success(c, statuses)
	}
}
