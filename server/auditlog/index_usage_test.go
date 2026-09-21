package auditlog

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/proximile/proxiport/db/migration/auditlog"
	"github.com/proximile/proxiport/db/sqlite"
	"github.com/proximile/proxiport/server/api/users"
	"github.com/proximile/proxiport/share/query"
)

// TestNonAdminAuditListingUsesTheUsernameIndex is M2.
//
// AuditLog.List force-appends `username = <caller>` to every request from a
// non-admin, and runs it twice -- once for the page and once for the count.
// The index meant to serve that predicate was created over "client_username",
// a column this table has never had, so SQLite turned it into an index over a
// constant and both queries scanned the whole audit database. That database
// holds a month of every API and agent action and is opened with a single
// connection, which is also the connection the audit WRITE path needs.
func TestNonAdminAuditListingUsesTheUsernameIndex(t *testing.T) {
	db, err := sqlite.New(":memory:", auditlog.AssetNames(), auditlog.Asset, DataSourceOptions)
	require.NoError(t, err)
	defer func() { require.NoError(t, db.Close()) }()

	provider := &SQLiteProvider{db: db, converter: query.NewSQLConverter(db.DriverName())}

	req := httptest.NewRequest("GET", "/auditlog?sort=-timestamp&page[limit]=10", nil)
	nonAdmin := &users.User{Username: "bob", Groups: []string{"Operators"}}

	options, err := listOptionsFor(req, nonAdmin)
	require.NoError(t, err)
	require.NotEmpty(t, options.Filters, "a non-admin listing must carry the forced username filter")

	for _, tc := range []struct {
		name  string
		build func(*query.ListOptions) (string, []interface{})
	}{
		{"List", provider.listQuery},
		{"Count", provider.countQuery},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			q, params := tc.build(options)
			plan := explainQueryPlan(t, db, q, params)

			assert.Contains(t, plan, "SEARCH", "plan was:\n%s\nfor:\n%s", plan, q)
			assert.Contains(t, plan, "auditlog_username_timestamp", "plan was:\n%s\nfor:\n%s", plan, q)
			assert.NotContains(t, plan, "SCAN auditlog", "plan was:\n%s\nfor:\n%s", plan, q)
		})
	}
}

// TestAdminAuditListingStillOrdersByTimestampIndex checks the new composite
// index did not cost the admin listing its ordering index -- an admin sends no
// username filter, so the timestamp index is all it has.
func TestAdminAuditListingStillOrdersByTimestampIndex(t *testing.T) {
	db, err := sqlite.New(":memory:", auditlog.AssetNames(), auditlog.Asset, DataSourceOptions)
	require.NoError(t, err)
	defer func() { require.NoError(t, db.Close()) }()

	provider := &SQLiteProvider{db: db, converter: query.NewSQLConverter(db.DriverName())}

	req := httptest.NewRequest("GET", "/auditlog?sort=-timestamp&page[limit]=10", nil)
	admin := &users.User{Username: "root", Groups: []string{users.Administrators}}

	options, err := listOptionsFor(req, admin)
	require.NoError(t, err)
	assert.Empty(t, options.Filters, "an admin listing carries no forced filter")

	q, params := provider.listQuery(options)
	plan := explainQueryPlan(t, db, q, params)
	assert.Contains(t, plan, "USING INDEX", "plan was:\n%s\nfor:\n%s", plan, q)
}

func explainQueryPlan(t *testing.T, db *sqlx.DB, q string, params []interface{}) string {
	t.Helper()

	// EXPLAIN QUERY PLAN returns (id, parent, notused, detail).
	var rows []struct {
		ID      int    `db:"id"`
		Parent  int    `db:"parent"`
		NotUsed int    `db:"notused"`
		Detail  string `db:"detail"`
	}
	require.NoError(t, db.Select(&rows, "EXPLAIN QUERY PLAN "+q, params...))
	require.NotEmpty(t, rows, "EXPLAIN QUERY PLAN returned nothing for:\n%s", q)

	details := make([]string, 0, len(rows))
	for _, r := range rows {
		details = append(details, r.Detail)
	}
	return strings.Join(details, "\n")
}

// TestIndexFixAppliesToAnExistingDatabase covers the upgrade path rather than a
// fresh install: every deployment that has ever run this server already has the
// two constant indexes on disk, so the migration has to drop and replace them
// in place, not merely create the right ones from scratch.
func TestIndexFixAppliesToAnExistingDatabase(t *testing.T) {
	dbPath := t.TempDir() + "/auditlog.db"

	// Bring the database up to the schema a deployed server already has: 001
	// and 002 only, with the two constant indexes in place.
	beforeFix := []string{}
	for _, name := range auditlog.AssetNames() {
		if strings.HasPrefix(name, "003_") {
			continue
		}
		beforeFix = append(beforeFix, name)
	}
	require.Len(t, beforeFix, len(auditlog.AssetNames())-2)

	old, err := sqlite.New(dbPath, beforeFix, auditlog.Asset, DataSourceOptions)
	require.NoError(t, err)

	var oldIndexSQL []string
	require.NoError(t, old.Select(&oldIndexSQL,
		"SELECT sql FROM sqlite_master WHERE type = 'index' AND name = 'auditlog_username'"))
	require.Len(t, oldIndexSQL, 1)
	require.Contains(t, oldIndexSQL[0], "client_username", "the pre-fix schema should carry the broken index")
	require.NoError(t, old.Close())

	// Now open the same file with the full migration set, as an upgraded server
	// would.
	upgraded, err := sqlite.New(dbPath, auditlog.AssetNames(), auditlog.Asset, DataSourceOptions)
	require.NoError(t, err)
	defer func() { require.NoError(t, upgraded.Close()) }()

	var remaining []string
	require.NoError(t, upgraded.Select(&remaining,
		"SELECT name FROM sqlite_master WHERE type = 'index' AND name = 'auditlog_username'"))
	assert.Empty(t, remaining, "the constant index must be gone after the upgrade")

	provider := &SQLiteProvider{db: upgraded, converter: query.NewSQLConverter(upgraded.DriverName())}
	req := httptest.NewRequest("GET", "/auditlog?sort=-timestamp&page[limit]=10", nil)
	options, err := listOptionsFor(req, &users.User{Username: "bob", Groups: []string{"Operators"}})
	require.NoError(t, err)

	q, params := provider.listQuery(options)
	plan := explainQueryPlan(t, upgraded, q, params)
	assert.Contains(t, plan, "SEARCH")
	assert.Contains(t, plan, "auditlog_username_timestamp", "plan was:\n%s", plan)
}
