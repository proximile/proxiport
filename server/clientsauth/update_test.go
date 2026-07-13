package clientsauth

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// upgradeToHash reproduces what the client listener does after a legacy
// plaintext credential authenticates: hash the matched plaintext and persist it.
func upgradeToHash(t *testing.T, p Provider, id, plaintext string) {
	t.Helper()
	hashed, err := HashPassword(plaintext)
	require.NoError(t, err)
	require.NoError(t, p.Update(&ClientAuth{ID: id, Password: hashed}))
}

// assertMigratedRow asserts a credential row is now a bcrypt hash that still
// verifies against the original plaintext.
func assertMigratedRow(t *testing.T, p Provider, id, plaintext string) {
	t.Helper()
	got, err := p.Get(id)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.True(t, IsHashed(got.Password), "row should be hashed after upgrade")
	assert.True(t, VerifyPassword(got.Password, []byte(plaintext)), "hashed row should still verify against the original password")
}

func TestDatabaseProviderUpdateMigratesPlaintext(t *testing.T) {
	// Seed a legacy plaintext row, then upgrade it in place (the migration path).
	p := NewDatabaseMockProvider([]*ClientAuth{{ID: "agent-legacy", Password: "clientpass"}}, t)

	got, err := p.Get("agent-legacy")
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, "clientpass", got.Password, "seeded row starts as plaintext")
	require.False(t, IsHashed(got.Password))

	upgradeToHash(t, p, "agent-legacy", "clientpass")
	assertMigratedRow(t, p, "agent-legacy", "clientpass")
}

func TestFileProviderUpdateMigratesPlaintext(t *testing.T) {
	p := NewMockFileProvider([]*ClientAuth{{ID: "agent-legacy", Password: "clientpass"}}, t)

	// Warm the cache with the plaintext entry, then upgrade; Update must
	// invalidate the cache so a later Get returns the hashed value.
	got, err := p.Get("agent-legacy")
	require.NoError(t, err)
	require.Equal(t, "clientpass", got.Password)

	upgradeToHash(t, p, "agent-legacy", "clientpass")
	assertMigratedRow(t, p, "agent-legacy", "clientpass")
}

func TestSingleProviderUpdateNotWriteable(t *testing.T) {
	p := NewSingleProvider("agent", "clientpass")
	assert.False(t, p.IsWriteable())
	assert.Error(t, p.Update(&ClientAuth{ID: "agent", Password: "x"}), "single provider must reject updates")
}
