package bearer

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/proximile/proxiport/db/sqlite"
	"github.com/proximile/proxiport/server/api/session"
	"github.com/proximile/proxiport/share/logger"
)

var bindingTestLog = logger.NewLogger("bearer-test", logger.LogOutput{File: nil}, logger.LogLevelError)

func newSessionStore(t *testing.T) *session.SqliteProvider {
	t.Helper()
	p, err := session.NewSqliteProvider(":memory:", sqlite.DataSourceOptions{WALEnabled: false})
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })
	return p
}

func login(t *testing.T, store *session.SqliteProvider, username string) *TokenContext {
	t.Helper()
	tok, err := CreateAuthToken(context.Background(), store, testSecret, time.Hour, username, nil, "ua", "192.0.2.10")
	require.NoError(t, err)
	tokCtx, err := ParseToken(tok, testSecret)
	require.NoError(t, err)
	return tokCtx
}

func tokenIsValid(t *testing.T, store *session.SqliteProvider, tokCtx *TokenContext) bool {
	t.Helper()
	valid, _, err := ValidateBearerToken(context.Background(), tokCtx, "/api/v1/me", "GET", store, bindingTestLog)
	require.NoError(t, err)
	return valid
}

// A token whose session was deleted stays dead when another user logs in
// afterwards, whatever id the new session gets.
func TestRevokedTokenStaysRevokedAfterAnotherUsersLogin(t *testing.T) {
	store := newSessionStore(t)

	alice := login(t, store, "alice")
	require.True(t, tokenIsValid(t, store, alice))

	require.NoError(t, store.DeleteAllByUser(context.Background(), "alice"))
	require.False(t, tokenIsValid(t, store, alice))

	bob := login(t, store, "bob")
	require.True(t, tokenIsValid(t, store, bob))
	assert.False(t, tokenIsValid(t, store, alice), "alice's revoked token is valid again after bob's login")
}

// The same holds when the next login is by the same user: the new session
// must not bring the old token back.
func TestRevokedTokenStaysRevokedAfterTheSameUsersLogin(t *testing.T) {
	store := newSessionStore(t)

	stolen := login(t, store, "alice")
	require.NoError(t, store.DeleteAllByUser(context.Background(), "alice"))

	fresh := login(t, store, "alice")
	require.True(t, tokenIsValid(t, store, fresh))
	assert.False(t, tokenIsValid(t, store, stolen), "the revoked token is valid again after its user logged in anew")
}

// A token is bound to the session it was issued for. One that names another
// user's live session, or carries another token's id, is refused.
func TestTokenMustMatchItsSession(t *testing.T) {
	store := newSessionStore(t)
	alice := login(t, store, "alice")
	bob := login(t, store, "bob")

	wrongUser := *alice.AppClaims
	wrongUser.SessionID = bob.AppClaims.SessionID
	assert.False(t, tokenIsValid(t, store, &TokenContext{AppClaims: &wrongUser, JwtToken: alice.JwtToken}),
		"a token for alice is accepted against bob's session")

	wrongID := *alice.AppClaims
	wrongID.Id = bob.AppClaims.Id
	assert.False(t, tokenIsValid(t, store, &TokenContext{AppClaims: &wrongID, JwtToken: alice.JwtToken}),
		"a token is accepted with another token's id")

	noID := *alice.AppClaims
	noID.Id = ""
	assert.False(t, tokenIsValid(t, store, &TokenContext{AppClaims: &noID, JwtToken: alice.JwtToken}),
		"a token without an id is accepted")

	assert.True(t, tokenIsValid(t, store, alice))
	assert.True(t, tokenIsValid(t, store, bob))
}

// Token ids come from the system's random source and are long enough not to
// be guessed.
func TestTokenIDsAreLongAndDistinct(t *testing.T) {
	store := newSessionStore(t)
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		id := login(t, store, "alice").AppClaims.Id
		require.GreaterOrEqual(t, len(id), 32)
		require.False(t, seen[id], "token id issued twice")
		seen[id] = true
	}
}

// Session ids are never issued twice, also after the newest session was
// deleted.
func TestSessionIDsAreNotReused(t *testing.T) {
	store := newSessionStore(t)
	first := login(t, store, "alice").AppClaims.SessionID
	require.NoError(t, store.Delete(context.Background(), first))
	second := login(t, store, "bob").AppClaims.SessionID
	assert.Greater(t, second, first)
}

// Extending a session keeps its token valid.
func TestSessionStaysValidAfterItsLifetimeIsExtended(t *testing.T) {
	store := newSessionStore(t)
	alice := login(t, store, "alice")
	_, info, err := ValidateBearerToken(context.Background(), alice, "/api/v1/me", "GET", store, bindingTestLog)
	require.NoError(t, err)
	require.NoError(t, IncreaseSessionLifetime(context.Background(), store, info))
	assert.True(t, tokenIsValid(t, store, alice))
}
