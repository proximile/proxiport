// Package keyprovider supplies the data encryption key (DEK) used by the
// at-rest envelope layer (share/enc). The DEK is held in RAM only and never
// persisted alongside the data it protects.
//
// Two providers exist today — a local key file and an environment variable —
// which is enough to keep recoverable secrets off a stolen disk. Both sit
// behind the KeyProvider interface so a KMS- or TEE-sealed provider (where the
// key is unwrapped by an external root of trust) can be added later without
// changing any call site.
package keyprovider

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
)

// Provider type identifiers as used in the [key_provider] config section.
const (
	TypeNone = "none"
	TypeFile = "file"
	TypeEnv  = "env"
)

// DEKSize is the required data-encryption-key length in bytes (AES-256).
const DEKSize = 32

// KeyProvider yields the DEK for the envelope layer. Implementations keep the
// key in memory; the interface deliberately exposes no way to persist it.
type KeyProvider interface {
	// Type returns the provider type identifier (none|file|env).
	Type() string
	// Enabled reports whether a DEK is available (false only for the none provider).
	Enabled() bool
	// DEK returns the 32-byte data encryption key, or nil when not enabled.
	DEK() []byte
}

// noneProvider is the disabled provider: no DEK, envelope encryption off. It is
// the transition mode for a deployment that stores recoverable secrets as
// plaintext and has not yet turned on a key provider.
type noneProvider struct{}

// NewNone returns the disabled key provider.
func NewNone() KeyProvider { return noneProvider{} }

func (noneProvider) Type() string  { return TypeNone }
func (noneProvider) Enabled() bool { return false }
func (noneProvider) DEK() []byte   { return nil }

// staticProvider holds a DEK already resolved into memory (from a file or env
// var). The bytes are copied on construction so the caller's buffer can be
// zeroed without affecting the provider.
type staticProvider struct {
	typ string
	dek []byte
}

func (p staticProvider) Type() string  { return p.typ }
func (p staticProvider) Enabled() bool { return len(p.dek) == DEKSize }
func (p staticProvider) DEK() []byte   { return p.dek }

// NewFileProvider reads and decodes a DEK from the file at path. A missing,
// unreadable, or malformed key file is a hard error so the server fails closed
// at boot rather than silently running without at-rest encryption.
func NewFileProvider(path string) (KeyProvider, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // operator-configured key path
	if err != nil {
		return nil, fmt.Errorf("read key file %q: %w", path, err)
	}
	dek, err := decodeDEK(raw)
	if err != nil {
		return nil, fmt.Errorf("key file %q: %w", path, err)
	}
	return staticProvider{typ: TypeFile, dek: dek}, nil
}

// NewEnvProvider reads and decodes a DEK from the named environment variable.
// An unset or malformed variable is a hard error (fail closed at boot).
func NewEnvProvider(envVar string) (KeyProvider, error) {
	val, ok := os.LookupEnv(envVar)
	if !ok || val == "" {
		return nil, fmt.Errorf("environment variable %q is not set", envVar)
	}
	dek, err := decodeDEK([]byte(val))
	if err != nil {
		return nil, fmt.Errorf("environment variable %q: %w", envVar, err)
	}
	return staticProvider{typ: TypeEnv, dek: dek}, nil
}

// decodeDEK turns raw key material into a 32-byte DEK. It accepts, in order:
// exactly 32 raw bytes (used verbatim), base64, hex, or a 32-character text
// key. Everything must resolve to exactly DEKSize bytes; anything else is an
// error so an accidentally-truncated or wrong-length key never silently
// weakens the cipher.
func decodeDEK(raw []byte) ([]byte, error) {
	// Exactly 32 bytes on disk: a raw binary key, used as-is (do not trim, so a
	// key byte that happens to be whitespace is preserved).
	if len(raw) == DEKSize {
		out := make([]byte, DEKSize)
		copy(out, raw)
		return out, nil
	}

	s := strings.TrimSpace(string(raw))
	if b, err := base64.StdEncoding.DecodeString(s); err == nil && len(b) == DEKSize {
		return b, nil
	}
	if b, err := hex.DecodeString(s); err == nil && len(b) == DEKSize {
		return b, nil
	}
	if len(s) == DEKSize {
		return []byte(s), nil
	}
	return nil, fmt.Errorf("key must decode to %d bytes (raw, base64, or hex)", DEKSize)
}
