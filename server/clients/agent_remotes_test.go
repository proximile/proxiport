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
		HTTPProxy:     true,
		HostHeader:    "internal.example",
		SkipTLSVerify: true,
		AuthUser:      "operator",
		AuthPassword:  "tunnelsecret",
		Owner:         "admin",
	}

	sanitized := sanitizeAgentRemotes([]*models.Remote{hostile})
	require.Len(t, sanitized, 1)
	got := sanitized[0]

	assert.Empty(t, got.TunnelURL)
	assert.False(t, got.HTTPProxy)
	assert.Empty(t, got.HostHeader)
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
