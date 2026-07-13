package chconfig

import (
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/proximile/proxiport/server/keyprovider"
	"github.com/proximile/proxiport/share/enc"
)

const (
	testDEKHex      = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	testOtherDEKHex = "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"
)

// keyProviderFromHex builds a resolved env-backed key provider config holding
// the given DEK, the same way a server boot does.
func keyProviderFromHex(t *testing.T, dekHex string) keyprovider.Config {
	t.Helper()

	t.Setenv("PROXIPORT_TEST_DEK", dekHex)
	kp := keyprovider.Config{Type: keyprovider.TypeEnv, EnvVar: "PROXIPORT_TEST_DEK"}
	require.NoError(t, kp.ParseAndValidate())
	require.True(t, kp.Envelope().Enabled())

	return kp
}

// encryptWith produces the at-rest form of a config secret under the test DEK,
// standing in for what `proxiportd secret encrypt` writes into a config file.
func encryptWith(t *testing.T, plaintext string) string {
	t.Helper()

	dek, err := hex.DecodeString(testDEKHex)
	require.NoError(t, err)

	encrypted, err := enc.NewEnvelope(dek).Encrypt(plaintext)
	require.NoError(t, err)
	require.True(t, enc.IsEncrypted(encrypted))

	return encrypted
}

// Every secret the config can hold must round-trip through the envelope, so a
// deployment can put ciphertext in the config file for all of them.
func TestDecryptSecretsUnwrapsEveryField(t *testing.T) {
	c := Config{KeyProvider: keyProviderFromHex(t, testDEKHex)}

	plaintexts := map[string]string{}
	for i, f := range c.secretFields() {
		plaintext := string(rune('a'+i)) + "-secret-value"
		plaintexts[f.name] = plaintext
		*f.value = encryptWith(t, plaintext)
	}

	require.NoError(t, c.decryptSecrets())

	for _, f := range c.secretFields() {
		assert.Equal(t, plaintexts[f.name], *f.value, "%s should have been decrypted", f.name)
	}
}

// A config written before the key provider existed keeps working: values
// without the envelope prefix are used exactly as they are.
func TestDecryptSecretsLeavesPlaintextUntouched(t *testing.T) {
	c := Config{KeyProvider: keyProviderFromHex(t, testDEKHex)}
	c.Server.KeySeed = "plain-seed"
	c.API.JWTSecret = "plain-jwt-secret"

	require.NoError(t, c.decryptSecrets())

	assert.Equal(t, "plain-seed", c.Server.KeySeed)
	assert.Equal(t, "plain-jwt-secret", c.API.JWTSecret)
}

// Without the key there is no plaintext fallback: an encrypted config value is
// an error, never the raw stored bytes.
func TestDecryptSecretsFailsClosedWithoutKeyProvider(t *testing.T) {
	encrypted := encryptWith(t, "plain-seed")

	c := Config{}
	c.Server.KeySeed = encrypted

	err := c.decryptSecrets()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "server.key_seed")
	assert.Equal(t, encrypted, c.Server.KeySeed, "the ciphertext must not be exposed as a plaintext value")
}

func TestDecryptSecretsFailsClosedWithWrongKey(t *testing.T) {
	c := Config{KeyProvider: keyProviderFromHex(t, testOtherDEKHex)}
	c.API.JWTSecret = encryptWith(t, "plain-jwt-secret")

	err := c.decryptSecrets()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "api.jwt_secret")
}

// The whole server refuses to start on a config whose secrets it cannot read,
// rather than falling back to the ciphertext or to a freshly generated secret.
func TestParseAndValidateFailsClosedOnUnreadableSecret(t *testing.T) {
	c := Config{Server: defaultValidMinServerConfig}
	c.Server.KeySeed = encryptWith(t, "plain-seed")

	err := c.ParseAndValidate(&Mlog)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "server.key_seed")
}

func TestParseAndValidateDecryptsSecretsBeforeUse(t *testing.T) {
	c := Config{Server: defaultValidMinServerConfig, KeyProvider: keyProviderFromHex(t, testDEKHex)}
	c.Server.KeySeed = encryptWith(t, "plain-seed")
	c.Server.Auth = encryptWith(t, "clientauth:clientpass")

	require.NoError(t, c.ParseAndValidate(&Mlog))

	assert.Equal(t, "plain-seed", c.Server.KeySeed)
	// The credential is decrypted early enough for the client-auth section to
	// parse it into its id/password parts.
	assert.Equal(t, "clientauth", c.Server.AuthID)
	assert.Equal(t, "clientpass", c.Server.AuthPassword)
}
