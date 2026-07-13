package chconfig

import (
	"fmt"

	"github.com/proximile/proxiport/share/enc"
)

// secretField is a config value that may be written as an "enc:v1:" envelope
// instead of in the clear. name is the TOML path, used in error messages.
type secretField struct {
	name  string
	value *string
}

// secretFields lists every config value the key provider is allowed to decrypt
// at startup: the long-term server identity, the session-signing key, and the
// credentials the server presents to other systems. A value that carries no
// envelope prefix is used exactly as written, so a plaintext config keeps
// working unchanged.
func (c *Config) secretFields() []secretField {
	return []secretField{
		{"server.key_seed", &c.Server.KeySeed},
		{"server.auth", &c.Server.Auth},
		{"api.jwt_secret", &c.API.JWTSecret},
		{"api.auth", &c.API.Auth},
		{"database.db_password", &c.Database.Password},
		{"pushover.api_token", &c.Pushover.APIToken},
		{"pushover.user_key", &c.Pushover.UserKey},
		{"smtp.auth_password", &c.SMTP.AuthPassword},
	}
}

// decryptSecrets replaces every enveloped config secret with its plaintext for
// the lifetime of the process, so what lives on disk is ciphertext and the
// plaintext exists only in memory. It must run after the key provider is
// resolved and before any section that consumes these values.
//
// It fails closed: a value carrying the envelope prefix that cannot be
// decrypted — no key provider configured, wrong DEK, unknown version — aborts
// startup rather than being used as-is or quietly replaced by a generated one.
func (c *Config) decryptSecrets() error {
	envelope := c.KeyProvider.Envelope()
	for _, f := range c.secretFields() {
		if !enc.IsEncrypted(*f.value) {
			continue
		}
		plaintext, err := envelope.Decrypt(*f.value)
		if err != nil {
			return fmt.Errorf("config secret %q: %w", f.name, err)
		}
		*f.value = plaintext
	}
	return nil
}
