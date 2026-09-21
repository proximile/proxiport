package jobs

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/proximile/proxiport/db/migration/jobs"
	"github.com/proximile/proxiport/db/sqlite"
	"github.com/proximile/proxiport/server/test/jb"
)

// withLocalZone sets the process's local zone for the duration of a test. The
// defect under test is that time.Time values carrying different zones are
// ordered as text, so the zone is the input -- there is no way to exercise it
// without setting it, and these tests are therefore not parallel.
func withLocalZone(t *testing.T, name string) *time.Location {
	t.Helper()

	loc, err := time.LoadLocation(name)
	require.NoError(t, err)

	original := time.Local
	time.Local = loc
	t.Cleanup(func() { time.Local = original })

	return loc
}

// TestMultiJobChildrenAreOrderedByInstantNotByText covers a defect the
// TZ sweep for this batch turned up: a multi-job's children are ordered by
// started_at, and those timestamps do not all come from the same clock. A job
// the server could not dispatch keeps the SERVER's time.Now(); one the agent
// accepted is re-stamped with the AGENT's reported start time. The driver
// renders each with its own UTC offset, so ordering the stored text is not
// ordering the instants -- "2026-09-21 23:17:25+00:00" sorts above
// "2026-09-21 19:17:25-04:00" even though they are 4 hours apart the other way.
//
// On a UTC server the two agree, which is why every existing test passed.
func TestMultiJobChildrenAreOrderedByInstantNotByText(t *testing.T) {
	newYork := withLocalZone(t, "America/New_York")

	ctx := context.Background()
	db, err := sqlite.New(":memory:", jobs.AssetNames(), jobs.Asset, DataSourceOptions)
	require.NoError(t, err)
	p := NewSqliteProvider(db, nil, testLog)
	t.Cleanup(func() { require.NoError(t, p.Close()) })

	multi := jb.NewMulti(t).Build()
	require.NoError(t, p.SaveMultiJob(multi))

	// The later job carries the server's local zone; the earlier one carries
	// the zone an agent reported it in. Ordered by instant, "later" comes
	// first; ordered by the raw text, it does not.
	later := time.Date(2026, 9, 21, 19, 17, 30, 0, newYork) // 23:17:30 UTC
	earlier := time.Date(2026, 9, 21, 23, 17, 25, 0, time.UTC)
	require.True(t, later.After(earlier))
	require.Less(t,
		later.Format("2006-01-02 15:04:05.999999999-07:00"),
		earlier.Format("2006-01-02 15:04:05.999999999-07:00"),
		"the premise: the later instant sorts BELOW the earlier one as text")

	require.NoError(t, p.SaveJob(jb.New(t).JID("job-later").MultiJobID(multi.JID).
		StartedAt(later).FinishedAt(later).Build()))
	require.NoError(t, p.SaveJob(jb.New(t).JID("job-earlier").MultiJobID(multi.JID).
		StartedAt(earlier).FinishedAt(earlier).Build()))

	got, err := p.GetMultiJob(ctx, multi.JID)
	require.NoError(t, err)
	require.Len(t, got.Jobs, 2)
	assert.Equal(t, "job-later", got.Jobs[0].JID, "children must be newest first by instant")
	assert.Equal(t, "job-earlier", got.Jobs[1].JID)
}

// TestJobTimestampsAreStoredInUTC is the source-side half: the ordering above
// is rescued by DATETIME() for rows already on disk, but rows written from here
// on should not need rescuing.
func TestJobTimestampsAreStoredInUTC(t *testing.T) {
	newYork := withLocalZone(t, "America/New_York")

	at := time.Date(2026, 9, 21, 19, 17, 30, 0, newYork)
	job := jb.New(t).StartedAt(at).FinishedAt(at).Build()

	row := convertToSqlite(job)
	assert.Equal(t, "UTC", row.StartedAt.Location().String())
	require.True(t, row.FinishedAt.Valid)
	assert.Equal(t, "UTC", row.FinishedAt.Time.Location().String())
	assert.True(t, row.StartedAt.Equal(at), "normalising the zone must not move the instant")

	multi := jb.NewMulti(t).Build()
	multi.StartedAt = at
	assert.Equal(t, "UTC", convertMultiJobToSqlite(multi).StartedAt.Location().String())
}
