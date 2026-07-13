package enc

import (
	"fmt"
	"strings"
)

// envelopePrefix marks a value that has been encrypted at rest by an Envelope.
// It is self-describing and versioned so a stored value carries enough
// information to be decrypted (or recognized as legacy plaintext) without an
// out-of-band schema flag. A legacy plaintext value simply lacks the prefix.
const (
	envelopePrefix   = "enc:"
	envelopeVersion1 = "v1"
	envelopeV1Prefix = envelopePrefix + envelopeVersion1 + ":" // "enc:v1:"
)

// Envelope encrypts and decrypts individual field values under a data
// encryption key (DEK) held in RAM. It is the at-rest field-encryption layer:
// callers hand it plaintext to store and stored values to read back, and it
// keeps the DEK out of the persisted data entirely.
//
// A nil/empty DEK yields a disabled Envelope that passes plaintext through
// unchanged on write — the transition mode for a deployment that has not yet
// enabled a key provider. Reading is always fail-closed: a value that carries
// the encryption prefix can only be returned if it decrypts under the current
// DEK, so a disabled Envelope (or a wrong DEK) can never expose an encrypted
// value as plaintext.
type Envelope struct {
	dek []byte
}

// NewEnvelope builds an Envelope over the given 32-byte DEK. A nil or empty dek
// produces a disabled (passthrough-on-write) Envelope.
func NewEnvelope(dek []byte) *Envelope {
	if len(dek) == 0 {
		return &Envelope{dek: nil}
	}
	return &Envelope{dek: dek}
}

// Enabled reports whether the Envelope has a DEK and therefore encrypts on
// write. A disabled Envelope still fails closed on encrypted reads.
func (e *Envelope) Enabled() bool {
	return e != nil && len(e.dek) > 0
}

// IsEncrypted reports whether a stored value was produced by an Envelope (i.e.
// carries the versioned encryption prefix). Values without the prefix are
// treated as legacy plaintext.
func IsEncrypted(stored string) bool {
	return strings.HasPrefix(stored, envelopePrefix)
}

// Encrypt returns the at-rest representation of plaintext. When the Envelope is
// enabled the result is the versioned prefix followed by base64 AES-256-GCM
// ciphertext; when disabled the plaintext is returned unchanged. The caller is
// responsible for not encrypting sentinel/empty values it needs to round-trip
// byte-for-byte (see the users provider, which skips empty/whitespace).
func (e *Envelope) Encrypt(plaintext string) (string, error) {
	if !e.Enabled() {
		return plaintext, nil
	}
	b64, err := Aes256EncryptByKeyToBase64String([]byte(plaintext), e.dek)
	if err != nil {
		return "", fmt.Errorf("envelope encrypt: %w", err)
	}
	return envelopeV1Prefix + b64, nil
}

// Decrypt returns the plaintext for a stored value. A value without the
// encryption prefix is legacy plaintext and is returned unchanged regardless of
// whether the Envelope is enabled. A prefixed value is decrypted under the DEK;
// if the Envelope is disabled, the DEK is wrong, or the version is unknown, it
// returns an error and never falls back to exposing the raw stored bytes.
func (e *Envelope) Decrypt(stored string) (string, error) {
	if !IsEncrypted(stored) {
		return stored, nil
	}
	if !strings.HasPrefix(stored, envelopeV1Prefix) {
		return "", fmt.Errorf("envelope decrypt: unsupported encrypted value version")
	}
	if !e.Enabled() {
		return "", fmt.Errorf("envelope decrypt: value is encrypted but no key provider is configured")
	}
	b64 := strings.TrimPrefix(stored, envelopeV1Prefix)
	plaintext, err := Aes256DecryptByKeyFromBase64String(b64, e.dek)
	if err != nil {
		return "", fmt.Errorf("envelope decrypt: %w", err)
	}
	return string(plaintext), nil
}
