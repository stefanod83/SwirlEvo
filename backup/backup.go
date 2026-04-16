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

// Start launches the hourly ticker. A missing or short SWIRL_BACKUP_KEY
// is logged as a warning; the ticker still runs, but each tick will skip
// actual archive creation until the key is configured.
func (s *Scheduler) Start() {
	if !s.b.KeyConfigured() {
		s.logger.Warn("SWIRL_BACKUP_KEY is not configured — scheduled backups will be skipped")
	}
	run.Schedule(time.Hour, s.tick, func(e interface{}) {
		s.logger.Error("backup scheduler panic: ", e)
	})
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

// Start is the hook called from main.Pipeline.
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
