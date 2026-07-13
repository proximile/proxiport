package vault

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	errors2 "github.com/proximile/proxiport/server/api/errors"
	"github.com/proximile/proxiport/share/enc"
)

const (
	minPassLengthBytes = 8
	maxPassLengthBytes = 256

	// Argon2id cost parameters for new and re-keyed vaults. The vault unlocks
	// infrequently, so this is tuned well above interactive-login budgets:
	// 64 MiB of memory, 3 passes, 4 lanes.
	argon2Time      uint32 = 3
	argon2MemoryKiB uint32 = 64 * 1024
	argon2Threads   uint8  = 4
	argon2SaltLen          = 16

	kdfArgon2id = "argon2id"
)

type Aes256PassManager struct {
}

func (apm *Aes256PassManager) ValidatePass(passToCheck string) error {
	if len(passToCheck) < minPassLengthBytes {
		return errors2.APIError{
			Message: fmt.Sprintf(
				"password is too short, expected length is at least %d bytes, provided length is %d bytes",
				minPassLengthBytes,
				len(passToCheck),
			),
			HTTPStatus: http.StatusBadRequest,
		}
	}

	if len(passToCheck) > maxPassLengthBytes {
		return errors2.APIError{
			Message: fmt.Sprintf(
				"password is too long, expected length is at most %d bytes, provided length is %d bytes",
				maxPassLengthBytes,
				len(passToCheck),
			),
			HTTPStatus: http.StatusBadRequest,
		}
	}

	return nil
}

// DeriveKey derives the AES-256 key for pass under the KDF described by
// dbStatus.KDF. When KDF is empty the vault predates the Argon2id migration and
// its key is the legacy unsalted SHA-256 of the passphrase; legacy is then true
// so the caller knows to re-key on a successful unlock.
func (apm *Aes256PassManager) DeriveKey(dbStatus DbStatus, pass string) (key []byte, legacy bool, err error) {
	if pass == "" {
		return nil, false, errors.New("empty password")
	}

	if strings.TrimSpace(dbStatus.KDF) == "" {
		return enc.DeriveKeyLegacySHA256(pass), true, nil
	}

	algo, salt, timeCost, memKiB, threads, err := parseKDF(dbStatus.KDF)
	if err != nil {
		return nil, false, err
	}
	if algo != kdfArgon2id {
		return nil, false, fmt.Errorf("unsupported vault kdf %q", algo)
	}

	return enc.DeriveKeyArgon2id(pass, salt, timeCost, memKiB, threads), false, nil
}

// Verify reports whether key authenticates dbStatus.EncCheckValue. The check is
// the AES-GCM authentication tag on a random verifier that was encrypted under
// the correct key at init/re-key time: a wrong key fails the tag (compared in
// constant time inside crypto/cipher). The verifier's plaintext is never stored,
// so the database holds no known-plaintext oracle to brute-force against.
func (apm *Aes256PassManager) Verify(dbStatus DbStatus, key []byte) bool {
	if dbStatus.EncCheckValue == "" || len(key) == 0 {
		return false
	}
	_, err := enc.Aes256DecryptByKeyFromBase64String(dbStatus.EncCheckValue, key)
	return err == nil
}

// NewStatusFor generates a fresh Argon2id salt, derives the key for pass, and
// returns the self-describing KDF descriptor plus an enc_check verifier (a fresh
// random value encrypted under the new key). Used both at initialization and
// when transparently re-keying a legacy vault.
func (apm *Aes256PassManager) NewStatusFor(pass string) (kdf, encCheck string, key []byte, err error) {
	salt := make([]byte, argon2SaltLen)
	if _, err = rand.Read(salt); err != nil {
		return "", "", nil, err
	}
	key = enc.DeriveKeyArgon2id(pass, salt, argon2Time, argon2MemoryKiB, argon2Threads)

	verifier := make([]byte, 32)
	if _, err = rand.Read(verifier); err != nil {
		return "", "", nil, err
	}
	encCheck, err = enc.Aes256EncryptByKeyToBase64String(verifier, key)
	if err != nil {
		return "", "", nil, err
	}

	kdf = encodeKDF(kdfArgon2id, salt, argon2Time, argon2MemoryKiB, argon2Threads)
	return kdf, encCheck, key, nil
}

// encodeKDF renders a self-describing descriptor: "argon2id$m=..,t=..,p=..$<b64salt>".
func encodeKDF(algo string, salt []byte, timeCost, memKiB uint32, threads uint8) string {
	return fmt.Sprintf(
		"%s$m=%d,t=%d,p=%d$%s",
		algo, memKiB, timeCost, threads, base64.StdEncoding.EncodeToString(salt),
	)
}

// parseKDF strictly parses a descriptor produced by encodeKDF. Any malformed
// input is rejected (fail closed) rather than defaulted, so a tampered
// descriptor cannot silently weaken the KDF.
func parseKDF(s string) (algo string, salt []byte, timeCost, memKiB uint32, threads uint8, err error) {
	parts := strings.Split(s, "$")
	if len(parts) != 3 {
		return "", nil, 0, 0, 0, fmt.Errorf("invalid kdf descriptor")
	}
	algo = parts[0]

	var mm, tt, pp uint64
	var haveM, haveT, haveP bool
	for _, kv := range strings.Split(parts[1], ",") {
		field := strings.SplitN(kv, "=", 2)
		if len(field) != 2 {
			return "", nil, 0, 0, 0, fmt.Errorf("invalid kdf param %q", kv)
		}
		v, perr := strconv.ParseUint(field[1], 10, 32)
		if perr != nil {
			return "", nil, 0, 0, 0, fmt.Errorf("invalid kdf param %q: %w", kv, perr)
		}
		switch field[0] {
		case "m":
			mm, haveM = v, true
		case "t":
			tt, haveT = v, true
		case "p":
			pp, haveP = v, true
		default:
			return "", nil, 0, 0, 0, fmt.Errorf("unknown kdf param %q", field[0])
		}
	}
	if !haveM || !haveT || !haveP {
		return "", nil, 0, 0, 0, fmt.Errorf("missing kdf params")
	}
	if mm == 0 || tt == 0 || pp == 0 || pp > 255 {
		return "", nil, 0, 0, 0, fmt.Errorf("out-of-range kdf params")
	}

	salt, err = base64.StdEncoding.DecodeString(parts[2])
	if err != nil {
		return "", nil, 0, 0, 0, fmt.Errorf("invalid kdf salt: %w", err)
	}
	if len(salt) == 0 {
		return "", nil, 0, 0, 0, fmt.Errorf("empty kdf salt")
	}

	return algo, salt, uint32(tt), uint32(mm), uint8(pp), nil
}
