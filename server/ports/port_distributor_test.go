package ports

import (
	"sync"
	"testing"

	mapset "github.com/deckarep/golang-set"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/proximile/proxiport/share/models"
)

func TestPortDistributor(t *testing.T) {

	for _, protocol := range []string{models.ProtocolTCP, models.ProtocolUDP, models.ProtocolTCPUDP} {
		t.Run(protocol, func(t *testing.T) {
			pd := NewPortDistributorForTests(
				mapset.NewSetFromSlice([]interface{}{1, 2, 3, 4, 5}),
				mapset.NewSetFromSlice([]interface{}{2, 3, 4, 5}),
				mapset.NewSetFromSlice([]interface{}{2, 3, 4, 5}),
			)

			busy, err := pd.IsPortBusy(protocol, 1)
			require.NoError(t, err)
			assert.Equal(t, true, busy)

			busy, err = pd.IsPortBusy(protocol, 2)
			require.NoError(t, err)
			assert.Equal(t, false, busy)

			port, err := pd.GetRandomPort(protocol)
			require.NoError(t, err)

			busy, err = pd.IsPortBusy(protocol, port)
			require.NoError(t, err)
			assert.Equal(t, true, busy)
		})
	}
}

// TestPortDistributor_ConcurrentTCPUDP exercises the tcp+udp read path
// (getPool -> Intersect) concurrently with pool replacement (setPool, as done
// by Refresh on every tunnel create). Before getPool read both sub-pools under
// the lock, this indexed d.portsPools directly while setPool wrote it under
// d.mu, so `go test -race` (and, unguarded, the Go runtime's "concurrent map
// read and map write" fatal error) would fire and crash the server. It must now
// run clean.
func TestPortDistributor_ConcurrentTCPUDP(t *testing.T) {
	pd := NewPortDistributorForTests(
		mapset.NewSetFromSlice([]interface{}{1, 2, 3, 4, 5}),
		mapset.NewSetFromSlice([]interface{}{2, 3, 4, 5}),
		mapset.NewSetFromSlice([]interface{}{2, 3, 4, 5}),
	)

	const iterations = 2000
	var wg sync.WaitGroup

	// Writers: replace the sub-pool map entries, mimicking Refresh()/setPool.
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				pd.setPool(models.ProtocolTCP, mapset.NewSetFromSlice([]interface{}{2, 3, 4, 5}))
				pd.setPool(models.ProtocolUDP, mapset.NewSetFromSlice([]interface{}{2, 3, 4, 5}))
			}
		}()
	}

	// Readers: hit the tcp+udp intersect path and IsPortBusy concurrently.
	for r := 0; r < 4; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				_, _ = pd.getPool(models.ProtocolTCPUDP)
				_, _ = pd.IsPortBusy(models.ProtocolTCPUDP, 3)
			}
		}()
	}

	wg.Wait()
}

// A tunnel with an internal proxy binds the operator's pinned port for the
// proxy and a random port for the tunnel behind it. checkLocalPort only
// *checks* the pinned port -- nothing removes it from the pool -- so
// GetRandomPort could hand the inner tunnel the very port the proxy was about
// to bind, and the proxy's bind then failed with EADDRINUSE. That failure used
// to be invisible (it happened inside a goroutine and was Debug-logged), so the
// API answered 200 with a tunnel whose proxy never listened.
func TestReserveKeepsAPortOutOfRandomAllocation(t *testing.T) {
	for _, protocol := range []string{models.ProtocolTCP, models.ProtocolUDP, models.ProtocolTCPUDP} {
		t.Run(protocol, func(t *testing.T) {
			pd := NewPortDistributorForTests(
				mapset.NewSetFromSlice([]interface{}{1, 2, 3}),
				mapset.NewSetFromSlice([]interface{}{1, 2, 3}),
				mapset.NewSetFromSlice([]interface{}{1, 2, 3}),
			)

			pd.Reserve(protocol, 2)

			// Drain the pool: the reserved port must never come back out.
			for i := 0; i < 3; i++ {
				port, err := pd.GetRandomPort(protocol)
				if err != nil {
					break
				}
				assert.NotEqual(t, 2, port, "handed out a port that was reserved for a caller about to bind it")
			}
		})
	}
}

// A protocol the agent invented used to index portsPools -- keyed only "tcp"
// and "udp" -- to a nil mapset.Set interface, and IsPortBusy called Contains on
// it. That panic landed in the connection handler, after the reconnect path had
// already force-terminated the client id's tunnels and marked it connected, so
// the genuine agent was refused with "client is already connected" until the
// next status-check sweep (5 minutes by default). The protocol is attacker-
// chosen: sanitizeAgentRemotes deliberately leaves it alone.
func TestIsPortBusyRejectsUnknownProtocolInsteadOfPanicking(t *testing.T) {
	pd := NewPortDistributorForTests(
		mapset.NewSetFromSlice([]interface{}{1, 2, 3, 4, 5}),
		mapset.NewSetFromSlice([]interface{}{2, 3, 4, 5}),
		mapset.NewSetFromSlice([]interface{}{2, 3, 4, 5}),
	)

	for _, protocol := range []string{"", "sctp", "TCP", "tcp+udp+icmp", "tcp ", "../tcp"} {
		t.Run("protocol="+protocol, func(t *testing.T) {
			require.NotPanics(t, func() {
				_, err := pd.IsPortBusy(protocol, 20001)
				require.Error(t, err)
				assert.Contains(t, err.Error(), "unsupported protocol")
			})
		})
	}
}

// A valid protocol whose pool has not been refreshed yet reached the same nil.
func TestIsPortBusyReportsAnUnrefreshedPool(t *testing.T) {
	pd := NewPortDistributor(mapset.NewSetFromSlice([]interface{}{1, 2, 3}))

	for _, protocol := range []string{models.ProtocolTCP, models.ProtocolUDP, models.ProtocolTCPUDP} {
		t.Run(protocol, func(t *testing.T) {
			require.NotPanics(t, func() {
				_, err := pd.IsPortBusy(protocol, 1)
				require.Error(t, err)
				assert.Contains(t, err.Error(), "not been refreshed")
			})
		})
	}
}

// gopsutil's Connections rejects "tcp+udp", so handing the protocol through
// verbatim made every tcp+udp tunnel on a pinned local port fail with a 500.
// A port is busy for tcp+udp if it is busy for either half.
func TestListBusyPortsHandlesTCPUDP(t *testing.T) {
	busy, err := ListBusyPorts(models.ProtocolTCPUDP)
	require.NoError(t, err, "tcp+udp must not be handed to gopsutil verbatim")
	require.NotNil(t, busy)

	_, err = ListBusyPorts("sctp")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported protocol")
}
