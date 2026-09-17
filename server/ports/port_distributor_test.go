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

			assert.Equal(t, true, pd.IsPortBusy(protocol, 1))
			assert.Equal(t, false, pd.IsPortBusy(protocol, 2))

			port, err := pd.GetRandomPort(protocol)
			require.NoError(t, err)

			assert.Equal(t, true, pd.IsPortBusy(protocol, port))
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
				_ = pd.getPool(models.ProtocolTCPUDP)
				_ = pd.IsPortBusy(models.ProtocolTCPUDP, 3)
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
