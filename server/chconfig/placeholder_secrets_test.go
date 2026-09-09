package chconfig

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// proxiportd.example.conf ships placeholder secrets for the packaging scripts
// to substitute. They are published in the repository, so a deployment still
// holding one is holding a secret everyone can read.
//
// jwt_secret was the silently fatal one: the code mints a random secret only
// when the setting is empty, and "<YOUR_SECRET>" is not empty, so it became the
// HS256 signing key. Anyone who could reach the API could then mint a token for
// any username -- the claims are entirely caller-chosen -- and hold the admin
// API.
func TestPlaceholderSecretsAreRefused(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mutate  func(*Config)
		mustSay string
	}{
		{"jwt secret", func(c *Config) { c.API.JWTSecret = "<YOUR_SECRET>" }, "[api] jwt_secret"},
		{"key seed", func(c *Config) { c.Server.KeySeed = "<YOUR_SEED>" }, "[server] key_seed"},
		{"client auth", func(c *Config) { c.Server.Auth = "clientAuth1:1234" }, "[server] auth"},
		{"api auth", func(c *Config) { c.API.Auth = "admin:foobaz" }, "[api] auth"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &Config{}
			tc.mutate(cfg)

			err := cfg.validateNoPlaceholderSecrets()
			require.Error(t, err, "a published placeholder must not start the server")
			assert.Contains(t, err.Error(), tc.mustSay)
			assert.Contains(t, err.Error(), "not a secret")
		})
	}
}

func TestRealSecretsAreAccepted(t *testing.T) {
	cfg := &Config{}
	cfg.API.JWTSecret = "a-real-random-secret"
	cfg.Server.KeySeed = "a-real-random-seed"
	cfg.Server.Auth = "clientAuth1:a-real-password"
	cfg.API.Auth = "admin:a-real-password"

	require.NoError(t, cfg.validateNoPlaceholderSecrets())

	// An unset value is fine: jwt_secret is generated when empty, and the
	// others are optional.
	require.NoError(t, (&Config{}).validateNoPlaceholderSecrets())
}
