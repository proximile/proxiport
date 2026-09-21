package sqlite

import (
	"regexp"
	"strings"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/proximile/proxiport/db/migration/api_sessions"
	"github.com/proximile/proxiport/db/migration/api_token"
	"github.com/proximile/proxiport/db/migration/auditlog"
	client_groups "github.com/proximile/proxiport/db/migration/client_groups"
	"github.com/proximile/proxiport/db/migration/clients"
	"github.com/proximile/proxiport/db/migration/jobs"
	"github.com/proximile/proxiport/db/migration/library"
	"github.com/proximile/proxiport/db/migration/monitoring"
	"github.com/proximile/proxiport/db/migration/vaults"
	notificationsSQLite "github.com/proximile/proxiport/server/notifications/repository/sqlite"
)

type migrationSet struct {
	Name       string
	AssetNames []string
	Asset      func(name string) ([]byte, error)
}

// everyShippedMigrationSet is every schema the server creates at boot. Add new
// ones here; the tests below are the only thing that reads the list.
func everyShippedMigrationSet() []migrationSet {
	return []migrationSet{
		{"api_sessions", api_sessions.AssetNames(), api_sessions.Asset},
		{"api_token", api_token.AssetNames(), api_token.Asset},
		{"auditlog", auditlog.AssetNames(), auditlog.Asset},
		{"client_groups", client_groups.AssetNames(), client_groups.Asset},
		{"clients", clients.AssetNames(), clients.Asset},
		{"jobs", jobs.AssetNames(), jobs.Asset},
		{"library", library.AssetNames(), library.Asset},
		{"monitoring", monitoring.AssetNames(), monitoring.Asset},
		{"vaults", vaults.AssetNames(), vaults.Asset},
		{"notifications", notificationsSQLite.AssetNames(), notificationsSQLite.Asset},
	}
}

// TestNoShippedIndexIsBuiltOverAConstant is the general form of M2.
//
// SQLITE_DQS defaults to 3 in the amalgamation go-sqlite3 builds, so a
// double-quoted token SQLite cannot resolve to a column is silently
// reinterpreted as a string literal instead of being rejected. A CREATE INDEX
// naming a column that does not exist therefore SUCCEEDS, producing an index
// over a constant: written on every insert, usable by no query. The migration
// passes, the schema looks indexed, and every lookup on the column the index
// was meant to cover is a full table scan.
//
// The check is two-sided on purpose, because this project also ships
// deliberate functional indexes -- DATETIME(expires_at) DESC on api_sessions,
// DATETIME(disconnected_at) DESC on clients. Those are expressions too. What
// distinguishes the defect is a key that is a BARE QUOTED TOKEN which SQLite
// did not resolve to a column: PRAGMA index_xinfo reports its table column id
// as -2, and the stored DDL shows nothing but the quoted word.
func TestNoShippedIndexIsBuiltOverAConstant(t *testing.T) {
	for _, set := range everyShippedMigrationSet() {
		set := set
		t.Run(set.Name, func(t *testing.T) {
			db, err := New(":memory:", set.AssetNames, set.Asset, DataSourceOptions{})
			require.NoError(t, err)
			defer func() { require.NoError(t, db.Close()) }()

			indexes := indexKeys(t, db)
			require.NotEmpty(t, indexes, "%s declares no indexes at all; the check would be vacuous", set.Name)

			for _, index := range indexes {
				for i, cid := range index.KeyColumnIDs {
					if cid != expressionColumnID {
						continue
					}
					expr := ""
					if i < len(index.KeyExpressions) {
						expr = index.KeyExpressions[i]
					}
					assert.False(t, isBareQuotedToken(expr),
						"%s: index %q has key %d over the constant %s -- a column name SQLite could not "+
							"resolve and silently reinterpreted as a string literal. The index is maintained "+
							"on every insert and can never be used.",
						set.Name, index.Name, i, expr)
				}
			}
		})
	}
}

// expressionColumnID is what PRAGMA index_xinfo reports for a key that is an
// expression rather than a column of the table.
const expressionColumnID = -2

type indexInfo struct {
	Name           string
	KeyColumnIDs   []int
	KeyExpressions []string
}

func indexKeys(t *testing.T, db *sqlx.DB) []indexInfo {
	t.Helper()

	var tables []string
	require.NoError(t, db.Select(&tables,
		"SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'"))

	out := []indexInfo{}
	for _, table := range tables {
		var indexes []struct {
			Name string `db:"name"`
		}
		require.NoError(t, db.Select(&indexes, "SELECT name FROM pragma_index_list(?)", table))

		for _, index := range indexes {
			var ddls []string
			require.NoError(t, db.Select(&ddls,
				"SELECT COALESCE(sql, '') FROM sqlite_master WHERE type = 'index' AND name = ?", index.Name))
			if len(ddls) == 0 || ddls[0] == "" {
				// An implicit index behind UNIQUE or PRIMARY KEY: it is not in
				// sqlite_master at all, or is there with a NULL sql. Either way
				// it has no DDL of its own and cannot carry a misspelled name.
				continue
			}
			ddl := ddls[0]

			var columns []struct {
				CID int `db:"cid"`
				Key int `db:"key"`
			}
			require.NoError(t, db.Select(&columns, "SELECT cid, key FROM pragma_index_xinfo(?)", index.Name))

			info := indexInfo{Name: index.Name, KeyExpressions: splitIndexKeys(ddl)}
			for _, c := range columns {
				if c.Key == 1 {
					info.KeyColumnIDs = append(info.KeyColumnIDs, c.CID)
				}
			}
			out = append(out, info)
		}
	}
	return out
}

// splitIndexKeys returns the key expressions of a CREATE INDEX statement, in
// order, stripped of any trailing ASC/DESC/COLLATE clause.
func splitIndexKeys(ddl string) []string {
	open := strings.Index(ddl, "(")
	close := strings.LastIndex(ddl, ")")
	if open < 0 || close <= open {
		return nil
	}

	var (
		keys  []string
		cur   strings.Builder
		depth int
		quote rune
	)
	flush := func() {
		keys = append(keys, trimKeySuffix(strings.TrimSpace(cur.String())))
		cur.Reset()
	}
	for _, r := range ddl[open+1 : close] {
		switch {
		case quote != 0:
			cur.WriteRune(r)
			if r == quote {
				quote = 0
			}
		case r == '\'' || r == '"' || r == '`':
			quote = r
			cur.WriteRune(r)
		case r == '(':
			depth++
			cur.WriteRune(r)
		case r == ')':
			depth--
			cur.WriteRune(r)
		case r == ',' && depth == 0:
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return keys
}

var keySuffix = regexp.MustCompile(`(?i)\s+(asc|desc)$|\s+collate\s+\w+$`)

func trimKeySuffix(key string) string {
	for {
		trimmed := strings.TrimSpace(keySuffix.ReplaceAllString(key, ""))
		if trimmed == key {
			return key
		}
		key = trimmed
	}
}

var bareQuotedToken = regexp.MustCompile(`^("[^"]*"|'[^']*'|` + "`[^`]*`" + `)$`)

func isBareQuotedToken(key string) bool {
	return bareQuotedToken.MatchString(strings.TrimSpace(key))
}
