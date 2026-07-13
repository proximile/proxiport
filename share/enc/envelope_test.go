package enc

import (
	"strings"
	"testing"
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
