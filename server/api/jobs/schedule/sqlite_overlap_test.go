package schedule

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	jobsmigration "github.com/proximile/proxiport/db/migration/jobs"
	"github.com/proximile/proxiport/db/sqlite"
	"github.com/proximile/proxiport/server/api/jobs"
	"github.com/proximile/proxiport/server/test/jb"
	"github.com/proximile/proxiport/share/logger"
)

// A job row's started_at is the agent's clock; 'now' in the overlap query is the
// server's. A row dated in the future makes the difference negative, and a plain
// "age <= timeout" test counts every negative age as still running -- so one
// such row pinned its schedule as permanently busy and the schedule silently
// never ran again, for every client it targeted.
func TestCountJobsInProgressIgnoresAJobDatedInTheFuture(t *testing.T) {
	testLog := logger.NewLogger("test", logger.LogOutput{File: os.Stdout}, logger.LogLevelError)

	db, err := sqlite.New(":memory:", jobsmigration.AssetNames(), jobsmigration.Asset, DataSourceOptions)
	require.NoError(t, err)
	dbProv := newSQLiteProvider(db)
	defer func() { _ = dbProv.Close() }()
	ctx := context.Background()

	jobsProvider := jobs.NewSqliteProvider(db, nil, testLog)
	multiJob := jb.NewMulti(t).ScheduleID(testData[0].ID).Build()
	require.NoError(t, jobsProvider.SaveMultiJob(multiJob))

	// Unfinished, and claiming to have started a thousand years from now.
	future := jb.New(t).MultiJobID(multiJob.JID).StartedAt(time.Now().AddDate(1000, 0, 0)).Build()
	require.NoError(t, jobsProvider.SaveJob(future))

	count, err := dbProv.CountJobsInProgress(ctx, testData[0].ID, 60)
	require.NoError(t, err)

	assert.Equal(t, 0, count, "a job dated far outside the timeout window is not in progress, in either direction")
}

// The guard still has to do its job: a job that really did just start, and has
// not finished, blocks a second run of a non-overlapping schedule.
func TestCountJobsInProgressStillCountsARunningJob(t *testing.T) {
	testLog := logger.NewLogger("test", logger.LogOutput{File: os.Stdout}, logger.LogLevelError)

	db, err := sqlite.New(":memory:", jobsmigration.AssetNames(), jobsmigration.Asset, DataSourceOptions)
	require.NoError(t, err)
	dbProv := newSQLiteProvider(db)
	defer func() { _ = dbProv.Close() }()
	ctx := context.Background()

	jobsProvider := jobs.NewSqliteProvider(db, nil, testLog)
	multiJob := jb.NewMulti(t).ScheduleID(testData[0].ID).Build()
	require.NoError(t, jobsProvider.SaveMultiJob(multiJob))

	running := jb.New(t).MultiJobID(multiJob.JID).StartedAt(time.Now()).Build()
	require.NoError(t, jobsProvider.SaveJob(running))

	count, err := dbProv.CountJobsInProgress(ctx, testData[0].ID, 60)
	require.NoError(t, err)

	assert.Equal(t, 1, count)
}
