package clienttunnel

import (
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func udpAddr(t *testing.T, s string) *net.UDPAddr {
	t.Helper()
	a, err := net.ResolveUDPAddr("udp", s)
	require.NoError(t, err)
	return a
}

// The same IPv4 address arrives as 4 bytes from one path and as a 16-byte
// IPv4-mapped form from another, and the agent's reply goes through a gob
// round-trip in between. Keying on the raw bytes would make the reply look
// like it was for an address no peer had used.
func TestUDPPeerKeyNormalizesAddressForms(t *testing.T) {
	four := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1).To4(), Port: 5000}
	sixteen := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1).To16(), Port: 5000}

	assert.Equal(t, udpPeerKey(four), udpPeerKey(sixteen))
	assert.NotEmpty(t, udpPeerKey(four))

	// A different port is a different peer.
	assert.NotEqual(t, udpPeerKey(four), udpPeerKey(&net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 5001}))
}

// An unusable address must never be "known", rather than collapsing to a
// shared empty key that any other unusable address would also match.
func TestUDPPeerKeyRejectsUnusableAddresses(t *testing.T) {
	table := newUDPPeerTable()

	for _, addr := range []*net.UDPAddr{
		nil,
		{IP: nil, Port: 5000},
		{IP: net.IP{1, 2, 3}, Port: 5000}, // not 4 or 16 bytes
		{IP: net.IPv4(127, 0, 0, 1), Port: -1},
		{IP: net.IPv4(127, 0, 0, 1), Port: 70000},
	} {
		assert.Empty(t, udpPeerKey(addr), "%v should have no key", addr)
		table.remember(addr)
		assert.False(t, table.known(addr), "%v must never be known", addr)
	}
}

// A peer stays repliable for the TTL and no longer. UDP has no connections, so
// this stands in for one.
func TestUDPPeerTableForgetsAfterTTL(t *testing.T) {
	now := time.Now()
	table := newUDPPeerTable()
	table.now = func() time.Time { return now }

	peer := udpAddr(t, "127.0.0.1:5000")
	table.remember(peer)
	require.True(t, table.known(peer))

	now = now.Add(udpPeerTTL - time.Second)
	assert.True(t, table.known(peer), "still inside the TTL")

	now = now.Add(2 * time.Second)
	assert.False(t, table.known(peer), "past the TTL")
}

// Anyone who can reach the tunnel port can add an entry, including with a
// spoofed source, so the table must stay bounded.
func TestUDPPeerTableStaysBounded(t *testing.T) {
	now := time.Now()
	table := newUDPPeerTable()
	table.now = func() time.Time { return now }

	for i := 0; i < maxTrackedUDPPeers+500; i++ {
		table.remember(udpAddr(t, fmt.Sprintf("127.0.0.1:%d", 1024+i%60000)))
		now = now.Add(time.Millisecond)
	}

	table.mtx.Lock()
	size := len(table.seen)
	table.mtx.Unlock()

	assert.LessOrEqual(t, size, maxTrackedUDPPeers,
		"the table must not grow past its cap")

	// The most recent peer is the one worth keeping.
	assert.True(t, table.known(udpAddr(t, fmt.Sprintf("127.0.0.1:%d", 1024+(maxTrackedUDPPeers+499)%60000))))
}

// Re-sending refreshes the entry rather than leaving it to expire mid-exchange.
func TestUDPPeerTableRefreshesOnEachDatagram(t *testing.T) {
	now := time.Now()
	table := newUDPPeerTable()
	table.now = func() time.Time { return now }

	peer := udpAddr(t, "127.0.0.1:5000")
	table.remember(peer)

	now = now.Add(udpPeerTTL - time.Second)
	table.remember(peer)

	now = now.Add(udpPeerTTL - time.Second)
	assert.True(t, table.known(peer), "the second datagram should have reset the TTL")
}
