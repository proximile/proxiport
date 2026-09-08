package clientsauth

import (
	"crypto/subtle"
	"errors"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// HashCost is the bcrypt work factor used for agent-auth credentials.
// bcrypt.DefaultCost (10) balances resistance to offline cracking against the
// cost paid on every agent (re)connect.
const HashCost = bcrypt.DefaultCost

// bcryptPrefixes are the algorithm identifiers a bcrypt hash can carry: "$2a$"
// (Go's golang.org/x/crypto/bcrypt), "$2b$" (most other libraries) and "$2y$"
// (htpasswd). A stored credential beginning with one of these is a hash;
// anything else is treated as a legacy plaintext credential. This mirrors the
// list the server package uses for API-user passwords.
var bcryptPrefixes = []string{"$2a$", "$2b$", "$2y$"}

// IsHashed reports whether a stored credential is already a bcrypt hash rather
// than a legacy plaintext value.
func IsHashed(stored string) bool {
	for _, p := range bcryptPrefixes {
		if strings.HasPrefix(stored, p) {
			return true
		}
	}
	return false
}

// HashPassword returns a bcrypt hash of the given plaintext. A value that is
// already a bcrypt hash is returned unchanged, so an operator may supply a
// pre-hashed credential (in a config or auth file) and it passes through
// untouched.
func HashPassword(password string) (string, error) {
	// Refuse to mint a hash of nothing. The at-rest upgrade path takes whatever
	// the provider holds and writes the hash back, so hashing an empty value
	// would replace a real credential on disk with bcrypt("") -- which then
	// authenticates any agent sending an empty password.
	if password == "" {
		return "", errors.New("refusing to hash an empty credential")
	}
	if IsHashed(password) {
		return password, nil
	}
	h, err := bcrypt.GenerateFromPassword([]byte(password), HashCost)
	if err != nil {
		return "", err
	}
	return string(h), nil
}

// VerifyPassword reports whether provided matches the stored credential. A
// hashed credential is compared with bcrypt; a legacy plaintext credential is
// compared in constant time. Callers upgrade a matched plaintext credential to
// a hash (see IsHashed / HashPassword) so the plaintext is replaced at rest.
func VerifyPassword(stored string, provided []byte) bool {
	// Fail closed on an empty credential on either side. A stored value can go
	// empty through a bug rather than through configuration -- it did, when the
	// API handlers redacted a response by writing to the provider's own object
	// -- and the plaintext branch below is a constant-time compare, which
	// answers true for two empty strings. That turns a corrupted credential
	// into an authentication bypass: golang.org/x/crypto/ssh passes a
	// zero-length password straight through to the callback.
	if stored == "" || len(provided) == 0 {
		return false
	}
	if IsHashed(stored) {
		return bcrypt.CompareHashAndPassword([]byte(stored), provided) == nil
	}
	return subtle.ConstantTimeCompare([]byte(stored), provided) == 1
}
