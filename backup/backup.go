package backup

import (
	"time"

	"github.com/cuigh/auxo/app/container"
	"github.com/cuigh/auxo/log"
	"github.com/cuigh/auxo/util/run"
	"github.com/cuigh/swirl/biz"
	"github.com/cuigh/swirl/misc"
)

const Name = "backup"

// Scheduler periodically inspects backup schedules and runs them when due.
type Scheduler struct {
	b      biz.BackupBiz
	logger log.Logger
}

// NewScheduler returns a scheduler wired to the backup biz.
func NewScheduler(b biz.BackupBiz) *Scheduler {
	return &Scheduler{b: b, logger: log.Get(Name)}
}

// Start launches the hourly ticker. If neither the SWIRL_BACKUP_KEY env
// var nor the Vault fallback yields a usable passphrase, a warning is
// logged; the ticker still runs, but each tick skips archive creation
// until a key becomes available.
func (s *Scheduler) Start() {
	if !s.b.KeyConfigured() {
		s.logger.Warn("backup passphrase is not available — set SWIRL_BACKUP_KEY or configure Vault with backup_key_path. Scheduled backups will be skipped.")
	}
	// Run the key compatibility check on a goroutine so it never blocks
	// startup. One log line summarises the result; per-backup detail is
	// available via the API/UI.
	go s.runStartupKeyCheck()
	run.Schedule(time.Hour, s.tick, func(e interface{}) {
		s.logger.Error("backup scheduler panic: ", e)
	})
}

// runStartupKeyCheck does a single, non-trial-decrypting pass over all
// backups and logs the aggregate result. Cheap and rotation-aware — see
// biz.BackupBiz.VerifyAll for the classification rules.
func (s *Scheduler) runStartupKeyCheck() {
	if !s.b.KeyConfigured() {
		s.logger.Info("backup key compatibility check skipped: no master key configured")
		return
	}
	ctx, cancel := misc.Context(2 * time.Minute)
	defer cancel()
	sum := s.b.VerifyAll(ctx)
	if sum.Total == 0 {
		return
	}
	if sum.Incompatible == 0 {
		s.logger.Infof("backup key check: %d/%d compatible (%d legacy unverified, %d missing files)",
			sum.Compatible, sum.Total, sum.Unverified, sum.Missing)
	} else {
		s.logger.Warnf("backup key check: %d incompatible / %d legacy unverified out of %d (key fingerprint %s). Use 'Recover' on the Backups page to re-encrypt with the current key.",
			sum.Incompatible, sum.Unverified, sum.Total, sum.Fingerprint)
	}
}

func (s *Scheduler) tick() {
	if !s.b.KeyConfigured() {
		// Check again at the next hour; operator may set the env var and restart.
		return
	}

	ctx, cancel := misc.Context(10 * time.Minute)
	defer cancel()

	schedules, err := s.b.Schedules(ctx)
	if err != nil {
		s.logger.Error("backup scheduler: cannot load schedules: ", err)
		return
	}

	now := time.Now()
	for _, schedule := range schedules {
		if !biz.ShouldRun(schedule, now) {
			continue
		}
		s.logger.Infof("running %s backup schedule", schedule.ID)
		if err := s.b.RunScheduled(ctx, schedule); err != nil {
			s.logger.Errorf("backup schedule %s failed: %v", schedule.ID, err)
		}
	}
}

// Start is the hook called from main.Pipeline. The Vault-backed fallback for
// SWIRL_BACKUP_KEY is installed earlier by main.initBackupKeyProvider — here
// we only need to launch the scheduler.
func Start() error {
	s, err := container.TryFind(Name)
	if err == nil {
		s.(*Scheduler).Start()
	}
	return err
}

func init() {
	container.Put(NewScheduler, container.Name(Name))
}
