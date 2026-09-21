package jobs

import (
	"context"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/proximile/proxiport/db/migration/jobs"
	"github.com/proximile/proxiport/db/sqlite"
	"github.com/proximile/proxiport/server/test/jb"
	"github.com/proximile/proxiport/share/query"
)

// newJobsProviderWithRows builds the same schema the server runs on, with one
// job that belongs to a multi-job (so the LEFT JOIN has a matching row) and one
// that does not.
func newJobsProviderWithRows(t *testing.T) *SqliteProvider {
	t.Helper()

	jobsDB, err := sqlite.New(":memory:", jobs.AssetNames(), jobs.Asset, DataSourceOptions)
	require.NoError(t, err)

	p := NewSqliteProvider(jobsDB, nil, testLog)
	t.Cleanup(func() { require.NoError(t, p.Close()) })

	multi := jb.NewMulti(t).Build()
	require.NoError(t, p.SaveMultiJob(multi))

	finished := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	require.NoError(t, p.SaveJob(jb.New(t).
		MultiJobID(multi.JID).
		StartedAt(time.Date(2026, 9, 15, 11, 0, 0, 0, time.UTC)).
		FinishedAt(finished).
		Build()))
	require.NoError(t, p.SaveJob(jb.New(t).
		StartedAt(time.Date(2026, 9, 15, 11, 30, 0, 0, time.UTC)).
		FinishedAt(finished).
		Build()))

	return p
}

// filterQueryKeys turns a supported-filter map into the query-string keys an
// operator would actually send: "started_at[gt]" is the filter key
// filter[started_at][gt], not filter[started_at[gt]].
func filterQueryKeys(supported map[string]bool) []string {
	keys := make([]string, 0, len(supported))
	for field := range supported {
		if i := strings.Index(field, "["); i >= 0 {
			keys = append(keys, "filter["+field[:i]+"]["+strings.TrimSuffix(field[i+1:], "]")+"]")
			continue
		}
		keys = append(keys, "filter["+field+"]")
	}
	sort.Strings(keys)
	return keys
}

// TestEveryAdvertisedJobFilterAndSortIsUsable is M3. The job listing joins jobs
// to multi_jobs, and jid, started_at and created_by exist in both tables, so
// against the unfixed tree every one of those filters made SQLite refuse the
// statement with "ambiguous column name" and the handler answered HTTP 500.
// Count wrapped the same join in a subquery and worked, which is why the two
// disagreed unnoticed -- so both are exercised here, for every filter and sort
// the API advertises.
func TestEveryAdvertisedJobFilterAndSortIsUsable(t *testing.T) {
	ctx := context.Background()
	p := newJobsProviderWithRows(t)

	t.Run("filters", func(t *testing.T) {
		for _, key := range filterQueryKeys(JobSupportedFilters) {
			key := key
			t.Run(key, func(t *testing.T) {
				options := optionsFromQuery(t, key+"=2026-09-15T11:00:00%2B00:00")

				_, err := p.List(ctx, options)
				require.NoError(t, err, "List must accept the advertised filter %s", key)

				_, err = p.Count(ctx, options)
				require.NoError(t, err, "Count must accept the advertised filter %s", key)
			})
		}
	})

	t.Run("sorts", func(t *testing.T) {
		for field := range JobSupportedSorts {
			field := field
			for _, param := range []string{field, "-" + field} {
				param := param
				t.Run(param, func(t *testing.T) {
					options := optionsFromQuery(t, "sort="+param)

					_, err := p.List(ctx, options)
					require.NoError(t, err, "List must accept the advertised sort %s", param)

					_, err = p.Count(ctx, options)
					require.NoError(t, err, "Count must accept the advertised sort %s", param)
				})
			}
		}
	})

	// L2: filter[*] used to expand to every key of the map verbatim, so the
	// bracketed range keys went into the SQL as column names and SQLite read
	// "started_at[gt]" as the identifier started_at followed by the
	// bracket-quoted identifier [gt].
	t.Run("filter[*]", func(t *testing.T) {
		options := optionsFromQuery(t, "filter[*]=nothing-matches-this")

		for _, fo := range options.Filters {
			for _, col := range fo.Column {
				require.NotContains(t, col, "[", "a wildcard column must be a plain column name")
			}
		}

		got, err := p.List(ctx, options)
		require.NoError(t, err)
		require.Empty(t, got)

		count, err := p.Count(ctx, options)
		require.NoError(t, err)
		require.Zero(t, count)
	})
}

func optionsFromQuery(t *testing.T, rawQuery string) *query.ListOptions {
	t.Helper()

	req := httptest.NewRequest("GET", "/api/v1/clients/c1/commands?"+rawQuery, nil)
	options := query.NewOptions(req, nil, nil, JobListDefaultFields)

	require.NoError(t, query.ValidateListOptions(
		options,
		JobSupportedSorts,
		JobSupportedFilters,
		JobSupportedFields,
		&query.PaginationConfig{MaxLimit: MaxLimit, DefaultLimit: DefaultLimit},
	), "the API already accepts %q; if validation rejects it the advertisement is wrong", rawQuery)

	return options
}
