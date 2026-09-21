package monitoring

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/proximile/proxiport/share/models"
	"github.com/proximile/proxiport/share/query"
)

// withLocalZone runs fn with the process's local zone set to loc.
//
// time.Local is a package-level variable and the defect under test is
// specifically that time.Unix and time.Now return times in it, so there is no
// way to exercise this without setting it. That makes these tests
// non-parallel, which is why they say so out loud rather than calling
// t.Parallel().
func withLocalZone(t *testing.T, name string, fn func(loc *time.Location)) {
	t.Helper()

	loc, err := time.LoadLocation(name)
	require.NoError(t, err)

	original := time.Local
	time.Local = loc
	t.Cleanup(func() { time.Local = original })

	fn(loc)
}

// TestTimestampFiltersAreNormalisedToUTC is M10. Measurements are stamped
// time.Now().UTC() and stored as text with a +00:00 suffix, but the filter
// conversion rendered the caller's value in the SERVER's local wall clock and
// dropped the zone -- so on any non-UTC host the comparison was off by the
// host's UTC offset. For the SPA's +/-60s process lookup that means it matched
// nothing at all, on every client, forever, with HTTP 200 and no error.
func TestTimestampFiltersAreNormalisedToUTC(t *testing.T) {
	instant := time.Date(2026, 9, 15, 12, 30, 0, 0, time.UTC)
	const wantDbText = "2026-09-15 12:30:00"

	for _, zone := range []string{"UTC", "Europe/Berlin", "America/New_York"} {
		zone := zone
		t.Run(zone, func(t *testing.T) {
			withLocalZone(t, zone, func(*time.Location) {
				t.Run("epoch gt/lt", func(t *testing.T) {
					filters := []query.FilterOption{{
						Column:   []string{"timestamp"},
						Operator: query.FilterOperatorTypeGT,
						Values:   []string{formatEpoch(instant)},
					}}
					require.NoError(t, parseAndConvertFilterValues(filters))
					assert.Equal(t, wantDbText, filters[0].Values[0])
				})

				t.Run("RFC3339 since/until keeps the instant, not the wall clock", func(t *testing.T) {
					// The same instant, written by the caller with a +01:00
					// offset -- the form the shipped OpenAPI examples use.
					offset := time.FixedZone("UTC+1", 3600)
					filters := []query.FilterOption{{
						Column:   []string{"timestamp"},
						Operator: query.FilterOperatorTypeSince,
						Values:   []string{instant.In(offset).Format(time.RFC3339)},
					}}
					require.NoError(t, parseAndConvertFilterValues(filters))
					assert.Equal(t, wantDbText, filters[0].Values[0])
				})
			})
		})
	}
}

// TestProcessesLookupFindsTheMeasurementItPointsAt is the same defect end to
// end: the window the SPA sends around a charted point must contain the
// measurement that point came from, whatever zone the server runs in.
func TestProcessesLookupFindsTheMeasurementItPointsAt(t *testing.T) {
	for _, zone := range []string{"UTC", "Europe/Berlin", "America/New_York"} {
		zone := zone
		t.Run(zone, func(t *testing.T) {
			withLocalZone(t, zone, func(*time.Location) {
				ctx := context.Background()
				dbProvider, err := NewSqliteProvider(":memory:", DataSourceOptions, nil, testLog)
				require.NoError(t, err)
				defer func() { require.NoError(t, dbProvider.Close()) }()

				at := time.Date(2026, 9, 15, 12, 30, 0, 0, time.UTC)
				require.NoError(t, dbProvider.CreateMeasurement(ctx, &models.Measurement{
					ClientID:  "test_client_1",
					Timestamp: at,
					Processes: `[{"pid":1,"name":"init"}]`,
				}))

				// Exactly what the monitoring page sends when an operator
				// clicks a point on the chart.
				filters := []query.FilterOption{
					{Column: []string{"timestamp"}, Operator: query.FilterOperatorTypeGT,
						Values: []string{formatEpoch(at.Add(-time.Minute))}},
					{Column: []string{"timestamp"}, Operator: query.FilterOperatorTypeLT,
						Values: []string{formatEpoch(at.Add(time.Minute))}},
				}
				require.NoError(t, parseAndConvertFilterValues(filters))

				options := createProcessesDefaultOptions()
				options.Filters = filters

				processes, err := dbProvider.ListProcessesByClientID(ctx, "test_client_1", options)
				require.NoError(t, err)
				require.Len(t, processes, 1, "the window around a charted point must contain that point")
			})
		})
	}
}

// TestRetentionThresholdIsUTC is L9. Measurements are stored UTC; the retention
// threshold was built from time.Now() in the host's zone and compared as text,
// so the cutoff moved by the host's UTC offset -- deleting hours of data early
// west of UTC, keeping hours past the window east of it.
func TestRetentionThresholdIsUTC(t *testing.T) {
	// Europe/Berlin is UTC+2 in September, so the buggy threshold sits two
	// hours in the future relative to the stored text: a 23h-old measurement
	// falls inside a 24h retention window but outside the shifted one.
	withLocalZone(t, "Europe/Berlin", func(*time.Location) {
		ctx := context.Background()
		dbProvider, err := NewSqliteProvider(":memory:", DataSourceOptions, nil, testLog)
		require.NoError(t, err)
		defer func() { require.NoError(t, dbProvider.Close()) }()

		service := NewService(dbProvider, testLog)

		keep := time.Now().UTC().Add(-23 * time.Hour)
		drop := time.Now().UTC().Add(-25 * time.Hour)
		for _, ts := range []time.Time{keep, drop} {
			require.NoError(t, dbProvider.CreateMeasurement(ctx, &models.Measurement{
				ClientID:  "test_client_1",
				Timestamp: ts,
				Processes: `[]`,
			}))
		}

		deleted, err := service.DeleteMeasurementsOlderThan(ctx, 24*time.Hour)
		require.NoError(t, err)
		assert.Equal(t, int64(1), deleted, "only the measurement past the retention window may be deleted")

		count, err := dbProvider.CountByClientID(ctx, "test_client_1", &query.ListOptions{})
		require.NoError(t, err)
		assert.Equal(t, 1, count)
	})
}

func formatEpoch(t time.Time) string {
	return strconv.FormatInt(t.Unix(), 10)
}
