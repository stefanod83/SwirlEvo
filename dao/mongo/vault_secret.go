package mongo

import (
	"context"

	"github.com/cuigh/swirl/dao"
	"go.mongodb.org/mongo-driver/bson"
)

const VaultSecret = "vault_secret"

func (d *Dao) VaultSecretCreate(ctx context.Context, s *dao.VaultSecret) error {
	return d.create(ctx, VaultSecret, s)
}

func (d *Dao) VaultSecretUpdate(ctx context.Context, s *dao.VaultSecret) error {
	update := bson.M{
		"$set": bson.M{
			"name":       s.Name,
			"desc":       s.Description,
			"path":       s.Path,
			"field":      s.Field,
			"labels":     s.Labels,
			"updated_at": s.UpdatedAt,
			"updated_by": s.UpdatedBy,
		},
	}
	return d.update(ctx, VaultSecret, s.ID, update)
}

func (d *Dao) VaultSecretGet(ctx context.Context, id string) (*dao.VaultSecret, error) {
	s := &dao.VaultSecret{}
	found, err := d.find(ctx, VaultSecret, id, s)
	if !found {
		return nil, err
	}
	return s, nil
}

func (d *Dao) VaultSecretGetByName(ctx context.Context, name string) (*dao.VaultSecret, error) {
	items := []*dao.VaultSecret{}
	err := d.fetch(ctx, VaultSecret, bson.M{"name": name}, &items)
	if err != nil || len(items) == 0 {
		return nil, err
	}
	return items[0], nil
}

func (d *Dao) VaultSecretGetAll(ctx context.Context) (items []*dao.VaultSecret, err error) {
	items = []*dao.VaultSecret{}
	err = d.fetch(ctx, VaultSecret, bson.M{}, &items)
	return
}

func (d *Dao) VaultSecretSearch(ctx context.Context, args *dao.VaultSecretSearchArgs) (items []*dao.VaultSecret, count int, err error) {
	filter := bson.M{}
	if args.Name != "" {
		filter["name"] = bson.M{"$regex": args.Name}
	}
	opts := searchOptions{filter: filter, pageIndex: args.PageIndex, pageSize: args.PageSize}
	items = []*dao.VaultSecret{}
	count, err = d.search(ctx, VaultSecret, opts, &items)
	return
}

func (d *Dao) VaultSecretDelete(ctx context.Context, id string) error {
	return d.delete(ctx, VaultSecret, id)
}
