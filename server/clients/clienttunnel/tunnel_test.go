package clienttunnel

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A Tunnel that came back from client storage has no TunnelProtocol: the field
// is an interface tagged `json:"-"`, so unmarshalling leaves it nil. Every
// stored tunnel is in this state after a daemon restart, and the reconnect path
// terminates a client's tunnels before rebuilding them, so terminating one must
// not panic.
func TestTunnelTerminateWithoutLiveProtocol(t *testing.T) {
	var tunnel Tunnel
	require.NoError(t, json.Unmarshal([]byte(`{"id":"1","lhost":"0.0.0.0","lport":"3000","rhost":"127.0.0.1","rport":"22"}`), &tunnel))

	// Precondition: this is what deserialization actually produces.
	require.Nil(t, tunnel.TunnelProtocol)

	assert.NotPanics(t, func() {
		assert.NoError(t, tunnel.Terminate(true))
		assert.NoError(t, tunnel.Terminate(false))
	})
}
