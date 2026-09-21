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

// TestGetTunnelsToReestablishSurvivesANullRemote is L4. The agent's Remotes are
// json.Unmarshal'ed straight into a []*models.Remote, so `"Remotes":[null]` is
// a slice with a nil element. sanitizeAgentRemotes already skips nil entries
// and is tested for it -- but it runs AFTER this function, which dereferenced
// the nil and panicked. net/http recovers that, so it aborted the handshake and
// dumped a full panic stack into the server log instead of killing the daemon:
// repeatable at will by an unprivileged agent.
func TestGetTunnelsToReestablishSurvivesANullRemote(t *testing.T) {
	pinned := &models.Remote{LocalHost: "0.0.0.0", LocalPort: "3390", RemoteHost: "0.0.0.0", RemotePort: "22"}
	random := &models.Remote{LocalPortRandom: true, RemoteHost: "0.0.0.0", RemotePort: "22"}

	testCases := []struct {
		name string
		old  []*models.Remote
		new  []*models.Remote
	}{
		{
			name: "a single null",
			old:  []*models.Remote{pinned},
			new:  []*models.Remote{nil},
		},
		{
			name: "a null beside a pinned remote",
			old:  []*models.Remote{pinned, random},
			new:  []*models.Remote{pinned, nil},
		},
		{
			// loop2 is a separate pass over the same slice and dereferences
			// curNew again; guarding only loop1 still panicked here.
			name: "a null beside a random-port remote, which only loop2 reaches",
			old:  []*models.Remote{random, pinned},
			new:  []*models.Remote{random, nil},
		},
		{
			name: "nothing at all",
			old:  []*models.Remote{pinned},
			new:  nil,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			assert.NotPanics(t, func() {
				getTunnelsToReestablish(tc.old, tc.new)
			})
		})
	}
}

// TestGetTunnelsToReestablishStillReestablishes is the anti-vacuity half: the
// nil guard must skip the null entry, not short-circuit the function. It
// returns the old tunnels the reconnecting agent did NOT ask for again, so an
// agent that reconnects carrying only a null must still get its tunnel back.
func TestGetTunnelsToReestablishStillReestablishes(t *testing.T) {
	remote := &models.Remote{LocalHost: "0.0.0.0", LocalPort: "3390", RemoteHost: "0.0.0.0", RemotePort: "22"}
	existing := []*models.Remote{remote}

	assert.Len(t, getTunnelsToReestablish(existing, nil), 1)
	assert.Len(t, getTunnelsToReestablish(existing, []*models.Remote{nil}), 1)
	assert.Empty(t, getTunnelsToReestablish(existing, []*models.Remote{remote}),
		"a tunnel the agent asked for again is not one to re-establish")
}
