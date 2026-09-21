package sqlite_test

import (
	"context"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/proximile/proxiport/db/sqlite"
	"github.com/proximile/proxiport/server/notifications"
	repo "github.com/proximile/proxiport/server/notifications/repository/sqlite"
	"github.com/proximile/proxiport/share/query"
)

// TestEveryAdvertisedNotificationFilterAndSortIsUsable is M4. Both List and
// Count started from a base query that already ended in ORDER BY, and the query
// builder appends " WHERE ..." and " ORDER BY ..." by string concatenation with
// no notion of clause position. So every documented sort and filter on
// /api/v1/notification-logs produced a statement SQLite refuses to prepare, and
// the endpoint worked only when neither was supplied -- which is the one case
// the existing tests covered.
//
// The table is notifications.SupportedFilters / SupportedSorts itself, so this
// cannot drift from what the API advertises.
func TestEveryAdvertisedNotificationFilterAndSortIsUsable(t *testing.T) {
	ctx := context.Background()

	db, err := sqlite.New(":memory:", repo.AssetNames(), repo.Asset, sqlite.DataSourceOptions{})
	require.NoError(t, err)
	repository := repo.NewRepository(db, testLog)

	// Inserted directly, with explicit timestamps: the repository's own write
	// path stamps CURRENT_TIMESTAMP, whose one-second resolution would make
	// two rows created in the same test indistinguishable -- and the ordering
	// assertion below needs them to be distinguishable.
	insert := func(id, state, ts string) {
		t.Helper()
		_, err := db.Exec(
			"INSERT INTO notifications_log (notification_id, timestamp, state, transport, subject, out, err)"+
				" VALUES (?, ?, ?, 'smtp', 'test-subject', '', '')",
			id, ts, state,
		)
		require.NoError(t, err)
	}
	insert("01OLDER0000000000000000000", "done", "2026-09-15 10:00:00")
	insert("01NEWER0000000000000000000", "error", "2026-09-15 12:00:00")

	t.Run("filters", func(t *testing.T) {
		for _, key := range notificationFilterQueryKeys() {
			key := key
			t.Run(key, func(t *testing.T) {
				options := notificationOptions(t, key+"=2026-09-15T11:00:00%2B00:00")

				_, err := repository.List(ctx, options)
				require.NoError(t, err, "List must accept the advertised filter %s", key)

				_, err = repository.Count(ctx, options)
				require.NoError(t, err, "Count must accept the advertised filter %s", key)
			})
		}
	})

	t.Run("sorts", func(t *testing.T) {
		for field := range notifications.SupportedSorts {
			field := field
			for _, param := range []string{field, "-" + field} {
				param := param
				t.Run(param, func(t *testing.T) {
					options := notificationOptions(t, "sort="+param)

					_, err := repository.List(ctx, options)
					require.NoError(t, err, "List must accept the advertised sort %s", param)

					_, err = repository.Count(ctx, options)
					require.NoError(t, err, "Count must accept the advertised sort %s", param)
				})
			}
		}
	})

	// The base query's ORDER BY is gone, so the newest-first default has to
	// come from somewhere else. It does -- but only when the caller did not ask
	// for an order of their own.
	t.Run("default order is newest first", func(t *testing.T) {
		items, err := repository.List(ctx, nil)
		require.NoError(t, err)
		require.Len(t, items, 2)
		require.Equal(t, "01NEWER0000000000000000000", items[0].NotificationID)

		ascending := notificationOptions(t, "sort=timestamp")
		items, err = repository.List(ctx, ascending)
		require.NoError(t, err)
		require.Len(t, items, 2)
		require.Equal(t, "01OLDER0000000000000000000", items[0].NotificationID,
			"an explicit sort must replace the default, not be appended after it")
	})

	t.Run("a filter actually filters", func(t *testing.T) {
		options := notificationOptions(t, "filter[state]=error")

		items, err := repository.List(ctx, options)
		require.NoError(t, err)
		require.Len(t, items, 1)
		require.Equal(t, "01NEWER0000000000000000000", items[0].NotificationID)

		count, err := repository.Count(ctx, options)
		require.NoError(t, err)
		require.Equal(t, 1, count)
	})
}

// notificationFilterQueryKeys turns the supported-filter map into the
// query-string keys an operator would send: "timestamp[gt]" is the filter key
// filter[timestamp][gt], not filter[timestamp[gt]].
func notificationFilterQueryKeys() []string {
	keys := make([]string, 0, len(notifications.SupportedFilters))
	for field := range notifications.SupportedFilters {
		if i := strings.Index(field, "["); i >= 0 {
			keys = append(keys, "filter["+field[:i]+"]["+strings.TrimSuffix(field[i+1:], "]")+"]")
			continue
		}
		keys = append(keys, "filter["+field+"]")
	}
	sort.Strings(keys)
	return keys
}

func notificationOptions(t *testing.T, rawQuery string) *query.ListOptions {
	t.Helper()

	req := httptest.NewRequest("GET", "/api/v1/notification-logs?"+rawQuery, nil)
	options := query.NewOptions(req, nil, nil, nil)

	require.NoError(t, query.ValidateListOptions(
		options,
		notifications.SupportedSorts,
		notifications.SupportedFilters,
		nil,
		&query.PaginationConfig{DefaultLimit: 10, MaxLimit: 100},
	), "the API already accepts %q; if validation rejects it the advertisement is wrong", rawQuery)

	return options
}
