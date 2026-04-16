package bolt

import (
	"context"
	"strings"

	"github.com/cuigh/swirl/dao"
)

const VaultSecret = "vault_secret"

func (d *Dao) VaultSecretCreate(ctx context.Context, s *dao.VaultSecret) error {
	return d.replace(VaultSecret, s.ID, s)
}

func (d *Dao) VaultSecretUpdate(ctx context.Context, s *dao.VaultSecret) error {
	old := &dao.VaultSecret{}
	return d.update(VaultSecret, s.ID, old, func() interface{} {
		s.CreatedAt = old.CreatedAt
		s.CreatedBy = old.CreatedBy
		return s
	})
}

func (d *Dao) VaultSecretGet(ctx context.Context, id string) (*dao.VaultSecret, error) {
	s := &dao.VaultSecret{}
	err := d.get(VaultSecret, id, s)
	if err == ErrNoRecords {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	return s, nil
}

func (d *Dao) VaultSecretGetByName(ctx context.Context, name string) (*dao.VaultSecret, error) {
	var found *dao.VaultSecret
	err := d.each(VaultSecret, func(v []byte) error {
		s := &dao.VaultSecret{}
		if e := decode(v, s); e != nil {
			return e
		}
		if s.Name == name {
			found = s
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return found, nil
}

func (d *Dao) VaultSecretGetAll(ctx context.Context) (items []*dao.VaultSecret, err error) {
	err = d.each(VaultSecret, func(v []byte) error {
		s := &dao.VaultSecret{}
		if e := decode(v, s); e != nil {
			return e
		}
		items = append(items, s)
		return nil
	})
	return
}

func (d *Dao) VaultSecretSearch(ctx context.Context, args *dao.VaultSecretSearchArgs) (items []*dao.VaultSecret, count int, err error) {
	err = d.each(VaultSecret, func(v []byte) error {
		s := &dao.VaultSecret{}
		if e := decode(v, s); e != nil {
			return e
		}
		if args.Name != "" && !strings.Contains(s.Name, args.Name) {
			return nil
		}
		items = append(items, s)
		return nil
	})
	if err == nil {
		count = len(items)
	}
	return
}

func (d *Dao) VaultSecretDelete(ctx context.Context, id string) error {
	return d.delete(VaultSecret, id)
}
