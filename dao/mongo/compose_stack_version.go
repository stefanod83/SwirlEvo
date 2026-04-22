package mongo

import (
	"context"

	"github.com/cuigh/swirl/dao"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const ComposeStackVersion = "compose_stack_version"

func (d *Dao) ComposeStackVersionCreate(ctx context.Context, v *dao.ComposeStackVersion) error {
	return d.create(ctx, ComposeStackVersion, v)
}

// ComposeStackVersionList returns versions for a stack newest-first
// (revision descending). limit <= 0 returns all.
func (d *Dao) ComposeStackVersionList(ctx context.Context, stackID string, limit int) ([]*dao.ComposeStackVersion, error) {
	findOpts := options.Find().SetSort(bson.M{"revision": -1})
	if limit > 0 {
		findOpts.SetLimit(int64(limit))
	}
	cur, err := d.db.Collection(ComposeStackVersion).Find(ctx, bson.M{"stack_id": stackID}, findOpts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var items []*dao.ComposeStackVersion
	if err := cur.All(ctx, &items); err != nil {
		return nil, err
	}
	return items, nil
}

func (d *Dao) ComposeStackVersionGet(ctx context.Context, id string) (*dao.ComposeStackVersion, error) {
	v := &dao.ComposeStackVersion{}
	found, err := d.find(ctx, ComposeStackVersion, id, v)
	if !found {
		return nil, err
	}
	return v, nil
}

func (d *Dao) ComposeStackVersionDeleteByStack(ctx context.Context, stackID string) error {
	_, err := d.db.Collection(ComposeStackVersion).DeleteMany(ctx, bson.M{"stack_id": stackID})
	return err
}

// ComposeStackVersionPrune keeps the newest `keep` versions for stackID,
// deleting the rest. keep <= 0 disables retention (no-op).
func (d *Dao) ComposeStackVersionPrune(ctx context.Context, stackID string, keep int) error {
	if keep <= 0 {
		return nil
	}
	// Strategy: fetch the IDs to PRESERVE, then deleteMany on the
	// complement. Cheaper than loading every doc to filter client-side.
	findOpts := options.Find().
		SetSort(bson.M{"revision": -1}).
		SetLimit(int64(keep)).
		SetProjection(bson.M{"_id": 1})
	cur, err := d.db.Collection(ComposeStackVersion).Find(ctx, bson.M{"stack_id": stackID}, findOpts)
	if err != nil {
		return err
	}
	defer cur.Close(ctx)
	var keepers []struct {
		ID string `bson:"_id"`
	}
	if err := cur.All(ctx, &keepers); err != nil {
		return err
	}
	keepIDs := make([]string, 0, len(keepers))
	for _, k := range keepers {
		keepIDs = append(keepIDs, k.ID)
	}
	_, err = d.db.Collection(ComposeStackVersion).DeleteMany(ctx, bson.M{
		"stack_id": stackID,
		"_id":      bson.M{"$nin": keepIDs},
	})
	return err
}
