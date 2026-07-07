package chshare

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
)

// trustedProxies is the process-wide set of upstream proxy networks whose
// X-Forwarded-For header is honored. It is set once at startup via
// SetTrustedProxies. When empty (the default), X-Forwarded-For is never
// trusted and RemoteIP returns the socket peer, so a client cannot spoof its
// source IP with a request header.
var trustedProxies atomic.Pointer[[]net.IPNet]

// SetTrustedProxies configures which upstream proxies may set X-Forwarded-For.
// Each entry is a CIDR ("10.0.0.0/8") or a single IP ("192.0.2.1"). An empty or
// nil list disables X-Forwarded-For handling entirely — the secure default —
// so RemoteIP reports the socket peer. It returns an error on a malformed
// entry and leaves the previous policy in place in that case.
func SetTrustedProxies(entries []string) error {
	nets := make([]net.IPNet, 0, len(entries))
	for _, e := range entries {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		if _, ipNet, err := net.ParseCIDR(e); err == nil {
			nets = append(nets, *ipNet)
			continue
		}
		ip := net.ParseIP(e)
		if ip == nil {
			return fmt.Errorf("invalid trusted proxy %q: expected an IP address or CIDR", e)
		}
		bits := 8 * net.IPv4len
		if ip.To4() == nil {
			bits = 8 * net.IPv6len
		}
		nets = append(nets, net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
	}
	trustedProxies.Store(&nets)
	return nil
}

func isTrustedProxy(ip net.IP) bool {
	nets := trustedProxies.Load()
	if nets == nil {
		return false
	}
	for i := range *nets {
		if (*nets)[i].Contains(ip) {
			return true
		}
	}
	return false
}

// RemoteIP returns the best available source IP for the request. It trusts only
// the socket peer (r.RemoteAddr) unless that peer is a configured trusted proxy
// (see SetTrustedProxies), in which case it walks X-Forwarded-For from right to
// left and returns the closest address that is not itself a trusted proxy — the
// real client as seen by the outermost trusted hop. Without configured trusted
// proxies X-Forwarded-For is ignored, so it cannot be used to spoof the source.
func RemoteIP(r *http.Request) string {
	peer := r.RemoteAddr
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		peer = host
	}

	peerIP := net.ParseIP(peer)
	if peerIP == nil || !isTrustedProxy(peerIP) {
		return peer
	}

	xff := r.Header.Get("X-Forwarded-For")
	if xff == "" {
		return peer
	}
	parts := strings.Split(xff, ",")
	for i := len(parts) - 1; i >= 0; i-- {
		ip := net.ParseIP(strings.TrimSpace(parts[i]))
		if ip == nil || isTrustedProxy(ip) {
			continue
		}
		return ip.String()
	}
	// Every X-Forwarded-For hop was itself a trusted proxy (or unparseable);
	// fall back to the socket peer.
	return peer
}
