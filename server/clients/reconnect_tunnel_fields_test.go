package clients

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	mapset "github.com/deckarep/golang-set"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/proximile/proxiport/server/clients/clientdata"
	"github.com/proximile/proxiport/server/clients/clienttunnel"
	"github.com/proximile/proxiport/server/ports"
	chshare "github.com/proximile/proxiport/share"
	"github.com/proximile/proxiport/share/models"
	"github.com/proximile/proxiport/share/test"
)

// freeTCPPort returns a port that was free a moment ago.
func freeTCPPort(t *testing.T) int {
	t.Helper()
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := probe.Addr().(*net.TCPAddr).Port
	require.NoError(t, probe.Close())
	return port
}

func reconnectTestService(pinned ...int) *ClientServiceProvider {
	allowed := make([]interface{}, 0, len(pinned))
	for _, p := range pinned {
		allowed = append(allowed, p)
	}
	now := time.Now()
	return NewClientService(
		&clienttunnel.InternalTunnelProxyConfig{},
		ports.NewPortDistributor(mapset.NewSetFromSlice(allowed)),
		NewClientRepository([]*clientdata.Client{{
			ID:             "client-1",
			ClientAuthID:   "client-auth-1",
			DisconnectedAt: &now,
		}}, &hour, testLog),
		testLog,
		nil,
	)
}

func connectForReconnectTest(t *testing.T, cs *ClientServiceProvider, sourcePort int, remotes ...*models.Remote) *clientdata.Client {
	t.Helper()
	conn := test.NewConnMock()
	conn.ReturnRemoteAddr = &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: sourcePort}
	// The server asks a reconnecting agent whether it still allows each
	// tunnel it is about to re-establish; this agent allows all of them.
	conn.ReturnOk = true
	conn.ReturnResponsePayload = []byte(`{"IsAllowed":true}`)
	c, err := cs.StartClient(
		context.Background(), "client-auth-1", "client-1", conn, false,
		&chshare.ConnectionRequest{SessionID: "session-1", Version: "0.7.0", Remotes: remotes}, testLog)
	require.NoError(t, err)
	return c
}

func protectedRemote(t *testing.T, port int) *models.Remote {
	t.Helper()
	r, err := models.NewRemote(fmt.Sprintf("%d:127.0.0.1:8080", port))
	require.NoError(t, err)
	r.Scheme = strPtr("http")
	r.HTTPProxy = true
	r.AuthUser = "user1"
	r.AuthPassword = "a-tunnel-password"
	r.Owner = "admin"
	r.SkipTLSVerify = true
	return r
}

func strPtr(s string) *string { return &s }

// A tunnel made through the API keeps what the API set on it when the agent
// reconnects. The fields that only the server may set are cleared on remotes
// the agent itself asks for; they must not be cleared on tunnels the server
// re-establishes from its own record, or a password-protected proxy tunnel
// comes back without its password after any dropped connection.
func TestReconnectKeepsServerSideTunnelFields(t *testing.T) {
	port := freeTCPPort(t)
	cs := reconnectTestService(port)

	first := connectForReconnectTest(t, cs, 2345)
	_, err := cs.StartClientTunnels(first, []*models.Remote{protectedRemote(t, port)})
	require.NoError(t, err)
	require.Len(t, first.GetTunnels(), 1)
	require.Equal(t, "user1", first.GetTunnels()[0].AuthUser)

	first.SetDisconnectedNow()

	second := connectForReconnectTest(t, cs, 2346)
	require.Len(t, second.GetTunnels(), 1)
	got := second.GetTunnels()[0]
	assert.Equal(t, "user1", got.AuthUser)
	assert.Equal(t, "a-tunnel-password", got.AuthPassword)
	assert.Equal(t, "admin", got.Owner)
	assert.True(t, got.SkipTLSVerify)
	assert.True(t, got.HTTPProxy)
}

// An agent cannot take a tunnel the API configured and replace it with its
// own, unprotected one by asking for the same remote when it reconnects.
func TestReconnectAgentRemoteDoesNotReplaceServerSideTunnel(t *testing.T) {
	port := freeTCPPort(t)
	cs := reconnectTestService(port)

	first := connectForReconnectTest(t, cs, 2345)
	_, err := cs.StartClientTunnels(first, []*models.Remote{protectedRemote(t, port)})
	require.NoError(t, err)
	first.SetDisconnectedNow()

	same, err := models.NewRemote(fmt.Sprintf("%d:127.0.0.1:8080", port))
	require.NoError(t, err)
	second := connectForReconnectTest(t, cs, 2346, same)

	require.Len(t, second.GetTunnels(), 1)
	got := second.GetTunnels()[0]
	assert.Equal(t, "user1", got.AuthUser)
	assert.Equal(t, "a-tunnel-password", got.AuthPassword)
	assert.Equal(t, "admin", got.Owner)
	assert.True(t, got.HTTPProxy)
}

// The agent's own remotes are still cleaned on a reconnect: what it sends in
// the server-only fields is dropped, also when the server re-establishes
// other tunnels in the same step.
func TestReconnectStillCleansAgentRemotes(t *testing.T) {
	apiPort, agentPort := freeTCPPort(t), freeTCPPort(t)
	cs := reconnectTestService(apiPort, agentPort)

	agentRemote := func() *models.Remote {
		r, err := models.NewRemote(fmt.Sprintf("%d:127.0.0.1:22", agentPort))
		require.NoError(t, err)
		r.AuthUser = "set-by-agent"
		r.AuthPassword = "set-by-agent"
		r.Owner = "set-by-agent"
		r.TunnelURL = "https://set-by-agent.example.com"
		r.SkipTLSVerify = true
		return r
	}

	first := connectForReconnectTest(t, cs, 2345, agentRemote())
	_, err := cs.StartClientTunnels(first, []*models.Remote{protectedRemote(t, apiPort)})
	require.NoError(t, err)
	require.Len(t, first.GetTunnels(), 2)
	first.SetDisconnectedNow()

	second := connectForReconnectTest(t, cs, 2346, agentRemote())
	require.Len(t, second.GetTunnels(), 2)
	for _, tun := range second.GetTunnels() {
		if tun.LocalPort == fmt.Sprint(agentPort) {
			assert.Empty(t, tun.AuthUser)
			assert.Empty(t, tun.AuthPassword)
			assert.Empty(t, tun.Owner)
			assert.Empty(t, tun.TunnelURL)
			assert.False(t, tun.SkipTLSVerify)
		} else {
			assert.Equal(t, "user1", tun.AuthUser)
			assert.Equal(t, "a-tunnel-password", tun.AuthPassword)
		}
	}
}

// A tunnel published under its own subdomain keeps its URL across a
// reconnect, and its route in the downstream proxy is created again: the
// route of the previous connection is removed when its listener is released.
func TestReconnectRecreatesSubdomainRoute(t *testing.T) {
	// The public port of the proxy and the port of the tunnel behind it.
	proxyPort, innerPort := freeTCPPort(t), freeTCPPort(t)
	now := time.Now()
	caddyAPI := &MockCaddyAPI{}
	cs := NewClientService(
		&clienttunnel.InternalTunnelProxyConfig{
			Enabled:  true,
			CertFile: "../../testdata/certs/proxiport.test.crt",
			KeyFile:  "../../testdata/certs/proxiport.test.key",
		},
		ports.NewPortDistributor(mapset.NewSetFromSlice([]interface{}{proxyPort, innerPort})),
		NewClientRepository([]*clientdata.Client{{
			ID:             "client-1",
			ClientAuthID:   "client-auth-1",
			DisconnectedAt: &now,
		}}, &hour, testLog),
		testLog,
		nil,
	)
	cs.caddyAPI = caddyAPI

	published, err := models.NewRemote(fmt.Sprintf("%d:127.0.0.1:8080", proxyPort))
	require.NoError(t, err)
	published.Scheme = strPtr("http")
	published.HTTPProxy = true
	published.TunnelURL = "https://12345678.tunnels.proxiport.test"

	first := connectForReconnectTest(t, cs, 2345)
	_, err = cs.StartClientTunnels(first, []*models.Remote{published})
	require.NoError(t, err)
	require.NotNil(t, caddyAPI.NewRouteRequest)

	first.SetDisconnectedNow()
	caddyAPI.NewRouteRequest = nil

	second := connectForReconnectTest(t, cs, 2346)
	require.Len(t, second.GetTunnels(), 1)
	assert.Equal(t, "https://12345678.tunnels.proxiport.test", second.GetTunnels()[0].TunnelURL)
	assert.Equal(t, "12345678", caddyAPI.DeletedRouteID, "the previous connection's route is removed")
	require.NotNil(t, caddyAPI.NewRouteRequest, "the route is created again for the new connection")
	assert.Equal(t, "12345678", caddyAPI.NewRouteRequest.RouteID)

	require.NoError(t, cs.TerminateTunnel(second, second.GetTunnels()[0], true))
}

// A proxied tunnel's public port is free again once the tunnel is closed.
// The port is set aside while the proxy is being started so that nothing
// else is given it; that must end when the proxy has bound it, or the port
// stays unavailable after the tunnel is gone.
func TestProxiedTunnelPortIsFreeAfterTermination(t *testing.T) {
	proxyPort, innerPort := freeTCPPort(t), freeTCPPort(t)
	now := time.Now()
	cs := NewClientService(
		&clienttunnel.InternalTunnelProxyConfig{
			Enabled:  true,
			CertFile: "../../testdata/certs/proxiport.test.crt",
			KeyFile:  "../../testdata/certs/proxiport.test.key",
		},
		ports.NewPortDistributor(mapset.NewSetFromSlice([]interface{}{proxyPort, innerPort})),
		NewClientRepository([]*clientdata.Client{{
			ID:             "client-1",
			ClientAuthID:   "client-auth-1",
			DisconnectedAt: &now,
		}}, &hour, testLog),
		testLog,
		nil,
	)
	c := connectForReconnectTest(t, cs, 2345)

	proxied := func() *models.Remote {
		r, err := models.NewRemote(fmt.Sprintf("%d:127.0.0.1:8080", proxyPort))
		require.NoError(t, err)
		r.Scheme = strPtr("http")
		r.HTTPProxy = true
		return r
	}

	for round := 1; round <= 3; round++ {
		tunnels, err := cs.StartClientTunnels(c, []*models.Remote{proxied()})
		require.NoError(t, err, "round %d", round)
		require.Len(t, tunnels, 1)
		require.NoError(t, cs.TerminateTunnel(c, tunnels[0], true))
	}
}
