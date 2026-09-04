package monitoring

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/proximile/proxiport/db/sqlite"
	"github.com/proximile/proxiport/share/models"
)

// vacuumCountingService records Vacuum calls and reports a fixed number of
// deleted rows, so a cleanup run's vacuum decision can be observed directly.
type vacuumCountingService struct {
	Service
	deleted int64
	vacuums int
}

func (s *vacuumCountingService) DeleteMeasurementsOlderThan(context.Context, time.Duration) (int64, error) {
	return s.deleted, nil
}

func (s *vacuumCountingService) Vacuum(context.Context) error {
	s.vacuums++
	return nil
}

// A fresh process must not VACUUM on its first cleanup. lastVacuum used to be
// left at the zero time, making time.Since(lastVacuum) ~2000 years, so the 24h
// throttle was defeated by any restart: a daemon restarted hourly vacuumed
// hourly, and VACUUM on a multi-gigabyte monitoring database is expensive.
func TestCleanupTaskDoesNotVacuumOnFirstRunAfterStart(t *testing.T) {
	svc := &vacuumCountingService{deleted: 100}
	task := NewCleanupTask(testLog, svc, time.Hour)

	require.NoError(t, task.Run(context.Background()))
	assert.Equal(t, 0, svc.vacuums, "a just-started process should not vacuum yet")

	// Once an interval has passed, a cleanup that removed rows does vacuum.
	task.lastVacuum = time.Now().Add(-vacuumMinInterval - time.Minute)
	require.NoError(t, task.Run(context.Background()))
	assert.Equal(t, 1, svc.vacuums)
}

// Nothing was deleted, so there is nothing to reclaim.
func TestCleanupTaskSkipsVacuumWhenNothingDeleted(t *testing.T) {
	svc := &vacuumCountingService{deleted: 0}
	task := NewCleanupTask(testLog, svc, time.Hour)
	task.lastVacuum = time.Now().Add(-vacuumMinInterval - time.Minute)

	require.NoError(t, task.Run(context.Background()))
	assert.Equal(t, 0, svc.vacuums)
}

// VACUUM rewrites every page of the database, and in WAL mode those pages go
// through the -wal file first. An automatic checkpoint only rewinds the WAL for
// reuse and never shrinks it, so without an explicit TRUNCATE the reclaim left
// behind a -wal as large as the database it had just compacted.
func TestVacuumTruncatesWriteAheadLog(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "monitoring.db")

	dbProvider, err := NewSqliteProvider(dbPath, sqlite.DataSourceOptions{WALEnabled: true}, nil, testLog)
	require.NoError(t, err)
	defer func() { _ = dbProvider.Close() }()

	ctx := context.Background()

	// Enough rows, with large payloads, that the WAL is unmistakably non-empty.
	payload := make([]byte, 64*1024)
	for i := range payload {
		payload[i] = 'x'
	}
	for i := 0; i < 200; i++ {
		require.NoError(t, dbProvider.CreateMeasurement(ctx, &models.Measurement{
			ClientID:    fmt.Sprintf("client_%d", i),
			Timestamp:   testStart.Add(time.Duration(i) * time.Second),
			Processes:   string(payload),
			Mountpoints: "{}",
		}))
	}

	walPath := dbPath + "-wal"
	before, err := os.Stat(walPath)
	require.NoError(t, err, "WAL should exist while the database is in WAL mode")
	require.Greater(t, before.Size(), int64(0), "precondition: the WAL has content to reclaim")

	require.NoError(t, dbProvider.Vacuum(ctx))

	after, err := os.Stat(walPath)
	require.NoError(t, err)
	assert.Zero(t, after.Size(), "VACUUM should leave a truncated WAL, not one the size of the database")
}
