package vault

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/proximile/proxiport/server/api/errors"
	"github.com/proximile/proxiport/share/enc"
)

func TestInvalidPasswords(t *testing.T) {
	testCases := []struct {
		InputPass     string
		ExpectedError string
		ExpectedCode  int
	}{
		{
			InputPass:     "1",
			ExpectedError: "password is too short, expected length is at least 8 bytes, provided length is 1 bytes",
			ExpectedCode:  http.StatusBadRequest,
		},
		{
			InputPass:     "1234567",
			ExpectedError: "password is too short, expected length is at least 8 bytes, provided length is 7 bytes",
			ExpectedCode:  http.StatusBadRequest,
		},
		{
			InputPass:     string(make([]byte, maxPassLengthBytes+1)),
			ExpectedError: "password is too long, expected length is at most 256 bytes, provided length is 257 bytes",
			ExpectedCode:  http.StatusBadRequest,
		},
	}

	for i := range testCases {
		passManager := Aes256PassManager{}
		err := passManager.ValidatePass(testCases[i].InputPass)
		apiErr, ok := err.(errors.APIError)
		require.True(t, ok)
		assert.Equal(t, testCases[i].ExpectedError, apiErr.Message)
		assert.Equal(t, testCases[i].ExpectedCode, apiErr.HTTPStatus)
	}
}

func TestValidPassword(t *testing.T) {
	passManager := Aes256PassManager{}
	require.NoError(t, passManager.ValidatePass("12345678"))
	require.NoError(t, passManager.ValidatePass("a-reasonably-long-master-passphrase"))
}

// TestNewStatusForRoundTrips proves the Argon2id path: NewStatusFor produces a
// self-describing descriptor + verifier, DeriveKey re-derives the *same* key
// from the passphrase, and Verify accepts the right key and rejects any other.
func TestNewStatusForRoundTrips(t *testing.T) {
	pm := &Aes256PassManager{}
	const pass = "correct horse battery"

	kdf, encCheck, key, err := pm.NewStatusFor(pass)
	require.NoError(t, err)
	assert.Contains(t, kdf, "argon2id$")
	assert.NotEmpty(t, encCheck)
	require.Len(t, key, 32)

	status := DbStatus{EncCheckValue: encCheck, KDF: kdf}

	derived, legacy, err := pm.DeriveKey(status, pass)
	require.NoError(t, err)
	assert.False(t, legacy)
	assert.Equal(t, key, derived, "same passphrase + stored salt must reproduce the key")
	assert.True(t, pm.Verify(status, derived))

	wrong, _, err := pm.DeriveKey(status, pass+"x")
	require.NoError(t, err)
	assert.NotEqual(t, key, wrong)
	assert.False(t, pm.Verify(status, wrong), "a wrong passphrase must fail the GCM tag")
}

// TestNewStatusForUsesFreshSalt proves the KDF is salted per vault: two vaults
// initialized with the same passphrase get different salts and thus keys.
func TestNewStatusForUsesFreshSalt(t *testing.T) {
	pm := &Aes256PassManager{}
	const pass = "same-passphrase-twice"

	kdf1, _, key1, err := pm.NewStatusFor(pass)
	require.NoError(t, err)
	kdf2, _, key2, err := pm.NewStatusFor(pass)
	require.NoError(t, err)

	assert.NotEqual(t, kdf1, kdf2, "salts must differ")
	assert.NotEqual(t, key1, key2, "keys must differ across installs")
}

// TestDeriveKeyLegacy proves a legacy (empty-KDF) vault is detected and read
// with the old unsalted key so it can be re-keyed.
func TestDeriveKeyLegacy(t *testing.T) {
	pm := &Aes256PassManager{}
	const pass = "legacy-pass"

	legacyKey := enc.DeriveKeyLegacySHA256(pass)
	encCheck, err := enc.Aes256EncryptByKeyToBase64String([]byte("verifier-value"), legacyKey)
	require.NoError(t, err)

	status := DbStatus{EncCheckValue: encCheck, KDF: ""}

	key, legacy, err := pm.DeriveKey(status, pass)
	require.NoError(t, err)
	assert.True(t, legacy)
	assert.Equal(t, legacyKey, key)
	assert.True(t, pm.Verify(status, key))
}

func TestDeriveKeyErrors(t *testing.T) {
	pm := &Aes256PassManager{}

	_, _, err := pm.DeriveKey(DbStatus{KDF: "argon2id$m=1,t=1,p=1$c2FsdA=="}, "")
	require.Error(t, err, "empty passphrase must be rejected")

	_, _, err = pm.DeriveKey(DbStatus{KDF: "scrypt$m=1,t=1,p=1$c2FsdA=="}, "somepass")
	require.Error(t, err, "unsupported algorithm must be rejected")

	_, _, err = pm.DeriveKey(DbStatus{KDF: "garbage-descriptor"}, "somepass")
	require.Error(t, err, "malformed descriptor must be rejected")
}

func TestVerifyRejectsEmpty(t *testing.T) {
	pm := &Aes256PassManager{}
	assert.False(t, pm.Verify(DbStatus{EncCheckValue: ""}, []byte("k")))
	assert.False(t, pm.Verify(DbStatus{EncCheckValue: "x"}, nil))
}

func TestKDFEncodeParse(t *testing.T) {
	salt := []byte("sixteen-byte-slt")
	kdf := encodeKDF(kdfArgon2id, salt, 3, 65536, 4)

	algo, gotSalt, timeCost, memKiB, threads, err := parseKDF(kdf)
	require.NoError(t, err)
	assert.Equal(t, kdfArgon2id, algo)
	assert.Equal(t, salt, gotSalt)
	assert.Equal(t, uint32(3), timeCost)
	assert.Equal(t, uint32(65536), memKiB)
	assert.Equal(t, uint8(4), threads)
}

func TestKDFParseRejectsMalformed(t *testing.T) {
	bad := []string{
		"",
		"argon2id",
		"argon2id$m=1,t=1,p=1",                   // missing salt segment
		"argon2id$m=1,t=1,p=1$c2FsdA==$extra",    // too many segments
		"argon2id$m=1,t=1$c2FsdA==",              // missing p
		"argon2id$m=1,t=1,p=1,x=9$c2FsdA==",      // unknown param
		"argon2id$m=abc,t=1,p=1$c2FsdA==",        // non-numeric
		"argon2id$m=1,t=1,p=0$c2FsdA==",          // zero threads
		"argon2id$m=0,t=1,p=1$c2FsdA==",          // zero memory
		"argon2id$m=1,t=1,p=999$c2FsdA==",        // threads out of uint8 range
		"argon2id$m=1,t=1,p=1$not-valid-base64!", // bad salt
		"argon2id$m=1,t=1,p=1$",                  // empty salt
	}
	for _, s := range bad {
		_, _, _, _, _, err := parseKDF(s)
		assert.Error(t, err, "expected %q to be rejected", s)
	}
}
