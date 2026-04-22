package bolt

import (
	"context"
	"sort"

	"github.com/cuigh/swirl/dao"
)

const ComposeStackVersion = "compose_stack_version"

func (d *Dao) ComposeStackVersionCreate(ctx context.Context, v *dao.ComposeStackVersion) error {
	return d.replace(ComposeStackVersion, v.ID, v)
}

// ComposeStackVersionList returns versions for a stack newest-first
// (revision descending). limit <= 0 returns all.
func (d *Dao) ComposeStackVersionList(ctx context.Context, stackID string, limit int) ([]*dao.ComposeStackVersion, error) {
	var items []*dao.ComposeStackVersion
	err := d.each(ComposeStackVersion, func(buf []byte) error {
		v := &dao.ComposeStackVersion{}
		if e := decode(buf, v); e != nil {
			return e
		}
		if v.StackID == stackID {
			items = append(items, v)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].Revision > items[j].Revision
	})
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (d *Dao) ComposeStackVersionGet(ctx context.Context, id string) (*dao.ComposeStackVersion, error) {
	v := &dao.ComposeStackVersion{}
	err := d.get(ComposeStackVersion, id, v)
	if err == ErrNoRecords {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	return v, nil
}

// ComposeStackVersionDeleteByStack removes every version belonging to the
// stack. Called when the stack itself is deleted so no orphans pile up.
func (d *Dao) ComposeStackVersionDeleteByStack(ctx context.Context, stackID string) error {
	var ids []string
	err := d.each(ComposeStackVersion, func(buf []byte) error {
		v := &dao.ComposeStackVersion{}
		if e := decode(buf, v); e != nil {
			return e
		}
		if v.StackID == stackID {
			ids = append(ids, v.ID)
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, id := range ids {
		if e := d.delete(ComposeStackVersion, id); e != nil {
			return e
		}
	}
	return nil
}

// ComposeStackVersionPrune keeps the newest `keep` versions for stackID,
// deleting the rest. keep <= 0 is a no-op (retention disabled).
func (d *Dao) ComposeStackVersionPrune(ctx context.Context, stackID string, keep int) error {
	if keep <= 0 {
		return nil
	}
	items, err := d.ComposeStackVersionList(ctx, stackID, 0)
	if err != nil {
		return err
	}
	if len(items) <= keep {
		return nil
	}
	// Items are already sorted newest-first; drop everything past `keep`.
	for _, v := range items[keep:] {
		if e := d.delete(ComposeStackVersion, v.ID); e != nil {
			return e
		}
	}
	return nil
}
