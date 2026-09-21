package clienttunnel

import (
	"net"
	"net/netip"
	"sync"
	"time"
)

const (
	// udpPeerTTL is how long the server keeps accepting the agent's replies to
	// a peer after that peer's last datagram. UDP has no connections, so this
	// stands in for one: long enough for a slow request/response exchange,
	// short enough that an address does not stay usable indefinitely.
	udpPeerTTL = 2 * time.Minute

	// maxTrackedUDPPeers bounds the table. Anyone who can reach the tunnel
	// port can put an entry in it, including with a spoofed source, so it must
	// not be allowed to grow without limit.
	maxTrackedUDPPeers = 4096
)

// udpPeerTable remembers the addresses a UDP tunnel has actually received
// datagrams from.
//
// The outbound path takes its destination from the agent, which is the peer
// the tunnel exists to serve but is not trusted: the server may be talking to
// a compromised agent, and its tunnel socket is unconnected and wildcard-bound,
// so WriteToUDP accepts any destination at all. Restricting outbound
// datagrams to addresses that have sent one is what makes the tunnel a reverse
// proxy rather than a relay -- an honest agent only ever echoes back the
// address it was given.
type udpPeerTable struct {
	mtx  sync.Mutex
	seen map[string]time.Time
	now  func() time.Time // overridable in tests
}

func newUDPPeerTable() *udpPeerTable {
	return &udpPeerTable{seen: make(map[string]time.Time), now: time.Now}
}

// udpPeerKey normalizes an address so the key does not depend on which of the
// two representations of an IPv4 address the kernel or the gob round-trip
// happened to produce. An address that cannot be represented is given no key,
// and so is never known.
func udpPeerKey(addr *net.UDPAddr) string {
	if addr == nil || addr.Port < 0 || addr.Port > 65535 {
		return ""
	}

	ip, ok := netip.AddrFromSlice(addr.IP)
	if !ok {
		return ""
	}
	ip = ip.Unmap()
	if addr.Zone != "" {
		ip = ip.WithZone(addr.Zone)
	}

	return netip.AddrPortFrom(ip, uint16(addr.Port)).String()
}

// remember records that a datagram arrived from addr.
func (p *udpPeerTable) remember(addr *net.UDPAddr) {
	key := udpPeerKey(addr)
	if key == "" {
		return
	}

	p.mtx.Lock()
	defer p.mtx.Unlock()

	now := p.now()
	if _, exists := p.seen[key]; !exists && len(p.seen) >= maxTrackedUDPPeers {
		p.evictLocked(now)
	}
	p.seen[key] = now
}

// known reports whether addr sent a datagram recently enough to still be
// replied to.
func (p *udpPeerTable) known(addr *net.UDPAddr) bool {
	key := udpPeerKey(addr)
	if key == "" {
		return false
	}

	p.mtx.Lock()
	defer p.mtx.Unlock()

	last, ok := p.seen[key]
	if !ok {
		return false
	}
	if p.now().Sub(last) > udpPeerTTL {
		delete(p.seen, key)
		return false
	}
	return true
}

// evictLocked makes room in a full table: expired entries first, and if that
// frees nothing, the least recently used one. Dropping an entry only costs a
// peer its replies until it sends again, whereas letting the table grow is
// unbounded memory driven by whoever can reach the port.
func (p *udpPeerTable) evictLocked(now time.Time) {
	for key, last := range p.seen {
		if now.Sub(last) > udpPeerTTL {
			delete(p.seen, key)
		}
	}
	if len(p.seen) < maxTrackedUDPPeers {
		return
	}

	oldestKey, oldest := "", time.Time{}
	for key, last := range p.seen {
		if oldestKey == "" || last.Before(oldest) {
			oldestKey, oldest = key, last
		}
	}
	if oldestKey != "" {
		delete(p.seen, oldestKey)
	}
}
