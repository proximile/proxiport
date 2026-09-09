package clients

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/proximile/proxiport/share/models"
)

// An agent that writes its own connection request rather than using the shipped
// client can put anything in its requested remotes, and a compromised agent is
// inside the threat model. models.Remote is also the API's tunnel-creation
// type, so it carries fields the API sets only after checking the caller's
// permissions.
//
// TunnelURL is the one that mattered: HasSubdomainTunnel() gates the creation of
// a downstream Caddy route, and the same value supplies that route's domain --
// which used to be rendered into the admin-API body by a text/template.
func TestSanitizeAgentRemotesDropsServerControlledFields(t *testing.T) {
	hostile := &models.Remote{
		// What an honest agent sends: parsed from its "remotes" config strings.
		Protocol:   models.ProtocolTCP,
		LocalHost:  "0.0.0.0",
		LocalPort:  "25000",
		RemoteHost: "127.0.0.1",
		RemotePort: "22",

		// What only a hand-written connection request can add.
		TunnelURL:     "https://sub.evil.example",
		SkipTLSVerify: true,
		AuthUser:      "operator",
		AuthPassword:  "tunnelsecret",
		Owner:         "admin",
	}

	sanitized := sanitizeAgentRemotes([]*models.Remote{hostile})
	require.Len(t, sanitized, 1)
	got := sanitized[0]

	assert.Empty(t, got.TunnelURL)
	assert.False(t, got.SkipTLSVerify)
	assert.Empty(t, got.AuthUser)
	assert.Empty(t, got.AuthPassword)
	assert.Empty(t, got.Owner)
	assert.False(t, got.HasSubdomainTunnel(), "no downstream caddy route can be reached from a connect request")

	// The tunnel the agent actually asked for still happens.
	assert.Equal(t, "0.0.0.0", got.LocalHost)
	assert.Equal(t, "25000", got.LocalPort)
	assert.Equal(t, "127.0.0.1", got.RemoteHost)
	assert.Equal(t, "22", got.RemotePort)
	assert.Equal(t, models.ProtocolTCP, got.Protocol)

	// The caller's own slice is untouched: it is still the request as received,
	// which the audit log and the client record are entitled to see.
	assert.Equal(t, "https://sub.evil.example", hostile.TunnelURL)
}

func TestSanitizeAgentRemotesHandlesEmptyInput(t *testing.T) {
	assert.Nil(t, sanitizeAgentRemotes(nil))
	assert.Empty(t, sanitizeAgentRemotes([]*models.Remote{}))
	assert.Empty(t, sanitizeAgentRemotes([]*models.Remote{nil}))
}

// The per-tunnel proxy options are a documented agent-side feature:
//
//	remotes = ['8443:pikvm.lan:443 scheme=https reverse_proxy host_header=pikvm.lan']
//
// client/config.go's parseRemoteEntry + applyTunnelsConfig turn those into
// Scheme, HTTPProxy and HostHeader on the Remote the agent sends. Clearing them
// here would silently turn every reverse-proxied agent tunnel back into a plain
// one -- a config that still loads, still connects, and quietly stops doing
// what it says.
//
// They also grant nothing: they configure the proxy in front of the agent's own
// tunnel, which it is entitled to ask for. The dangerous field is TunnelURL,
// which builds a *downstream Caddy route*, and that is cleared above.
func TestSanitizeAgentRemotesKeepsTheAgentsOwnProxySettings(t *testing.T) {
	scheme := "https"
	configured := &models.Remote{
		Protocol:   models.ProtocolTCP,
		LocalHost:  "0.0.0.0",
		LocalPort:  "8443",
		RemoteHost: "pikvm.lan",
		RemotePort: "443",

		Scheme:     &scheme,
		HTTPProxy:  true,
		HostHeader: "pikvm.lan",
	}

	sanitized := sanitizeAgentRemotes([]*models.Remote{configured})
	require.Len(t, sanitized, 1)
	got := sanitized[0]

	require.NotNil(t, got.Scheme)
	assert.Equal(t, "https", *got.Scheme, "scheme= must survive")
	assert.True(t, got.HTTPProxy, "reverse_proxy must survive")
	assert.Equal(t, "pikvm.lan", got.HostHeader, "host_header= must survive")
}
