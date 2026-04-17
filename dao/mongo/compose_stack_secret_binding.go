package mongo

import (
	"context"

	"github.com/cuigh/swirl/dao"
	"go.mongodb.org/mongo-driver/bson"
)

const ComposeStackSecretBinding = "compose_stack_secret_binding"

func (d *Dao) ComposeStackSecretBindingGet(ctx context.Context, id string) (*dao.ComposeStackSecretBinding, error) {
	b := &dao.ComposeStackSecretBinding{}
	found, err := d.find(ctx, ComposeStackSecretBinding, id, b)
	if !found {
		return nil, err
	}
	return b, nil
}

func (d *Dao) ComposeStackSecretBindingGetByStack(ctx context.Context, stackID string) (items []*dao.ComposeStackSecretBinding, err error) {
	items = []*dao.ComposeStackSecretBinding{}
	err = d.fetch(ctx, ComposeStackSecretBinding, bson.M{"stack_id": stackID}, &items)
	return
}

func (d *Dao) ComposeStackSecretBindingGetByVaultSecret(ctx context.Context, vaultSecretID string) (items []*dao.ComposeStackSecretBinding, err error) {
	items = []*dao.ComposeStackSecretBinding{}
	err = d.fetch(ctx, ComposeStackSecretBinding, bson.M{"vault_secret_id": vaultSecretID}, &items)
	return
}

func (d *Dao) ComposeStackSecretBindingGetAll(ctx context.Context) (items []*dao.ComposeStackSecretBinding, err error) {
	items = []*dao.ComposeStackSecretBinding{}
	err = d.fetch(ctx, ComposeStackSecretBinding, bson.M{}, &items)
	return
}

func (d *Dao) ComposeStackSecretBindingUpsert(ctx context.Context, binding *dao.ComposeStackSecretBinding) error {
	update := bson.M{
		"$set": bson.M{
			"stack_id":        binding.StackID,
			"vault_secret_id": binding.VaultSecretID,
			"field":           binding.Field,
			"service":         binding.Service,
			"target_type":     binding.TargetType,
			"target_path":     binding.TargetPath,
			"env_name":        binding.EnvName,
			"uid":             binding.UID,
			"gid":             binding.GID,
			"mode":            binding.Mode,
			"storage_mode":    binding.StorageMode,
			"deployed_hash":   binding.DeployedHash,
			"deployed_at":     binding.DeployedAt,
			"updated_at":      binding.UpdatedAt,
			"updated_by":      binding.UpdatedBy,
		},
		"$setOnInsert": bson.M{
			"created_at": binding.CreatedAt,
			"created_by": binding.CreatedBy,
		},
	}
	return d.upsert(ctx, ComposeStackSecretBinding, binding.ID, update)
}

func (d *Dao) ComposeStackSecretBindingDelete(ctx context.Context, id string) error {
	return d.delete(ctx, ComposeStackSecretBinding, id)
}

func (d *Dao) ComposeStackSecretBindingDeleteByStack(ctx context.Context, stackID string) error {
	_, err := d.db.Collection(ComposeStackSecretBinding).DeleteMany(ctx, bson.M{"stack_id": stackID})
	return err
}
