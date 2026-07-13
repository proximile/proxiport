package clientsauth

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHashPassword(t *testing.T) {
	const plaintext = "clientpass"

	hashed, err := HashPassword(plaintext)
	require.NoError(t, err)
	assert.True(t, IsHashed(hashed), "HashPassword must return a bcrypt hash")
	assert.NotEqual(t, plaintext, hashed, "hash must not equal the plaintext")

	// Distinct salts => distinct hashes for the same input.
	hashed2, err := HashPassword(plaintext)
	require.NoError(t, err)
	assert.NotEqual(t, hashed, hashed2, "each hash should use a fresh salt")

	// An already-hashed value passes through unchanged (operator-supplied hash).
	passthrough, err := HashPassword(hashed)
	require.NoError(t, err)
	assert.Equal(t, hashed, passthrough)
}

func TestVerifyPassword(t *testing.T) {
	hashed, err := HashPassword("clientpass")
	require.NoError(t, err)

	assert.True(t, VerifyPassword(hashed, []byte("clientpass")), "correct password must verify against the hash")
	assert.False(t, VerifyPassword(hashed, []byte("wrong")), "wrong password must not verify")

	// Legacy plaintext credential (pre-migration rows / single & file configs).
	assert.True(t, VerifyPassword("legacyplain", []byte("legacyplain")), "correct plaintext must verify")
	assert.False(t, VerifyPassword("legacyplain", []byte("nope")), "wrong plaintext must not verify")
}

func TestIsHashed(t *testing.T) {
	for _, prefix := range bcryptPrefixes {
		assert.True(t, IsHashed(prefix+"abcdefghijklmnopqrstuv"), prefix+" should be detected as hashed")
	}
	assert.False(t, IsHashed("clientpass"), "a plaintext credential is not hashed")
	assert.False(t, IsHashed(""), "an empty value is not hashed")
}
