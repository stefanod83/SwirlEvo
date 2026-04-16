package bolt

import (
	"context"

	"github.com/cuigh/swirl/dao"
)

const ComposeStackSecretBinding = "compose_stack_secret_binding"

func (d *Dao) ComposeStackSecretBindingGet(ctx context.Context, id string) (*dao.ComposeStackSecretBinding, error) {
	b := &dao.ComposeStackSecretBinding{}
	err := d.get(ComposeStackSecretBinding, id, b)
	if err == ErrNoRecords {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	return b, nil
}

func (d *Dao) ComposeStackSecretBindingGetByStack(ctx context.Context, stackID string) (items []*dao.ComposeStackSecretBinding, err error) {
	err = d.each(ComposeStackSecretBinding, func(v []byte) error {
		b := &dao.ComposeStackSecretBinding{}
		if e := decode(v, b); e != nil {
			return e
		}
		if b.StackID == stackID {
			items = append(items, b)
		}
		return nil
	})
	return
}

func (d *Dao) ComposeStackSecretBindingGetByVaultSecret(ctx context.Context, vaultSecretID string) (items []*dao.ComposeStackSecretBinding, err error) {
	err = d.each(ComposeStackSecretBinding, func(v []byte) error {
		b := &dao.ComposeStackSecretBinding{}
		if e := decode(v, b); e != nil {
			return e
		}
		if b.VaultSecretID == vaultSecretID {
			items = append(items, b)
		}
		return nil
	})
	return
}

func (d *Dao) ComposeStackSecretBindingGetAll(ctx context.Context) (items []*dao.ComposeStackSecretBinding, err error) {
	err = d.each(ComposeStackSecretBinding, func(v []byte) error {
		b := &dao.ComposeStackSecretBinding{}
		if e := decode(v, b); e != nil {
			return e
		}
		items = append(items, b)
		return nil
	})
	return
}

func (d *Dao) ComposeStackSecretBindingUpsert(ctx context.Context, binding *dao.ComposeStackSecretBinding) error {
	return d.replace(ComposeStackSecretBinding, binding.ID, binding)
}

func (d *Dao) ComposeStackSecretBindingDelete(ctx context.Context, id string) error {
	return d.delete(ComposeStackSecretBinding, id)
}

func (d *Dao) ComposeStackSecretBindingDeleteByStack(ctx context.Context, stackID string) error {
	items, err := d.ComposeStackSecretBindingGetByStack(ctx, stackID)
	if err != nil {
		return err
	}
	for _, b := range items {
		if err := d.delete(ComposeStackSecretBinding, b.ID); err != nil {
			return err
		}
	}
	return nil
}
