package ports

import (
	"sync"
	"testing"

	mapset "github.com/deckarep/golang-set"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/proximile/proxiport/share/models"
)

// TestPortDistributor_ConcurrentAllocationNoDuplicates verifies that
// concurrent GetRandomPort callers are never handed the same local port, for
// every protocol including the tcp+udp path that previously popped from a
// throwaway Intersect copy without holding the allocation lock. Run with
// -race to also catch unsynchronised pool access.
func TestPortDistributor_ConcurrentAllocationNoDuplicates(t *testing.T) {
	for _, protocol := range []string{models.ProtocolTCP, models.ProtocolUDP, models.ProtocolTCPUDP} {
		protocol := protocol
		t.Run(protocol, func(t *testing.T) {
			const poolSize = 1000

			ports := make([]interface{}, 0, poolSize)
			for p := 1; p <= poolSize; p++ {
				ports = append(ports, p)
			}
			pd := NewPortDistributorForTests(
				mapset.NewSetFromSlice(ports),
				mapset.NewSetFromSlice(ports),
				mapset.NewSetFromSlice(ports),
			)

			var (
				mu   sync.Mutex
				seen = make(map[int]int)
				wg   sync.WaitGroup
			)
			// Exactly poolSize goroutines contend for poolSize ports: with the
			// allocation lock held across pop-and-remove every call must succeed
			// and every port must be unique.
			for i := 0; i < poolSize; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					port, err := pd.GetRandomPort(protocol)
					if err != nil {
						return
					}
					mu.Lock()
					seen[port]++
					mu.Unlock()
				}()
			}
			wg.Wait()

			require.Len(t, seen, poolSize)
			for port, count := range seen {
				assert.Equalf(t, 1, count, "port %d handed out %d times", port, count)
			}

			// The pool is now exhausted.
			_, err := pd.GetRandomPort(protocol)
			assert.Error(t, err)
		})
	}
}
