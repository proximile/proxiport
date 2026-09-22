package enc

import (
	"fmt"
	"regexp"
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

// envelopeVersionedPrefix matches the full versioned prefix an Envelope writes,
// and nothing shorter. Matching the bare "enc:" treated any value that happened
// to begin with those four characters as ciphertext -- which is a string a
// command's stdout can easily contain (`grep -r 'enc:' /etc`), and a string a
// hostile agent can return on purpose.
var envelopeVersionedPrefix = regexp.MustCompile(`^` + envelopePrefix + `v[0-9]+:`)

// IsEncrypted reports whether a stored value has the shape an Envelope writes:
// the prefix, a version, and a colon. Values without it are legacy plaintext.
//
// Shape is not provenance. A caller deciding whether to ENCRYPT a value must
// use Envelope.IsEncryptedBy instead -- a value can carry this shape and still
// be untrusted content that has never been encrypted.
func IsEncrypted(stored string) bool {
	return envelopeVersionedPrefix.MatchString(stored)
}

// IsEncryptedBy reports whether stored was produced by THIS Envelope: it has
// the envelope shape AND decrypts under the current DEK.
//
// This is the question a write path means when it asks "is this already
// encrypted, can I skip it?". Asking IsEncrypted instead meant that a command
// whose output began with the sentinel was stored in CLEARTEXT under a
// configured key -- the at-rest guarantee silently skipped for exactly the
// values an attacker chooses -- and then failed to decrypt on every later read,
// taking the whole job listing with it.
//
// A disabled Envelope can never answer true, which is correct: with no key, no
// value here was produced by us.
func (e *Envelope) IsEncryptedBy(stored string) bool {
	if !e.Enabled() || !IsEncrypted(stored) {
		return false
	}
	_, err := e.Decrypt(stored)
	return err == nil
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
