package ports

import (
	"fmt"
	"sync"

	mapset "github.com/deckarep/golang-set"
	"github.com/shirou/gopsutil/v3/net"

	"github.com/proximile/proxiport/share/models"
)

type PortDistributor struct {
	allowedPorts mapset.Set

	portsPools map[string]mapset.Set
	// reserved holds ports handed out by GetRandomPort that the caller may not
	// have bound yet. They are kept out of every rebuilt pool so a concurrent
	// Refresh cannot re-hand a port before its listener is bound. A reserved
	// port is dropped again once refresh observes it as busy (i.e. bound), so
	// that once its tunnel closes a later refresh can return it to the pool.
	reserved mapset.Set
	mu       sync.RWMutex
}

func NewPortDistributor(allowedPorts mapset.Set) *PortDistributor {
	return &PortDistributor{
		allowedPorts: allowedPorts,
		portsPools:   make(map[string]mapset.Set),
		reserved:     mapset.NewSet(),
	}
}

// NewPortDistributorForTests is used only for unit-testing.
func NewPortDistributorForTests(allowedPorts, tcpPortsPool, udpPortsPool mapset.Set) *PortDistributor {
	return &PortDistributor{
		allowedPorts: allowedPorts,
		portsPools: map[string]mapset.Set{
			models.ProtocolTCP: tcpPortsPool,
			models.ProtocolUDP: udpPortsPool,
		},
		reserved: mapset.NewSet(),
	}
}

func (d *PortDistributor) GetRandomPort(protocol string) (int, error) {
	subProtocols := []string{protocol}
	if protocol == models.ProtocolTCPUDP {
		subProtocols = []string{models.ProtocolTCP, models.ProtocolUDP}
	}
	// Initialize any missing sub-pool before taking the allocation lock:
	// refresh() acquires d.mu itself, so it must not run inside the critical
	// section below.
	for _, p := range subProtocols {
		if d.getPoolFromMap(p) == nil {
			if err := d.refresh(p); err != nil {
				return 0, err
			}
		}
	}

	// Hold d.mu across the whole pop-and-remove so two concurrent callers can
	// never be handed the same local port, and so a Refresh() cannot rebuild a
	// pool in the middle of the selection. We choose from the real shared
	// pool(s) — for tcp+udp only ports present in both qualify — remove the
	// chosen port from every underlying pool, and record it as reserved so a
	// concurrent Refresh cannot re-add it before the caller binds it.
	d.mu.Lock()
	defer d.mu.Unlock()

	pools := make([]mapset.Set, 0, len(subProtocols))
	var candidates mapset.Set
	for _, p := range subProtocols {
		pool := d.portsPools[p]
		if pool == nil {
			return 0, fmt.Errorf("no ports available")
		}
		pools = append(pools, pool)
		if candidates == nil {
			candidates = pool.Clone()
		} else {
			candidates = candidates.Intersect(pool)
		}
	}

	port := candidates.Pop()
	if port == nil {
		return 0, fmt.Errorf("no ports available")
	}

	// Make sure the port is removed from all pools for tcp+udp protocol.
	for _, pool := range pools {
		pool.Remove(port)
	}
	d.reserved.Add(port)

	return port.(int), nil
}

func (d *PortDistributor) IsPortAllowed(port int) bool {
	return d.allowedPorts.Contains(port)
}

func (d *PortDistributor) IsPortBusy(protocol string, port int) bool {
	return !d.getPool(protocol).Contains(port)
}

func (d *PortDistributor) getPool(protocol string) mapset.Set {
	if protocol == models.ProtocolTCPUDP {
		// Read both sub-pools through getPoolFromMap so the underlying map is
		// only ever touched under d.mu. Indexing d.portsPools directly here
		// races with setPool's locked write (concurrent map read+write is a Go
		// fatal error that crashes the whole server). Intersect then operates on
		// the two set references, which carry their own locking.
		tcpPool := d.getPoolFromMap(models.ProtocolTCP)
		udpPool := d.getPoolFromMap(models.ProtocolUDP)
		if tcpPool == nil || udpPool == nil {
			return nil
		}
		return tcpPool.Intersect(udpPool)
	}
	return d.getPoolFromMap(protocol)
}

func (d *PortDistributor) Refresh() error {
	err := d.refresh(models.ProtocolTCP)
	if err != nil {
		return err
	}
	err = d.refresh(models.ProtocolUDP)
	if err != nil {
		return err
	}
	return nil
}

func (d *PortDistributor) refresh(protocol string) error {
	busyPorts, err := ListBusyPorts(protocol)
	if err != nil {
		return err
	}

	// Read the reserved set and write the rebuilt pool under the same lock that
	// GetRandomPort holds while it removes a port and reserves it, so the two
	// stay consistent: a reserved-but-not-yet-bound port is never re-added to a
	// pool. A reserved port that now shows as busy has been bound, so drop it
	// from the reserved set — once its tunnel closes a later refresh returns it
	// to the pool.
	d.mu.Lock()
	defer d.mu.Unlock()

	for _, p := range d.reserved.ToSlice() {
		if busyPorts.Contains(p) {
			d.reserved.Remove(p)
		}
	}
	d.portsPools[protocol] = d.allowedPorts.Difference(busyPorts).Difference(d.reserved)

	return nil
}

func ListBusyPorts(protocol string) (mapset.Set, error) {
	result := mapset.NewSet()
	connections, err := net.Connections(protocol)
	if err != nil {
		return nil, err
	}

	for _, c := range connections {
		isActive := c.Status == "LISTEN" || c.Status == "NONE" || c.Status == ""
		if isActive && c.Laddr.Port != 0 {
			result.Add(int(c.Laddr.Port))
		}
	}

	return result, nil
}

func (d *PortDistributor) getPoolFromMap(protocol string) (pool mapset.Set) {
	d.mu.RLock()
	pool = d.portsPools[protocol]
	d.mu.RUnlock()
	return pool
}

func (d *PortDistributor) setPool(protocol string, pool mapset.Set) {
	d.mu.Lock()
	d.portsPools[protocol] = pool
	d.mu.Unlock()
}
