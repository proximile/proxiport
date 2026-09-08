package chserver

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/proximile/proxiport/server/clients/clienttunnel"
	"github.com/proximile/proxiport/share/models"
)

// GET /tunnels serves every tunnel the caller can see, and TunnelPayload
// embeds models.Remote whole -- including the HTTP basic-auth password that
// guards the tunnel. The server needs that value in memory to check incoming
// requests, so it cannot leave the model's JSON; it has to be cleared on the
// way out.
func TestConvertToTunnelPayloadRedactsPassword(t *testing.T) {
	tunnel := &clienttunnel.Tunnel{
		ID: "1",
		Remote: models.Remote{
			Protocol:     models.ProtocolTCP,
			LocalHost:    "0.0.0.0",
			LocalPort:    "3390",
			RemoteHost:   "127.0.0.1",
			RemotePort:   "22",
			HTTPProxy:    true,
			AuthUser:     "operator",
			AuthPassword: "tunnelsecret",
		},
	}

	payload := convertToTunnelPayload(tunnel, "client-1")

	assert.Empty(t, payload.AuthPassword)
	assert.Equal(t, "operator", payload.AuthUser, "the user name is not the secret")

	encoded, err := json.Marshal(payload)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "tunnelsecret")

	// The live tunnel keeps the value the proxy checks against.
	assert.Equal(t, "tunnelsecret", tunnel.AuthPassword)
}
