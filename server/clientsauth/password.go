package clientsauth

import (
	"crypto/subtle"
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
	if IsHashed(stored) {
		return bcrypt.CompareHashAndPassword([]byte(stored), provided) == nil
	}
	return subtle.ConstantTimeCompare([]byte(stored), provided) == 1
}
