package monitoring

import (
	"context"
	"fmt"
	"time"

	"github.com/proximile/proxiport/share/logger"
)

// vacuumMinInterval throttles the reclaim VACUUM. DELETE alone never shrinks
// the SQLite file, so we compact it — but VACUUM is expensive and briefly
// locks the DB, so run it at most this often, and only after a cleanup that
// actually removed rows.
const vacuumMinInterval = 24 * time.Hour

type CleanupTask struct {
	log        *logger.Logger
	service    Service
	duration   time.Duration
	lastVacuum time.Time
}

// NewCleanupTask returns a task to cleanup monitoring data after configured period
func NewCleanupTask(log *logger.Logger, service Service, duration time.Duration) *CleanupTask {
	return &CleanupTask{
		log:     log,
		service: service,
		// Start the VACUUM clock now rather than at the zero time. Left at the
		// zero value, time.Since(lastVacuum) is ~2000 years on a fresh process,
		// so the first cleanup that deleted anything vacuumed immediately and
		// the 24h throttle only ever applied within a single run: a daemon that
		// restarts daily vacuums daily, one that restarts hourly vacuums hourly.
		lastVacuum: time.Now(),
		duration:   duration,
	}
}

func (t *CleanupTask) Run(ctx context.Context) error {
	deletedRecords, err := t.service.DeleteMeasurementsOlderThan(ctx, t.duration)
	if err != nil {
		return fmt.Errorf("failed to cleanup measurements: %v", err)
	}
	t.log.Debugf("monitoring.CleanupTask: %d measurement records deleted", deletedRecords)

	// Reclaim the freed pages so monitoring.db actually shrinks on disk, but
	// only when something was deleted and not more than once per interval.
	if deletedRecords > 0 && time.Since(t.lastVacuum) >= vacuumMinInterval {
		if err := t.service.Vacuum(ctx); err != nil {
			t.log.Errorf("monitoring.CleanupTask: could not reclaim space (VACUUM): %v", err)
		} else {
			t.lastVacuum = time.Now()
			t.log.Infof("monitoring.CleanupTask: reclaimed monitoring.db free space")
		}
	}
	return nil
}
