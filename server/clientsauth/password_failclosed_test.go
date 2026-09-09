package clientsauth

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A stored credential can go empty through a bug rather than through
// configuration -- it did, when the API read handlers redacted a response by
// writing to the provider's own object. The plaintext branch of VerifyPassword
// is a constant-time compare, and two empty strings compare equal, so a blank
// stored credential authenticated any agent that sent no password at all.
// golang.org/x/crypto/ssh hands a zero-length password straight to the
// callback, so there was nothing else in the way.
func TestVerifyPasswordFailsClosedOnEmptyCredentials(t *testing.T) {
	assert.False(t, VerifyPassword("", []byte("")), "blank stored, blank supplied")
	assert.False(t, VerifyPassword("", []byte("anything")), "blank stored")
	assert.False(t, VerifyPassword("s3cret", []byte("")), "blank supplied")

	hashed, err := HashPassword("s3cret")
	require.NoError(t, err)
	assert.False(t, VerifyPassword(hashed, []byte("")), "blank supplied against a hash")

	// The credential still works.
	assert.True(t, VerifyPassword("s3cret", []byte("s3cret")))
	assert.True(t, VerifyPassword(hashed, []byte("s3cret")))
}

// The at-rest upgrade path takes whatever the provider holds and writes the
// hash back, so hashing an empty value would replace a real credential on disk
// with bcrypt("") -- permanent, and satisfied by an empty password.
func TestHashPasswordRefusesAnEmptyCredential(t *testing.T) {
	_, err := HashPassword("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty credential")
}

func TestProvidersRefuseToStoreABlankCredential(t *testing.T) {
	for _, tc := range []struct {
		name string
		ca   *ClientAuth
	}{
		{"no credential", nil},
		{"empty id", &ClientAuth{ID: "", Password: "s3cret"}},
		{"empty password", &ClientAuth{ID: "clientAuth1", Password: ""}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Error(t, validateStorableCredential(tc.ca))
		})
	}

	require.NoError(t, validateStorableCredential(&ClientAuth{ID: "clientAuth1", Password: "s3cret"}))
}
