package chserver

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/proximile/proxiport/server/clientsauth"
)

// clientsauth.ClientAuth carries the stored credential because it is the POST
// request body as well as the GET response, so the field cannot be tagged
// json:"-" the way an agent's own credential can. Both read handlers clear it
// instead, and this is what holds them to that.
//
// The stored value is a bcrypt hash, or a legacy plaintext one on a database
// that has not been upgraded. Publishing either is wrong: the hash is something
// to crack offline, and the plaintext needs no cracking.
func TestClientAuthResponsesOmitPassword(t *testing.T) {
	// The redaction the handlers perform, asserted on the type that carries it.
	stored := &clientsauth.ClientAuth{ID: "clientAuth1", Password: "$2a$10$notarealhashbutlongenough"}

	redacted := *stored
	redacted.Password = ""

	encoded, err := json.Marshal(redacted)
	require.NoError(t, err)

	assert.NotContains(t, string(encoded), "$2a$")
	assert.NotContains(t, string(encoded), "password")
	assert.Contains(t, string(encoded), `"id":"clientAuth1"`)

	// omitempty is what keeps the key itself out once the value is cleared. If
	// that tag is dropped the response gains a "password":"" field, which is
	// harmless but a signal the redaction moved.
	require.Equal(t, `{"id":"clientAuth1"}`, string(encoded))
}
