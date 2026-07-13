package keyprovider

import (
	"fmt"
	"strings"

	"github.com/proximile/proxiport/share/enc"
)

// Config is the [key_provider] configuration section. It selects and builds the
// KeyProvider that supplies the at-rest envelope DEK.
//
//	[key_provider]
//	  type = "file"          # none | file | env
//	  key_file = "/etc/proxiport/dek.key"   # for type = file
//	  env_var  = "PROXIPORT_DEK"            # for type = env
type Config struct {
	Type    string `mapstructure:"type"`
	KeyFile string `mapstructure:"key_file"`
	EnvVar  string `mapstructure:"env_var"`

	// provider is built by ParseAndValidate and returned by Provider(). It is
	// never populated from the config file.
	provider KeyProvider `mapstructure:"-"`
}

// ParseAndValidate resolves the configured provider, loading the DEK into
// memory for the file/env types. A misconfigured or unavailable key is a hard
// error so the server fails closed at boot instead of starting without the
// at-rest encryption the operator asked for.
func (c *Config) ParseAndValidate() error {
	switch strings.ToLower(strings.TrimSpace(c.Type)) {
	case "", TypeNone:
		c.provider = NewNone()
		return nil
	case TypeFile:
		if c.KeyFile == "" {
			return fmt.Errorf("key_provider.key_file is required when type = %q", TypeFile)
		}
		p, err := NewFileProvider(c.KeyFile)
		if err != nil {
			return fmt.Errorf("key_provider: %w", err)
		}
		c.provider = p
		return nil
	case TypeEnv:
		if c.EnvVar == "" {
			return fmt.Errorf("key_provider.env_var is required when type = %q", TypeEnv)
		}
		p, err := NewEnvProvider(c.EnvVar)
		if err != nil {
			return fmt.Errorf("key_provider: %w", err)
		}
		c.provider = p
		return nil
	default:
		return fmt.Errorf("key_provider.type must be one of %s|%s|%s, got %q", TypeNone, TypeFile, TypeEnv, c.Type)
	}
}

// Provider returns the resolved key provider. It is safe to call before
// ParseAndValidate (returns the disabled none provider).
func (c *Config) Provider() KeyProvider {
	if c.provider == nil {
		return NewNone()
	}
	return c.provider
}

// DEK is a convenience for call sites building an envelope: it returns the
// resolved provider's DEK (nil when disabled).
func (c *Config) DEK() []byte {
	return c.Provider().DEK()
}

// Envelope builds an at-rest field-encryption envelope over the resolved DEK.
// A disabled provider yields a passthrough envelope.
func (c *Config) Envelope() *enc.Envelope {
	return enc.NewEnvelope(c.DEK())
}
