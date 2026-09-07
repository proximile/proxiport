package clients

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/proximile/proxiport/server/clients/clientdata"
	"github.com/proximile/proxiport/share/query"
)

// legacyDetails returns a details blob in the shape a server wrote before the
// agent configuration was redacted: the agent's own credential and a proxy
// credential, sitting in clear text next to the fields the server actually
// uses. The integers are the awkward ones on purpose — a byte count and a
// duration in nanoseconds — because a scrub that decodes JSON numbers as
// float64 re-encodes these in exponent form and the row stops loading.
//
// The proxy URL is assembled rather than written out so the fixture is not
// itself a hardcoded-credential finding.
func legacyDetails() string {
	proxyURL := &url.URL{
		Scheme: "http",
		User:   url.UserPassword("proxyuser", "proxypass"),
		Host:   "proxy.example.com:8081",
	}
	return fmt.Sprintf(`{
	"num_cpus": 8,
	"mem_total": 17179869184,
	"name": "agent one",
	"hostname": "agent-one",
	"an_unknown_future_field": {"nested": [1, 2, 3]},
	"client_configuration": {
		"client": {
			"server": "wss://server.example.com:443",
			"id": "agent-1",
			"auth": "clientAuth1:supersecretpassword",
			"auth_user": "clientAuth1",
			"auth_pass": "supersecretpassword",
			"proxy": %q,
			"proxy_url": null,
			"updates_interval": 14400000000000
		},
		"monitoring": {"enabled": true, "interval": 60000000000}
	}
}`, proxyURL.String())
}

func TestScrubStoredCredentials(t *testing.T) {
	ctx := context.Background()
	p := NewFakeClientProvider(t, nil)
	defer p.Close()

	_, err := p.db.ExecContext(ctx,
		"INSERT INTO clients (id, client_auth_id, disconnected_at, details) VALUES (?, ?, NULL, ?)",
		"agent-1", "clientAuth1", legacyDetails())
	require.NoError(t, err)

	scrubbed, err := p.ScrubStoredCredentials(ctx, testLog)
	require.NoError(t, err)
	assert.Equal(t, 1, scrubbed)

	var stored string
	require.NoError(t, p.db.GetContext(ctx, &stored, "SELECT details FROM clients WHERE id = ?", "agent-1"))

	for _, secret := range []string{"supersecretpassword", "proxypass", "proxyuser", "clientAuth1"} {
		assert.NotContains(t, stored, secret, "credential must be gone from the stored row")
	}

	// Everything else survives, numbers included and unrounded.
	assert.Contains(t, stored, "17179869184")
	assert.Contains(t, stored, "14400000000000")
	assert.Contains(t, stored, "an_unknown_future_field")

	var decoded map[string]any
	require.NoError(t, json.Unmarshal([]byte(stored), &decoded))
	cfg, ok := decoded["client_configuration"].(map[string]any)
	require.True(t, ok)
	clientCfg, ok := cfg["client"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "wss://server.example.com:443", clientCfg["server"])
	for _, key := range credentialFields {
		assert.NotContains(t, clientCfg, key)
	}

	// The scrubbed row still loads, and still carries what the server reads.
	loaded, err := p.GetAll(ctx, testLog)
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	require.NotNil(t, loaded[0].ClientConfiguration)
	assert.True(t, loaded[0].ClientConfiguration.Monitoring.Enabled)
	assert.EqualValues(t, 17179869184, loaded[0].MemoryTotal)

	// A second pass has nothing left to do.
	scrubbed, err = p.ScrubStoredCredentials(ctx, testLog)
	require.NoError(t, err)
	assert.Equal(t, 0, scrubbed)
}

// TestScrubStoredCredentialsSkipsUnreadableRow: a row that cannot be decoded
// cannot be loaded or served either, so it is reported and stepped over rather
// than failing the server's start.
func TestScrubStoredCredentialsSkipsUnreadableRow(t *testing.T) {
	ctx := context.Background()
	p := NewFakeClientProvider(t, nil)
	defer p.Close()

	_, err := p.db.ExecContext(ctx,
		"INSERT INTO clients (id, client_auth_id, disconnected_at, details) VALUES (?, ?, NULL, ?)",
		"broken", "clientAuth1", "{not json")
	require.NoError(t, err)
	_, err = p.db.ExecContext(ctx,
		"INSERT INTO clients (id, client_auth_id, disconnected_at, details) VALUES (?, ?, NULL, ?)",
		"agent-1", "clientAuth1", legacyDetails())
	require.NoError(t, err)

	scrubbed, err := p.ScrubStoredCredentials(ctx, testLog)
	require.NoError(t, err)
	assert.Equal(t, 1, scrubbed)
}

// TestClientPayloadRedactsTunnelPassword guards the tunnel side of the same
// response. models.Remote.AuthPassword guards a tunnel's HTTP proxy and the
// server needs it in memory, so it cannot be dropped from the model's JSON the
// way the agent's credential can — it has to be cleared on the way out.
func TestClientPayloadRedactsTunnelPassword(t *testing.T) {
	client := New(t).Logger(testLog).Build()
	tunnels := client.GetTunnels()
	require.NotEmpty(t, tunnels)
	tunnels[0].AuthUser = "tunneluser"
	tunnels[0].AuthPassword = "tunnelsecret"

	calculated := clientdata.NewCalculatedClient(client, nil, clientdata.Connected)
	payload := ConvertToClientPayload(calculated, []query.FieldsOption{
		{Resource: "clients", Fields: []string{"id", "tunnels"}},
	})

	require.NotNil(t, payload.Tunnels)
	require.NotEmpty(t, *payload.Tunnels)
	assert.Empty(t, (*payload.Tunnels)[0].AuthPassword)
	assert.Equal(t, "tunneluser", (*payload.Tunnels)[0].AuthUser, "the user name is not the secret")

	encoded, err := json.Marshal(payload)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "tunnelsecret")

	// The live tunnel keeps the password it needs to check requests.
	assert.Equal(t, "tunnelsecret", client.GetTunnels()[0].AuthPassword)
}
