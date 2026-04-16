package bolt

import (
	"context"
	"sort"
	"time"

	"github.com/cuigh/swirl/dao"
)

const (
	Backup         = "backup"
	BackupSchedule = "backup_schedule"
)

func (d *Dao) BackupCreate(ctx context.Context, backup *dao.Backup) error {
	return d.replace(Backup, backup.ID, backup)
}

// BackupUpdate overwrites the persisted record. Bolt's replace is upsert
// semantics — same primitive as Create but with intent renamed for the
// callers that mutate an existing row (e.g. Recover, Verify backfill).
func (d *Dao) BackupUpdate(ctx context.Context, backup *dao.Backup) error {
	return d.replace(Backup, backup.ID, backup)
}

func (d *Dao) BackupGet(ctx context.Context, id string) (backup *dao.Backup, err error) {
	backup = &dao.Backup{}
	err = d.get(Backup, id, backup)
	if err == ErrNoRecords {
		return nil, nil
	} else if err != nil {
		backup = nil
	}
	return
}

func (d *Dao) BackupGetAll(ctx context.Context) (backups []*dao.Backup, err error) {
	err = d.each(Backup, func(v []byte) error {
		b := &dao.Backup{}
		if err := decode(v, b); err != nil {
			return err
		}
		backups = append(backups, b)
		return nil
	})
	if err == nil {
		sort.Slice(backups, func(i, j int) bool {
			return backups[i].CreatedAt.After(backups[j].CreatedAt)
		})
	}
	return
}

func (d *Dao) BackupGetBySource(ctx context.Context, source string) (backups []*dao.Backup, err error) {
	err = d.each(Backup, func(v []byte) error {
		b := &dao.Backup{}
		if err := decode(v, b); err != nil {
			return err
		}
		if b.Source == source {
			backups = append(backups, b)
		}
		return nil
	})
	if err == nil {
		sort.Slice(backups, func(i, j int) bool {
			return backups[i].CreatedAt.After(backups[j].CreatedAt)
		})
	}
	return
}

func (d *Dao) BackupDelete(ctx context.Context, id string) error {
	return d.delete(Backup, id)
}

func (d *Dao) BackupScheduleGet(ctx context.Context, id string) (schedule *dao.BackupSchedule, err error) {
	schedule = &dao.BackupSchedule{}
	err = d.get(BackupSchedule, id, schedule)
	if err == ErrNoRecords {
		return nil, nil
	} else if err != nil {
		schedule = nil
	}
	return
}

func (d *Dao) BackupScheduleGetAll(ctx context.Context) (schedules []*dao.BackupSchedule, err error) {
	err = d.each(BackupSchedule, func(v []byte) error {
		s := &dao.BackupSchedule{}
		if err := decode(v, s); err != nil {
			return err
		}
		schedules = append(schedules, s)
		return nil
	})
	return
}

func (d *Dao) BackupScheduleUpsert(ctx context.Context, schedule *dao.BackupSchedule) error {
	return d.replace(BackupSchedule, schedule.ID, schedule)
}

func (d *Dao) BackupScheduleDelete(ctx context.Context, id string) error {
	return d.delete(BackupSchedule, id)
}

func (d *Dao) BackupScheduleTouch(ctx context.Context, id string, lastRunAt time.Time) error {
	old := &dao.BackupSchedule{}
	return d.update(BackupSchedule, id, old, func() interface{} {
		t := lastRunAt
		old.LastRunAt = &t
		old.UpdatedAt = lastRunAt
		return old
	})
}
