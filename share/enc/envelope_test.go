package enc

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	testDEK      = []byte("0123456789abcdef0123456789abcdef") // 32 bytes
	otherDEK     = []byte("abcdef0123456789abcdef0123456789") // 32 bytes, different
	samplePlain  = "otpauth://totp/proxiport:admin?secret=JBSWY3DPEHPK3PXP&issuer=proxiport&algorithm=SHA1&digits=6&period=30"
	legacyPlain  = "otpauth://totp/legacy:user?secret=GEZDGNBVGY3TQOJQ&issuer=legacy&algorithm=SHA1&digits=6&period=30"
	unknownVerCT = "enc:v2:AAAAAAAAAAAAAAAAAAAA"
)

func TestEnvelope_RoundTrip(t *testing.T) {
	e := NewEnvelope(testDEK)
	if !e.Enabled() {
		t.Fatal("envelope with a 32-byte DEK should be enabled")
	}

	ct, err := e.Encrypt(samplePlain)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if !IsEncrypted(ct) {
		t.Fatalf("ciphertext %q should be recognized as encrypted", ct)
	}
	if !strings.HasPrefix(ct, envelopeV1Prefix) {
		t.Fatalf("ciphertext %q should carry the v1 prefix", ct)
	}
	if strings.Contains(ct, samplePlain) {
		t.Fatal("plaintext must not appear in the ciphertext")
	}

	got, err := e.Decrypt(ct)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if got != samplePlain {
		t.Fatalf("round-trip mismatch: got %q want %q", got, samplePlain)
	}
}

func TestEnvelope_NonceIsRandom(t *testing.T) {
	e := NewEnvelope(testDEK)
	a, err := e.Encrypt(samplePlain)
	if err != nil {
		t.Fatal(err)
	}
	b, err := e.Encrypt(samplePlain)
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("two encryptions of the same plaintext must differ (random nonce)")
	}
}

func TestEnvelope_DisabledPassthrough(t *testing.T) {
	e := NewEnvelope(nil)
	if e.Enabled() {
		t.Fatal("nil-DEK envelope should be disabled")
	}
	ct, err := e.Encrypt(samplePlain)
	if err != nil {
		t.Fatal(err)
	}
	if ct != samplePlain {
		t.Fatalf("disabled envelope should pass plaintext through, got %q", ct)
	}
	if IsEncrypted(ct) {
		t.Fatal("disabled-envelope output should not be marked encrypted")
	}
}

func TestEnvelope_DisabledFailsClosedOnEncrypted(t *testing.T) {
	ct, err := NewEnvelope(testDEK).Encrypt(samplePlain)
	if err != nil {
		t.Fatal(err)
	}
	// A disabled envelope must NOT expose an encrypted value as raw bytes.
	if _, err := NewEnvelope(nil).Decrypt(ct); err == nil {
		t.Fatal("disabled envelope must fail to decrypt an encrypted value, not pass it through")
	}
}

func TestEnvelope_WrongKeyFailsClosed(t *testing.T) {
	ct, err := NewEnvelope(testDEK).Encrypt(samplePlain)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewEnvelope(otherDEK).Decrypt(ct); err == nil {
		t.Fatal("decrypt with the wrong DEK must fail (GCM tag), not return garbage")
	}
}

func TestEnvelope_LegacyPlaintextPassthrough(t *testing.T) {
	// A value without the prefix is legacy plaintext and reads back unchanged
	// under both an enabled and a disabled envelope.
	for name, e := range map[string]*Envelope{"enabled": NewEnvelope(testDEK), "disabled": NewEnvelope(nil)} {
		got, err := e.Decrypt(legacyPlain)
		if err != nil {
			t.Fatalf("%s: legacy plaintext decrypt: %v", name, err)
		}
		if got != legacyPlain {
			t.Fatalf("%s: legacy plaintext should pass through, got %q", name, got)
		}
	}
}

func TestEnvelope_UnknownVersionFailsClosed(t *testing.T) {
	if _, err := NewEnvelope(testDEK).Decrypt(unknownVerCT); err == nil {
		t.Fatal("an unknown encrypted-value version must be rejected")
	}
}

func TestEnvelope_EmptyRoundTrip(t *testing.T) {
	// The users layer skips empty/whitespace, but the envelope itself must still
	// round-trip an empty string when enabled.
	e := NewEnvelope(testDEK)
	ct, err := e.Encrypt("")
	if err != nil {
		t.Fatal(err)
	}
	got, err := e.Decrypt(ct)
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("empty round-trip mismatch: got %q", got)
	}
}

// TestIsEncryptedRequiresAVersionedPrefix is half of M5. The sentinel used to
// be the bare "enc:", which is four characters any command's stdout can
// contain -- `grep -r 'enc:' /etc` is enough, and a hostile agent can send it
// on purpose. A write path asking "is this already encrypted?" then answered
// yes, skipped encryption, and stored the value in cleartext under a
// configured key; the read path then refused it and took the whole listing
// with it.
func TestIsEncryptedRequiresAVersionedPrefix(t *testing.T) {
	testCases := []struct {
		value string
		want  bool
	}{
		{"", false},
		{"enc:", false},
		{"enc:x", false},
		{"enc:hello", false},
		{"enc:v", false},
		{"enc:v1", false},
		{"encrypted output follows", false},
		{"the word enc: appears mid-string", false},
		{"enc:v1:", true},
		{"enc:v1:AAAA", true},
		{"enc:v2:AAAA", true},
		{"enc:v10:AAAA", true},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.value, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, IsEncrypted(tc.value))
		})
	}
}

// TestIsEncryptedByRequiresProvenanceNotShape is the other half. Shape is
// forgeable -- an agent can send "enc:v1:whatever" -- so a write path has to
// ask whether THIS envelope produced the value, which only the key can answer.
func TestIsEncryptedByRequiresProvenanceNotShape(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	other := []byte("fedcba9876543210fedcba9876543210")

	envelope := NewEnvelope(key)

	ciphertext, err := envelope.Encrypt("real output")
	require.NoError(t, err)
	require.True(t, IsEncrypted(ciphertext))

	assert.True(t, envelope.IsEncryptedBy(ciphertext), "our own ciphertext")
	assert.False(t, envelope.IsEncryptedBy("enc:v1:whatever"), "forged shape is not provenance")
	assert.False(t, envelope.IsEncryptedBy("enc:v1:"+base64.StdEncoding.EncodeToString([]byte("not ours"))),
		"well-formed base64 that is not our ciphertext")
	assert.False(t, envelope.IsEncryptedBy("plain output"), "plaintext")

	foreign, err := NewEnvelope(other).Encrypt("real output")
	require.NoError(t, err)
	assert.False(t, envelope.IsEncryptedBy(foreign), "another key's ciphertext is not ours")
	assert.True(t, IsEncrypted(foreign),
		"but it IS envelope-shaped -- which is what the backfill path must respect")

	disabled := NewEnvelope(nil)
	assert.False(t, disabled.IsEncryptedBy(ciphertext), "with no key, nothing here was produced by us")
}
