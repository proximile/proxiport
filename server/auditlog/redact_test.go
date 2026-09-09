package auditlog

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRedactSecretsCoversNestedBodies: the audit log stores whole request and
// response bodies, so a credential anywhere in one has to be caught wherever it
// sits — including inside a nested object or an array element, which is how a
// tunnel's auth_password arrived.
func TestRedactSecretsCoversNestedBodies(t *testing.T) {
	body := map[string]any{
		"name":          "web",
		"auth_user":     "operator",
		"auth_password": "tunnelsecret",
		"nested": map[string]any{
			"password": "innersecret",
			"port":     8080,
		},
		"list": []any{
			map[string]any{"token": "tok_abcdef", "id": "1"},
		},
		"mem_total": json.Number("17179869184"),
	}
	raw, err := json.Marshal(body)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, json.Unmarshal(redactSecrets(raw), &got))

	assert.Equal(t, redactedPlaceholder, got["auth_password"])
	assert.Equal(t, redactedPlaceholder, got["nested"].(map[string]any)["password"])
	assert.Equal(t, redactedPlaceholder, got["list"].([]any)[0].(map[string]any)["token"])

	// Everything that is not a secret survives, the auditable facts included.
	assert.Equal(t, "web", got["name"])
	assert.Equal(t, "operator", got["auth_user"], "the user name is the auditable half")
	assert.EqualValues(t, 8080, got["nested"].(map[string]any)["port"])
	assert.Equal(t, "1", got["list"].([]any)[0].(map[string]any)["id"])

	// A large integer must not come back through float64 in exponent form.
	assert.Contains(t, string(redactSecrets(raw)), "17179869184")
}

// TestRedactSecretsLeavesUnsetFieldsAlone: writing "[redacted]" over an absent
// credential would claim one was supplied. The placeholder has to mean
// something.
func TestRedactSecretsLeavesUnsetFieldsAlone(t *testing.T) {
	raw := []byte(`{"auth_user":"","auth_password":"","password":null,"token":"real"}`)

	var got map[string]any
	require.NoError(t, json.Unmarshal(redactSecrets(raw), &got))

	assert.Equal(t, "", got["auth_password"])
	assert.Nil(t, got["password"])
	assert.Equal(t, redactedPlaceholder, got["token"])
}

// TestRedactSecretsPassesCleanBodiesThrough keeps entries byte-identical when
// there is nothing to redact, so the tamper-evidence chain is not disturbed by
// a re-encode.
func TestRedactSecretsPassesCleanBodiesThrough(t *testing.T) {
	raw := []byte(`{"b":1,"a":"two"}`)
	assert.Equal(t, string(raw), string(redactSecrets(raw)))
}
