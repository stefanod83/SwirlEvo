package mongo

import (
	"context"
	"time"

	"github.com/cuigh/swirl/dao"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	Backup         = "backup"
	BackupSchedule = "backup_schedule"
)

func (d *Dao) BackupCreate(ctx context.Context, backup *dao.Backup) error {
	return d.create(ctx, Backup, backup)
}

// BackupUpdate sets only the mutable fields. created_at / created_by /
// name / source are preserved so a recovery never accidentally rewrites
// the audit trail.
func (d *Dao) BackupUpdate(ctx context.Context, backup *dao.Backup) error {
	return d.update(ctx, Backup, backup.ID, bson.M{"$set": bson.M{
		"size":            backup.Size,
		"checksum":        backup.Checksum,
		"path":            backup.Path,
		"includes":        backup.Includes,
		"stats":           backup.Stats,
		"key_fingerprint": backup.KeyFingerprint,
		"verified_at":     backup.VerifiedAt,
	}})
}

func (d *Dao) BackupGet(ctx context.Context, id string) (backup *dao.Backup, err error) {
	backup = &dao.Backup{}
	found, err := d.find(ctx, Backup, id, backup)
	if !found {
		return nil, err
	}
	return
}

func (d *Dao) BackupGetAll(ctx context.Context) (backups []*dao.Backup, err error) {
	backups = []*dao.Backup{}
	cur, err := d.db.Collection(Backup).Find(ctx, bson.M{}, options.Find().SetSort(bson.M{"created_at": -1}))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	err = cur.All(ctx, &backups)
	return
}

func (d *Dao) BackupGetBySource(ctx context.Context, source string) (backups []*dao.Backup, err error) {
	backups = []*dao.Backup{}
	cur, err := d.db.Collection(Backup).Find(ctx, bson.M{"source": source}, options.Find().SetSort(bson.M{"created_at": -1}))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	err = cur.All(ctx, &backups)
	return
}

func (d *Dao) BackupDelete(ctx context.Context, id string) error {
	return d.delete(ctx, Backup, id)
}

func (d *Dao) BackupScheduleGet(ctx context.Context, id string) (schedule *dao.BackupSchedule, err error) {
	schedule = &dao.BackupSchedule{}
	found, err := d.find(ctx, BackupSchedule, id, schedule)
	if !found {
		return nil, err
	}
	return
}

func (d *Dao) BackupScheduleGetAll(ctx context.Context) (schedules []*dao.BackupSchedule, err error) {
	schedules = []*dao.BackupSchedule{}
	err = d.fetch(ctx, BackupSchedule, bson.M{}, &schedules)
	return
}

func (d *Dao) BackupScheduleUpsert(ctx context.Context, schedule *dao.BackupSchedule) error {
	return d.upsert(ctx, BackupSchedule, schedule.ID, bson.M{"$set": bson.M{
		"enabled":     schedule.Enabled,
		"day_config":  schedule.DayConfig,
		"time":        schedule.Time,
		"retention":   schedule.Retention,
		"last_run_at": schedule.LastRunAt,
		"created_at":  schedule.CreatedAt,
		"updated_at":  schedule.UpdatedAt,
	}})
}

func (d *Dao) BackupScheduleDelete(ctx context.Context, id string) error {
	return d.delete(ctx, BackupSchedule, id)
}

func (d *Dao) BackupScheduleTouch(ctx context.Context, id string, lastRunAt time.Time) error {
	return d.update(ctx, BackupSchedule, id, bson.M{"$set": bson.M{
		"last_run_at": lastRunAt,
		"updated_at":  lastRunAt,
	}})
}
