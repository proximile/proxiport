package users

import (
	"strings"
	"testing"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/proximile/proxiport/server/chconfig"
)

func TestIsBcryptHash(t *testing.T) {
	assert.True(t, IsBcryptHash("$2y$05$cIOk1IlsdgdUeZpV464d6OXKI1tF2Yc3MWo55xDu4XhopEJmGb2KC"))
	assert.True(t, IsBcryptHash("$2a$10$abcdefghijklmnopqrstuv"))
	assert.True(t, IsBcryptHash("$2b$12$abcdefghijklmnopqrstuv"))
	assert.False(t, IsBcryptHash("plaintext"))
	assert.False(t, IsBcryptHash("$1$md5salt$hash"))
	assert.False(t, IsBcryptHash(""))
}

// The static `auth = "user:password"` config credential must be hashed at load
// so the running process never holds the plaintext and the verifier is
// bcrypt-only.
func TestStaticAuthUserIsHashedAtLoad(t *testing.T) {
	cfg := &chconfig.Config{}
	cfg.API.Auth = "admin:foobaz"

	svc, err := NewAPIServiceFromConfig(nil, cfg)
	require.NoError(t, err)

	u, err := svc.GetByUsername("admin")
	require.NoError(t, err)
	require.NotNil(t, u)
	assert.True(t, IsBcryptHash(u.Password), "static user password must be stored as a bcrypt hash, got %q", u.Password)
	assert.NoError(t, bcrypt.CompareHashAndPassword([]byte(u.Password), []byte("foobaz")),
		"the hash must verify against the original plaintext")
	assert.Error(t, bcrypt.CompareHashAndPassword([]byte(u.Password), []byte("wrong")))
}

// A plaintext password in the DB user table makes the server refuse to start.
func TestDatabaseRejectsPlaintextPasswordAtBoot(t *testing.T) {
	db, err := sqlx.Connect("sqlite3", ":memory:")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.NoError(t, prepareTables(db, false, false, false))

	_, err = db.Exec("INSERT INTO `users` (username, password) VALUES ('legacy', 'plaintextpw')")
	require.NoError(t, err)

	_, err = NewUserDatabase(db, "users", "groups", "", false, false, nil, testLog)
	require.Error(t, err, "a plaintext password row must make construction fail closed")
	assert.Contains(t, err.Error(), "non-bcrypt")
	assert.Contains(t, err.Error(), "legacy")
}

// A bcrypt-hashed DB row is accepted; an empty password row is ignored (it can
// never authenticate anyway).
func TestDatabaseAcceptsBcryptAndEmptyPasswords(t *testing.T) {
	db, err := sqlx.Connect("sqlite3", ":memory:")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.NoError(t, prepareTables(db, false, false, false))

	hash, err := GenerateTokenHash("secret")
	require.NoError(t, err)
	_, err = db.Exec("INSERT INTO `users` (username, password) VALUES ('admin', ?)", hash)
	require.NoError(t, err)
	_, err = db.Exec("INSERT INTO `users` (username, password) VALUES ('nopass', '')")
	require.NoError(t, err)

	_, err = NewUserDatabase(db, "users", "groups", "", false, false, nil, testLog)
	require.NoError(t, err)
}

// The file provider accepts every bcrypt identifier ($2y$, $2a$, $2b$), not
// just htpasswd's $2y$, and still rejects plaintext.
func TestFileProviderAcceptsAllBcryptPrefixes(t *testing.T) {
	for _, p := range []string{"$2y$", "$2a$", "$2b$"} {
		json := `[{"username":"u","password":"` + p + `10$0123456789012345678901uWXYZabcdefghijklmnopqrstuvwx012"}]`
		usrs, err := parseUsers(strings.NewReader(json))
		require.NoErrorf(t, err, "prefix %s should be accepted", p)
		require.Len(t, usrs, 1)
	}

	_, err := parseUsers(strings.NewReader(`[{"username":"u","password":"plaintext"}]`))
	require.Error(t, err, "plaintext password must be rejected")
}
