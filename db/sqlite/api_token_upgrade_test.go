package sqlite

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/proximile/proxiport/db/migration/api_token"
)

// apiTokenAssetsUpTo returns the api_token migrations up to and including
// the given version, which is how a database of an older installation is
// built here before the full set is applied to it.
func apiTokenAssetsUpTo(t *testing.T, version string) []string {
	t.Helper()
	var names []string
	for _, name := range api_token.AssetNames() {
		if name[:3] <= version {
			names = append(names, name)
		}
	}
	require.NotEmpty(t, names)
	return names
}

type storedToken struct {
	Username string `db:"username"`
	Prefix   string `db:"prefix"`
	Scope    string `db:"scope"`
	Token    string `db:"token"`
	Name     string `db:"name"`
}

func upgradedTokens(t *testing.T, dbPath string) []storedToken {
	t.Helper()
	db, err := New(dbPath, api_token.AssetNames(), api_token.Asset, DataSourceOptions{})
	require.NoError(t, err, "the upgrade to the current schema failed")
	defer func() { require.NoError(t, db.Close()) }()

	var tokens []storedToken
	require.NoError(t, db.Select(&tokens,
		"SELECT username, prefix, scope, token, name FROM api_tokens ORDER BY username, prefix"))

	// The unique index on (username, name) is what the server relies on.
	var indexes []string
	require.NoError(t, db.Select(&indexes,
		"SELECT name FROM sqlite_master WHERE type = 'index' AND name = 'api_tokens_unique_name'"))
	assert.Len(t, indexes, 1)
	return tokens
}

// A datastore that holds API tokens in the first schema, from before tokens
// had names, upgrades with every token kept. Each token gets its prefix as
// its name, which is unique per user as the name has to be.
func TestAPITokensSurviveTheUpgradeFromTheFirstSchema(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "api_token.db")

	old, err := New(dbPath, apiTokenAssetsUpTo(t, "001"), api_token.Asset, DataSourceOptions{})
	require.NoError(t, err)
	for _, row := range [][]string{
		{"user1", "aaaaaaaa", "read", "hash-1"},
		{"user1", "bbbbbbbb", "read+write", "hash-2"},
		{"user2", "cccccccc", "read", "hash-3"},
	} {
		_, err = old.Exec("INSERT INTO api_token (username, prefix, scope, token) VALUES (?, ?, ?, ?)", row[0], row[1], row[2], row[3])
		require.NoError(t, err)
	}
	require.NoError(t, old.Close())

	assert.Equal(t, []storedToken{
		{"user1", "aaaaaaaa", "read", "hash-1", "aaaaaaaa"},
		{"user1", "bbbbbbbb", "read+write", "hash-2", "bbbbbbbb"},
		{"user2", "cccccccc", "read", "hash-3", "cccccccc"},
	}, upgradedTokens(t, dbPath))
}

// A datastore that already has named tokens, in the second schema, upgrades
// with the tokens and their names kept.
func TestAPITokensSurviveTheUpgradeFromTheSecondSchema(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "api_token.db")

	old, err := New(dbPath, apiTokenAssetsUpTo(t, "002"), api_token.Asset, DataSourceOptions{})
	require.NoError(t, err)
	for _, row := range [][]string{
		{"user1", "aaaaaaaa", "read", "hash-1", "backup job"},
		{"user1", "bbbbbbbb", "read+write", "hash-2", "deploy"},
		{"user2", "cccccccc", "read", "hash-3", "backup job"},
	} {
		_, err = old.Exec("INSERT INTO api_tokens (username, prefix, scope, token, name) VALUES (?, ?, ?, ?, ?)", row[0], row[1], row[2], row[3], row[4])
		require.NoError(t, err)
	}
	require.NoError(t, old.Close())

	assert.Equal(t, []storedToken{
		{"user1", "aaaaaaaa", "read", "hash-1", "backup job"},
		{"user1", "bbbbbbbb", "read+write", "hash-2", "deploy"},
		{"user2", "cccccccc", "read", "hash-3", "backup job"},
	}, upgradedTokens(t, dbPath))
}

// A new installation ends with the same table as before: same columns, same
// primary key, same unique index.
func TestAPITokenSchemaOfANewInstallation(t *testing.T) {
	db, err := New(filepath.Join(t.TempDir(), "api_token.db"), api_token.AssetNames(), api_token.Asset, DataSourceOptions{})
	require.NoError(t, err)
	defer func() { require.NoError(t, db.Close()) }()

	_, err = db.Exec("INSERT INTO api_tokens (username, prefix, scope, token, name) VALUES ('user1', 'aaaaaaaa', 'read', 'hash-1', 'one')")
	require.NoError(t, err)
	_, err = db.Exec("INSERT INTO api_tokens (username, prefix, scope, token, name) VALUES ('user1', 'aaaaaaaa', 'read', 'hash-2', 'two')")
	assert.Error(t, err, "two tokens of one user with the same prefix")
	_, err = db.Exec("INSERT INTO api_tokens (username, prefix, scope, token, name) VALUES ('user1', 'bbbbbbbb', 'read', 'hash-2', 'one')")
	assert.Error(t, err, "two tokens of one user with the same name")
	_, err = db.Exec("INSERT INTO api_tokens (username, prefix, scope, token) VALUES ('user1', 'cccccccc', 'read', 'hash-3')")
	assert.Error(t, err, "a token without a name")

	assertColumns(t, db, "api_tokens", "username", "prefix", "created_at", "expires_at", "scope", "token", "name")
}

func assertColumns(t *testing.T, db *sqlx.DB, table string, want ...string) {
	t.Helper()
	var got []string
	require.NoError(t, db.Select(&got, "SELECT name FROM pragma_table_info(?)", table))
	assert.ElementsMatch(t, want, got)
}

// Every migration set is embedded from its SQL files by a generator. The
// embedded copy is what runs; a SQL file edited without regenerating it
// changes nothing. This compares the two for every set the server ships.
func TestEmbeddedMigrationsMatchTheirSQLFiles(t *testing.T) {
	dirs := map[string]string{
		"notifications": filepath.Join("..", "..", "server", "notifications", "repository", "sqlite", "migrations"),
	}
	for _, set := range everyShippedMigrationSet() {
		t.Run(set.Name, func(t *testing.T) {
			dir, ok := dirs[set.Name]
			if !ok {
				dir = filepath.Join("..", "migration", set.Name, "sql")
			}
			entries, err := os.ReadDir(dir)
			require.NoError(t, err)

			var onDisk []string
			for _, e := range entries {
				if strings.HasSuffix(e.Name(), ".sql") {
					onDisk = append(onDisk, e.Name())
				}
			}
			assert.ElementsMatch(t, onDisk, set.AssetNames, "the embedded set and the SQL files differ")

			for _, name := range onDisk {
				want, err := os.ReadFile(filepath.Join(dir, name)) //nolint:gosec // G304: a migration file of this repository
				require.NoError(t, err)
				got, err := set.Asset(name)
				if assert.NoError(t, err, name) {
					assert.Equal(t, string(want), string(got), "%s: the embedded copy differs from the file; regenerate it", name)
				}
			}
		})
	}
}
