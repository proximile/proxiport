package clienttunnel

import (
	"context"
	"net"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/proximile/proxiport/share/comm"
	"github.com/proximile/proxiport/share/logger"
	"github.com/proximile/proxiport/share/models"
	"github.com/proximile/proxiport/share/test"
)

func newRelayTestTunnel(t *testing.T, acl *TunnelACL) (*tunnelUDP, *comm.UDPChannel) {
	t.Helper()

	udpReadTimeout = time.Millisecond
	log := logger.NewLogger("udp-relay-test", logger.LogOutput{File: os.Stdout}, logger.LogLevelDebug)
	tunnel := newTunnelUDP(log, nil, models.Remote{}, acl)

	serverChannel, clientChannel := test.NewMockChannel()
	require.NoError(t, tunnel.start(context.Background(), serverChannel))
	t.Cleanup(func() { _ = tunnel.Terminate(false) })

	return tunnel, comm.NewUDPChannel(clientChannel)
}

// A UDP tunnel is a reverse proxy: an outside peer sends to the server's
// listener, the server forwards the datagram to the agent tagged with that
// peer's address, and the agent's reply carries the same address back. The
// server used to hand that address straight to WriteToUDP with no check, so
// the address was really a destination chosen by the agent -- and the socket
// is unconnected and wildcard-bound, so it accepted any destination at all.
//
// An agent holding one valid credential could therefore aim datagrams at the
// server's own loopback (StatsD, a local resolver, syslog) or at a
// management LAN it cannot route to itself, sourced from the server's IP.
func TestUDPTunnelDropsOutboundToAnAddressNoPeerUsed(t *testing.T) {
	_, channel := newRelayTestTunnel(t, nil)

	// Stands in for a service the agent should not be able to reach through
	// the server. Nothing ever sends to the tunnel from this address.
	victim, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	require.NoError(t, err)
	defer func() { _ = victim.Close() }()

	require.NoError(t, channel.Encode(victim.LocalAddr().(*net.UDPAddr), []byte("relayed")))

	require.NoError(t, victim.SetReadDeadline(time.Now().Add(500*time.Millisecond)))
	buf := make([]byte, 128)
	n, from, err := victim.ReadFromUDP(buf)

	require.Error(t, err, "the datagram reached %s carrying %q -- the server relayed for the agent", from, buf[:n])
	netErr, ok := err.(net.Error)
	require.True(t, ok, "expected a timeout, got %v", err)
	assert.True(t, netErr.Timeout(), "expected a read timeout, got %v", err)
}

// Anti-vacuity, and the property that must not regress: a reply to a peer the
// tunnel really did receive from still goes through. Without this the test
// above would pass just as well if the outbound path were deleted.
func TestUDPTunnelStillRepliesToAPeerThatSentFirst(t *testing.T) {
	tunnel, channel := newRelayTestTunnel(t, nil)

	peer, err := net.DialUDP("udp", nil, tunnel.conn.LocalAddr().(*net.UDPAddr))
	require.NoError(t, err)
	defer func() { _ = peer.Close() }()

	_, err = peer.Write([]byte("request"))
	require.NoError(t, err)

	addr, data, err := channel.Decode()
	require.NoError(t, err)
	require.Equal(t, []byte("request"), data)

	// The agent echoes back the address it was given, as an honest one does.
	require.NoError(t, channel.Encode(addr, []byte("response")))

	require.NoError(t, peer.SetReadDeadline(time.Now().Add(5*time.Second)))
	buf := make([]byte, 128)
	n, err := peer.Read(buf)
	require.NoError(t, err, "the reply to a real peer must still be delivered")
	assert.Equal(t, []byte("response"), buf[:n])
}

// The ACL governs which peers may use the tunnel. A peer that was allowed when
// it sent, and is then excluded by a new ACL, must not keep receiving replies:
// otherwise revoking access only half works.
func TestUDPTunnelStopsRepliesAfterTheACLExcludesThePeer(t *testing.T) {
	acl, err := ParseTunnelACL("127.0.0.1")
	require.NoError(t, err)
	tunnel, channel := newRelayTestTunnel(t, acl)

	peer, err := net.DialUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")},
		tunnel.conn.LocalAddr().(*net.UDPAddr))
	require.NoError(t, err)
	defer func() { _ = peer.Close() }()

	_, err = peer.Write([]byte("request"))
	require.NoError(t, err)

	addr, _, err := channel.Decode()
	require.NoError(t, err)

	excluding, err := ParseTunnelACL("127.0.0.2")
	require.NoError(t, err)
	tunnel.SetACL(excluding)

	require.NoError(t, channel.Encode(addr, []byte("response")))

	require.NoError(t, peer.SetReadDeadline(time.Now().Add(500*time.Millisecond)))
	buf := make([]byte, 128)
	_, err = peer.Read(buf)
	require.Error(t, err, "a peer the ACL now excludes must not receive a reply")
}
