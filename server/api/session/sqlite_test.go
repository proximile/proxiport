package session

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/proximile/proxiport/db/migration/api_sessions"
	"github.com/proximile/proxiport/db/sqlite"
	_ "github.com/proximile/proxiport/share/test/utcprocess"

	"github.com/golang-jwt/jwt/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var DataSourceOptions = sqlite.DataSourceOptions{WALEnabled: false}

func TestAPISessionSqlite(t *testing.T) {
	ctx := context.Background()

	longTTL := time.Hour
	negativeTTL := -time.Minute

	timeNow := time.Now().UTC()

	s1 := generateAPISession(t, "user1", timeNow.Add(longTTL), timeNow)
	s2 := generateAPISession(t, "user1", timeNow.Add(longTTL), timeNow)
	s3 := generateAPISession(t, "user1", timeNow.Add(longTTL), timeNow)
	s4Expired := generateAPISession(t, "user1", timeNow.Add(negativeTTL), timeNow)
	s5Expired := generateAPISession(t, "user1", timeNow.Add(negativeTTL), timeNow)

	// check create
	p := newInmemoryDB(t, &s1, &s2, &s3, &s4Expired, &s5Expired)

	s5Updated := APISession{
		SessionID: s5Expired.SessionID,
		ExpiresAt: timeNow.Add(longTTL),
	}

	// check get all(unexpired)
	gotAll1, err := p.GetAll(ctx)

	require.NoError(t, err)
	require.NotEmpty(t, gotAll1)

	assert.Equal(t, s1, gotAll1[0])

	assert.ElementsMatch(t, []APISession{s1, s2, s3}, gotAll1)

	// check expired are in DB
	_, gotExpiredS4, err := p.Get(ctx, s4Expired.SessionID)
	require.NoError(t, err)
	assert.EqualValues(t, s4Expired, gotExpiredS4)
	_, gotExpiredS5, err := p.Get(ctx, s5Expired.SessionID)
	require.NoError(t, err)
	assert.EqualValues(t, s5Expired, gotExpiredS5)

	// check updated
	_, err = p.Save(ctx, s5Updated)
	require.NoError(t, err)

	// check get all(unexpired)
	gotAll2, err := p.GetAll(ctx)
	require.NoError(t, err)
	assert.ElementsMatch(t, []APISession{s1, s2, s3, s5Updated}, gotAll2)

	// check expired is in DB
	_, gotExpired, err := p.Get(ctx, s4Expired.SessionID)
	require.NoError(t, err)
	assert.EqualValues(t, s4Expired, gotExpired)

	// check updated
	_, gotUpdated, err := p.Get(ctx, s5Expired.SessionID)
	require.NoError(t, err)

	assert.EqualValues(t, s5Updated, gotUpdated)

	// check delete by token
	err = p.Delete(ctx, s2.SessionID)
	require.NoError(t, err)
	gotAll3, err := p.GetAll(ctx)
	require.NoError(t, err)
	assert.ElementsMatch(t, []APISession{s1, s3, s5Updated}, gotAll3)

	// make sure expired is in DB
	found, gotExpired2, err := p.Get(ctx, s4Expired.SessionID)
	require.NoError(t, err)
	require.True(t, found)

	assert.EqualValues(t, s4Expired, gotExpired2)

	// check delete all expired
	err = p.DeleteExpired(ctx)
	require.NoError(t, err)
	gotAll4, err := p.GetAll(ctx)
	require.NoError(t, err)
	assert.ElementsMatch(t, []APISession{s1, s3, s5Updated}, gotAll4)

	// make sure expired is not in DB
	found, _, err = p.Get(ctx, s4Expired.SessionID)
	require.NoError(t, err)
	require.False(t, found)
}

func newInmemoryDB(t *testing.T, sessions ...*APISession) *SqliteProvider {
	p, err := NewSqliteProvider(":memory:", DataSourceOptions)
	require.NoError(t, err)

	for _, cur := range sessions {
		sessionID, err := p.Save(context.Background(), *cur)
		require.NoError(t, err)

		// this will patch the supplied sessions to give them their session ids
		cur.SessionID = sessionID
	}

	return p
}

type Token struct {
	Username string `json:"username,omitempty"`
	jwt.StandardClaims
}

func generateAPISession(t *testing.T, username string, expiresAt time.Time, lastAccessAt time.Time) (apiSession APISession) {
	t.Helper()

	apiSession = APISession{
		Username:     username,
		ExpiresAt:    expiresAt,
		LastAccessAt: lastAccessAt,
		UserAgent:    "Chrome",
		IPAddress:    "1.2.3.4",
	}

	return apiSession
}

// An existing database with sessions in the previous schema is brought to
// the current one: the old sessions are gone (their tokens cannot be matched
// to them), new ones carry a token id, and ids are not reused.
func TestMigrationFromThePreviousSessionSchema(t *testing.T) {
	ctx := context.Background()
	dbPath := t.TempDir() + "/api_sessions.db"

	var previous []string
	for _, name := range api_sessions.AssetNames() {
		if strings.HasPrefix(name, "001_") || strings.HasPrefix(name, "002_") {
			previous = append(previous, name)
		}
	}
	require.Len(t, previous, 4)

	old, err := sqlite.New(dbPath, previous, api_sessions.Asset, DataSourceOptions)
	require.NoError(t, err)
	for _, user := range []string{"alice", "bob", "alice"} {
		_, err = old.ExecContext(ctx,
			"INSERT INTO api_sessions (expires_at, username, last_access_at, user_agent, ip_address) VALUES (?, ?, ?, 'ua', '192.0.2.10')",
			time.Now().Add(time.Hour), user, time.Now())
		require.NoError(t, err)
	}
	require.NoError(t, old.Close())

	p, err := NewSqliteProvider(dbPath, DataSourceOptions)
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })

	all, err := p.GetAll(ctx)
	require.NoError(t, err)
	assert.Empty(t, all, "sessions of the previous schema must not survive")

	first, err := p.Save(ctx, APISession{Username: "alice", ExpiresAt: time.Now().Add(time.Hour), LastAccessAt: time.Now(), TokenID: "token-id-1"})
	require.NoError(t, err)
	found, got, err := p.Get(ctx, first)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "token-id-1", got.TokenID)

	// An update does not change the token id, whatever the caller passes.
	got.TokenID = "something-else"
	_, err = p.Save(ctx, got)
	require.NoError(t, err)
	_, got, err = p.Get(ctx, first)
	require.NoError(t, err)
	assert.Equal(t, "token-id-1", got.TokenID)

	require.NoError(t, p.Delete(ctx, first))
	second, err := p.Save(ctx, APISession{Username: "bob", ExpiresAt: time.Now().Add(time.Hour), LastAccessAt: time.Now(), TokenID: "token-id-2"})
	require.NoError(t, err)
	assert.Greater(t, second, first, "a session id was issued twice")
}
