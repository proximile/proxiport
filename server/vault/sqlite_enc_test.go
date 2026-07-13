package vault

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/proximile/proxiport/share/enc"
)

var vaultTestDEK = []byte("0123456789abcdef0123456789abcdef") // 32 bytes

type fileConfigMock struct{ path string }

func (f fileConfigMock) GetVaultDBPath() string { return f.path }

func rawCol(t *testing.T, p *SqliteProvider, q string) string {
	t.Helper()
	var s string
	require.NoError(t, p.db.Get(&s, q))
	return s
}

func saveSecret(t *testing.T, p *SqliteProvider, plaintext string) int64 {
	t.Helper()
	id, err := p.Save(context.Background(), "user1",
		0, &InputValue{ClientID: "c1", Key: "k1", Value: plaintext, Type: SecretType}, time.Now())
	require.NoError(t, err)
	return id
}

// With a key provider configured, enc_check and every values.value are wrapped
// under the DEK at rest, yet read back transparently.
func TestSqliteProvider_WrapsSecretsAtRest(t *testing.T) {
	p, err := NewSqliteProvider(configMock{}, enc.NewEnvelope(vaultTestDEK), testLog)
	require.NoError(t, err)
	defer func() { _ = p.Close() }()
	ctx := context.Background()

	require.NoError(t, p.SetStatus(ctx, DbStatus{
		StatusName:    DbStatusInit,
		EncCheckValue: "passphrase-layer-verifier",
		KDF:           "argon2id$m=65536,t=3,p=4$c2FsdA==",
	}))
	id := saveSecret(t, p, "passphrase-layer-ciphertext")

	// At rest: enc_check and value are DEK-wrapped; kdf stays plaintext (salt).
	encCheckRaw := rawCol(t, p, "SELECT enc_check FROM `status` LIMIT 1")
	assert.True(t, enc.IsEncrypted(encCheckRaw), "enc_check should be DEK-wrapped")
	assert.NotContains(t, encCheckRaw, "passphrase-layer-verifier")
	valueRaw := rawCol(t, p, "SELECT value FROM `values` LIMIT 1")
	assert.True(t, enc.IsEncrypted(valueRaw), "value should be DEK-wrapped")
	assert.NotContains(t, valueRaw, "passphrase-layer-ciphertext")
	assert.Equal(t, "argon2id$m=65536,t=3,p=4$c2FsdA==",
		rawCol(t, p, "SELECT kdf FROM `status` LIMIT 1"), "kdf (salt) must stay plaintext")

	// Read back: transparently unwrapped.
	st, err := p.GetStatus(ctx)
	require.NoError(t, err)
	assert.Equal(t, "passphrase-layer-verifier", st.EncCheckValue)
	got, found, err := p.GetByID(ctx, int(id))
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "passphrase-layer-ciphertext", got.Value)
}

// A vault written without a key provider is wrapped in place at startup when a
// provider is later configured, idempotently.
func TestSqliteProvider_BackfillWrapsExisting(t *testing.T) {
	cfg := fileConfigMock{path: filepath.Join(t.TempDir(), "vault.db")}
	ctx := context.Background()

	// First boot: no key provider — secrets written as plaintext at rest.
	p0, err := NewSqliteProvider(cfg, enc.NewEnvelope(nil), testLog)
	require.NoError(t, err)
	require.NoError(t, p0.SetStatus(ctx, DbStatus{StatusName: DbStatusInit, EncCheckValue: "verifier", KDF: "kdf1"}))
	saveSecret(t, p0, "plain-secret")
	assert.False(t, enc.IsEncrypted(rawCol(t, p0, "SELECT value FROM `values` LIMIT 1")))
	require.NoError(t, p0.Close())

	// Next boot with a key provider: the backfill wraps the existing secrets.
	p1, err := NewSqliteProvider(cfg, enc.NewEnvelope(vaultTestDEK), testLog)
	require.NoError(t, err)
	defer func() { _ = p1.Close() }()

	assert.True(t, enc.IsEncrypted(rawCol(t, p1, "SELECT enc_check FROM `status` LIMIT 1")))
	wrappedValue := rawCol(t, p1, "SELECT value FROM `values` LIMIT 1")
	assert.True(t, enc.IsEncrypted(wrappedValue))
	st, err := p1.GetStatus(ctx)
	require.NoError(t, err)
	assert.Equal(t, "verifier", st.EncCheckValue)
	got, found, err := p1.GetByID(ctx, 1)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "plain-secret", got.Value)
	require.NoError(t, p1.Close())

	// Idempotent: a third boot must not re-wrap (ciphertext unchanged).
	p2, err := NewSqliteProvider(cfg, enc.NewEnvelope(vaultTestDEK), testLog)
	require.NoError(t, err)
	defer func() { _ = p2.Close() }()
	assert.Equal(t, wrappedValue, rawCol(t, p2, "SELECT value FROM `values` LIMIT 1"),
		"already-wrapped values must not be re-wrapped")
}

// ReKey peels the DEK wrap, applies the passphrase-layer transform, and re-wraps
// — leaving values encrypted at rest and readable through the provider.
func TestSqliteProvider_ReKeyThroughDEK(t *testing.T) {
	p, err := NewSqliteProvider(configMock{}, enc.NewEnvelope(vaultTestDEK), testLog)
	require.NoError(t, err)
	defer func() { _ = p.Close() }()
	ctx := context.Background()

	require.NoError(t, p.SetStatus(ctx, DbStatus{StatusName: DbStatusInit, EncCheckValue: "old-enc", KDF: ""}))
	saveSecret(t, p, "val1")

	transform := func(old string) (string, error) { return "reenc:" + old, nil }
	require.NoError(t, p.ReKey(ctx, transform, "argon2id$m=1,t=1,p=1$c2FsdA==", "new-enc"))

	assert.True(t, enc.IsEncrypted(rawCol(t, p, "SELECT value FROM `values` LIMIT 1")),
		"value must remain DEK-wrapped after re-key")
	got, found, err := p.GetByID(ctx, 1)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "reenc:val1", got.Value, "transform applied to the passphrase layer")
	st, err := p.GetStatus(ctx)
	require.NoError(t, err)
	assert.Equal(t, "new-enc", st.EncCheckValue)
	assert.Equal(t, "argon2id$m=1,t=1,p=1$c2FsdA==", st.KDF)
}

// A vault wrapped under one DEK must fail closed when opened with the wrong (or
// absent) key — never surface ciphertext.
func TestSqliteProvider_FailsClosedWrongKey(t *testing.T) {
	cfg := fileConfigMock{path: filepath.Join(t.TempDir(), "vault.db")}
	ctx := context.Background()

	p, err := NewSqliteProvider(cfg, enc.NewEnvelope(vaultTestDEK), testLog)
	require.NoError(t, err)
	require.NoError(t, p.SetStatus(ctx, DbStatus{StatusName: DbStatusInit, EncCheckValue: "verifier", KDF: "kdf1"}))
	saveSecret(t, p, "plain-secret")
	require.NoError(t, p.Close())

	wrongKey := []byte("ffffffffffffffffffffffffffffffff")
	p2, err := NewSqliteProvider(cfg, enc.NewEnvelope(wrongKey), testLog)
	require.NoError(t, err)
	defer func() { _ = p2.Close() }()

	_, err = p2.GetStatus(ctx)
	require.Error(t, err, "enc_check must fail closed under the wrong DEK")
	_, _, err = p2.GetByID(ctx, 1)
	require.Error(t, err, "value must fail closed under the wrong DEK")
}
