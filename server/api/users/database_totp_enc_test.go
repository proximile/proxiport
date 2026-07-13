package users

import (
	"testing"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/proximile/proxiport/share/enc"
)

var totpTestDEK = []byte("0123456789abcdef0123456789abcdef") // 32 bytes

const sampleTotP = "otpauth://totp/proxiport:admin?secret=JBSWY3DPEHPK3PXP&issuer=proxiport&algorithm=SHA1&digits=6&period=30"

func rawTotP(t *testing.T, db *sqlx.DB, username string) string {
	t.Helper()
	var stored string
	require.NoError(t, db.Get(&stored, "SELECT totp_secret FROM `users` WHERE username = ?", username))
	return stored
}

// With a key provider configured, totp_secret is written encrypted and read
// back transparently; empty and the " " clear-sentinel are stored verbatim.
func TestUserDatabase_TotPEncryptedAtRest(t *testing.T) {
	db, err := sqlx.Connect("sqlite3", ":memory:")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.NoError(t, prepareTables(db, false, true, false))

	d, err := NewUserDatabase(db, "users", "groups", "", false, true, enc.NewEnvelope(totpTestDEK), testLog)
	require.NoError(t, err)

	require.NoError(t, d.Add(&User{Username: "admin", Password: "pass", TotP: sampleTotP}))

	stored := rawTotP(t, db, "admin")
	assert.True(t, enc.IsEncrypted(stored), "stored value should be encrypted")
	assert.NotContains(t, stored, "JBSWY3DPEHPK3PXP", "base32 seed must not be present at rest")

	got, err := d.GetByUsername("admin")
	require.NoError(t, err)
	assert.Equal(t, sampleTotP, got.TotP, "decrypt-on-read should return the plaintext otpauth URL")

	// The delete path writes " " to clear a secret — it must not be encrypted.
	require.NoError(t, d.Update(&User{TotP: " "}, "admin"))
	assert.Equal(t, " ", rawTotP(t, db, "admin"), "clear sentinel must be stored verbatim")
	got, err = d.GetByUsername("admin")
	require.NoError(t, err)
	assert.Equal(t, " ", got.TotP)
}

// A vault seeded in the legacy plaintext format is re-encrypted in place at
// startup, is then ciphertext at rest, and still reads back; re-running the
// migration is a no-op.
func TestUserDatabase_TotPBackfillMigration(t *testing.T) {
	db, err := sqlx.Connect("sqlite3", ":memory:")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.NoError(t, prepareTables(db, false, true, false))

	// Seed a legacy plaintext secret directly, as the old code would have.
	_, err = db.Exec("INSERT INTO `users` (username, password, totp_secret) VALUES ('legacy', 'pass', ?)", sampleTotP)
	require.NoError(t, err)
	// And a user with no secret, to prove empty values are left alone.
	_, err = db.Exec("INSERT INTO `users` (username, password) VALUES ('nosecret', 'pass')")
	require.NoError(t, err)

	// Constructing the provider with an enabled envelope runs the backfill.
	d, err := NewUserDatabase(db, "users", "groups", "", false, true, enc.NewEnvelope(totpTestDEK), testLog)
	require.NoError(t, err)

	migrated := rawTotP(t, db, "legacy")
	assert.True(t, enc.IsEncrypted(migrated), "legacy plaintext should be encrypted at rest after startup")
	assert.NotEqual(t, sampleTotP, migrated)
	assert.Equal(t, "", rawTotP(t, db, "nosecret"), "an empty secret must be left untouched")

	got, err := d.GetByUsername("legacy")
	require.NoError(t, err)
	assert.Equal(t, sampleTotP, got.TotP, "the migrated secret must still read back")

	// Re-running the migration is idempotent: the ciphertext is unchanged.
	_, err = NewUserDatabase(db, "users", "groups", "", false, true, enc.NewEnvelope(totpTestDEK), testLog)
	require.NoError(t, err)
	assert.Equal(t, migrated, rawTotP(t, db, "legacy"), "already-encrypted values must not be re-encrypted")
}

// With no key provider the column is stored and read as plaintext (legacy mode).
func TestUserDatabase_TotPDisabledPassthrough(t *testing.T) {
	db, err := sqlx.Connect("sqlite3", ":memory:")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.NoError(t, prepareTables(db, false, true, false))

	d, err := NewUserDatabase(db, "users", "groups", "", false, true, nil, testLog)
	require.NoError(t, err)

	require.NoError(t, d.Add(&User{Username: "admin", Password: "pass", TotP: sampleTotP}))
	assert.Equal(t, sampleTotP, rawTotP(t, db, "admin"), "disabled provider stores plaintext")

	got, err := d.GetByUsername("admin")
	require.NoError(t, err)
	assert.Equal(t, sampleTotP, got.TotP)
}

// A value encrypted under one key must fail closed when the provider has a
// different (or absent) key — never surface as ciphertext.
func TestUserDatabase_TotPFailsClosedOnWrongKey(t *testing.T) {
	db, err := sqlx.Connect("sqlite3", ":memory:")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.NoError(t, prepareTables(db, false, true, false))

	d, err := NewUserDatabase(db, "users", "groups", "", false, true, enc.NewEnvelope(totpTestDEK), testLog)
	require.NoError(t, err)
	require.NoError(t, d.Add(&User{Username: "admin", Password: "pass", TotP: sampleTotP}))

	// A provider that has no key (DEK absent) must refuse to read the encrypted
	// value rather than return the raw ciphertext.
	d2, err := NewUserDatabase(db, "users", "groups", "", false, true, enc.NewEnvelope(nil), testLog)
	require.NoError(t, err)
	_, err = d2.GetByUsername("admin")
	require.Error(t, err, "reading an encrypted secret without the key must fail closed")
}
