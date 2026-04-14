package bolt

import (
	"context"
	"strings"

	"github.com/cuigh/swirl/dao"
)

const ComposeStack = "compose_stack"

func (d *Dao) ComposeStackCreate(ctx context.Context, stack *dao.ComposeStack) error {
	return d.replace(ComposeStack, stack.ID, stack)
}

func (d *Dao) ComposeStackUpdate(ctx context.Context, stack *dao.ComposeStack) error {
	old := &dao.ComposeStack{}
	return d.update(ComposeStack, stack.ID, old, func() interface{} {
		stack.CreatedAt = old.CreatedAt
		stack.CreatedBy = old.CreatedBy
		if stack.Status == "" {
			stack.Status = old.Status
		}
		return stack
	})
}

func (d *Dao) ComposeStackUpdateStatus(ctx context.Context, id, status string) error {
	old := &dao.ComposeStack{}
	return d.update(ComposeStack, id, old, func() interface{} {
		old.Status = status
		return old
	})
}

func (d *Dao) ComposeStackGet(ctx context.Context, id string) (*dao.ComposeStack, error) {
	s := &dao.ComposeStack{}
	err := d.get(ComposeStack, id, s)
	if err == ErrNoRecords {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	return s, nil
}

func (d *Dao) ComposeStackGetByName(ctx context.Context, hostID, name string) (*dao.ComposeStack, error) {
	var found *dao.ComposeStack
	err := d.each(ComposeStack, func(v []byte) error {
		s := &dao.ComposeStack{}
		if e := decode(v, s); e != nil {
			return e
		}
		if s.HostID == hostID && s.Name == name {
			found = s
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return found, nil
}

func (d *Dao) ComposeStackSearch(ctx context.Context, args *dao.ComposeStackSearchArgs) (stacks []*dao.ComposeStack, count int, err error) {
	err = d.each(ComposeStack, func(v []byte) error {
		s := &dao.ComposeStack{}
		if e := decode(v, s); e != nil {
			return e
		}
		if args.HostID != "" && s.HostID != args.HostID {
			return nil
		}
		if args.Name != "" && !strings.Contains(s.Name, args.Name) {
			return nil
		}
		stacks = append(stacks, s)
		return nil
	})
	if err == nil {
		count = len(stacks)
	}
	return
}

func (d *Dao) ComposeStackDelete(ctx context.Context, id string) error {
	return d.delete(ComposeStack, id)
}
