package usercli

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/proximile/proxiport/db/migration/api_token"
	"github.com/proximile/proxiport/db/sqlite"
	"github.com/proximile/proxiport/server/api/authorization"
	"github.com/proximile/proxiport/server/api/session"
	"github.com/proximile/proxiport/server/chconfig"
)

func newUserServiceTestConfig(t *testing.T) *chconfig.Config {
	t.Helper()
	dir := t.TempDir()
	authFile := filepath.Join(dir, "users.json")
	//nolint:gosec // G101: a bcrypt hash of a fixture password, not a credential
	require.NoError(t, os.WriteFile(authFile, []byte(`[{"username":"user1","password":"$2y$05$ep2DdPDeLDDhwRrED9q/vuVEzRpZtB5WHCFT7YbcmH9r9oNmlsZOm","groups":[]}]`), 0o600))

	cfg := &chconfig.Config{}
	cfg.Server.DataDir = dir
	cfg.API.AuthFile = authFile
	cfg.API.PasswordZxcvbnMinscore = -1
	return cfg
}

// Deleting a user from the command line deletes the API tokens the server
// keeps for that user.
func TestDeleteAlsoDeletesTheUsersAPITokens(t *testing.T) {
	cfg := newUserServiceTestConfig(t)
	dbPath := filepath.Join(cfg.Server.DataDir, "api_token.db")

	db, err := sqlite.New(dbPath, api_token.AssetNames(), api_token.Asset, cfg.Server.GetSQLiteDataSourceOptions())
	require.NoError(t, err)
	tokens := authorization.NewSqliteProvider(db)
	for _, username := range []string{"user1", "user2"} {
		require.NoError(t, tokens.Save(context.Background(), &authorization.APIToken{
			Username: username,
			Prefix:   "theprefi",
			Name:     "a token",
			Scope:    authorization.APITokenRead,
			Token:    "a-hash",
		}))
	}
	require.NoError(t, tokens.Close())

	userService, err := NewUserService(cfg)
	require.NoError(t, err)
	require.NoError(t, userService.Delete("user1"))

	db, err = sqlite.New(dbPath, api_token.AssetNames(), api_token.Asset, cfg.Server.GetSQLiteDataSourceOptions())
	require.NoError(t, err)
	tokens = authorization.NewSqliteProvider(db)
	defer func() { _ = tokens.Close() }()

	left, err := tokens.GetAll(context.Background(), "user1")
	require.NoError(t, err)
	assert.Empty(t, left)

	kept, err := tokens.GetAll(context.Background(), "user2")
	require.NoError(t, err)
	assert.Len(t, kept, 1, "another user's token must be untouched")
}

// Deleting a user from the command line deletes that user's stored sessions,
// so that a server does not load them again at its next start.
func TestDeleteAlsoDeletesTheUsersStoredSessions(t *testing.T) {
	cfg := newUserServiceTestConfig(t)
	dbPath := filepath.Join(cfg.Server.DataDir, "api_sessions.db")
	ctx := context.Background()

	sessions, err := session.NewSqliteProvider(dbPath, cfg.Server.GetSQLiteDataSourceOptions())
	require.NoError(t, err)
	for _, username := range []string{"user1", "user2"} {
		_, err := sessions.Save(ctx, session.APISession{
			Username:     username,
			ExpiresAt:    time.Now().Add(time.Hour),
			LastAccessAt: time.Now(),
		})
		require.NoError(t, err)
	}
	require.NoError(t, sessions.Close())

	userService, err := NewUserService(cfg)
	require.NoError(t, err)
	require.NoError(t, userService.Delete("user1"))

	sessions, err = session.NewSqliteProvider(dbPath, cfg.Server.GetSQLiteDataSourceOptions())
	require.NoError(t, err)
	defer func() { _ = sessions.Close() }()

	left, err := sessions.GetAll(ctx)
	require.NoError(t, err)
	require.Len(t, left, 1)
	assert.Equal(t, "user2", left[0].Username, "only the other user's session must be left")
}

// The command may run as another account than the server, so it must not
// create a database the server has not created itself.
func TestNoDatabaseIsCreated(t *testing.T) {
	cfg := newUserServiceTestConfig(t)

	userService, err := NewUserService(cfg)
	require.NoError(t, err)
	require.NoError(t, userService.Delete("user1"))

	for _, name := range []string{"api_token.db", "api_sessions.db"} {
		_, err = os.Stat(filepath.Join(cfg.Server.DataDir, name))
		assert.ErrorIs(t, err, os.ErrNotExist, name)
	}
}
