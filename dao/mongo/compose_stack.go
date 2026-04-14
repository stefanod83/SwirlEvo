package mongo

import (
	"context"

	"github.com/cuigh/swirl/dao"
	"go.mongodb.org/mongo-driver/bson"
)

const ComposeStack = "compose_stack"

func (d *Dao) ComposeStackCreate(ctx context.Context, stack *dao.ComposeStack) error {
	return d.create(ctx, ComposeStack, stack)
}

func (d *Dao) ComposeStackUpdate(ctx context.Context, stack *dao.ComposeStack) error {
	update := bson.M{
		"$set": bson.M{
			"name":       stack.Name,
			"host_id":    stack.HostID,
			"content":    stack.Content,
			"updated_at": stack.UpdatedAt,
			"updated_by": stack.UpdatedBy,
		},
	}
	if stack.Status != "" {
		update["$set"].(bson.M)["status"] = stack.Status
	}
	return d.update(ctx, ComposeStack, stack.ID, update)
}

func (d *Dao) ComposeStackUpdateStatus(ctx context.Context, id, status string) error {
	return d.update(ctx, ComposeStack, id, bson.M{"$set": bson.M{"status": status}})
}

func (d *Dao) ComposeStackGet(ctx context.Context, id string) (*dao.ComposeStack, error) {
	s := &dao.ComposeStack{}
	found, err := d.find(ctx, ComposeStack, id, s)
	if !found {
		return nil, err
	}
	return s, nil
}

func (d *Dao) ComposeStackGetByName(ctx context.Context, hostID, name string) (*dao.ComposeStack, error) {
	stacks := []*dao.ComposeStack{}
	err := d.fetch(ctx, ComposeStack, bson.M{"host_id": hostID, "name": name}, &stacks)
	if err != nil || len(stacks) == 0 {
		return nil, err
	}
	return stacks[0], nil
}

func (d *Dao) ComposeStackSearch(ctx context.Context, args *dao.ComposeStackSearchArgs) (stacks []*dao.ComposeStack, count int, err error) {
	filter := bson.M{}
	if args.HostID != "" {
		filter["host_id"] = args.HostID
	}
	if args.Name != "" {
		filter["name"] = args.Name
	}
	opts := searchOptions{filter: filter, pageIndex: args.PageIndex, pageSize: args.PageSize}
	stacks = []*dao.ComposeStack{}
	count, err = d.search(ctx, ComposeStack, opts, &stacks)
	return
}

func (d *Dao) ComposeStackDelete(ctx context.Context, id string) error {
	return d.delete(ctx, ComposeStack, id)
}
