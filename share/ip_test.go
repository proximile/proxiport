package chshare

import (
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRemoteIP(t *testing.T) {
	testCases := []struct {
		Name           string
		TrustedProxies []string
		RemoteAddr     string
		XForwardedFor  string
		ExpectedIP     string
	}{
		{
			// Secure default: with no trusted proxies, X-Forwarded-For is
			// ignored and the socket peer is reported.
			Name:       "no header",
			RemoteAddr: "192.168.0.13:1234",
			ExpectedIP: "192.168.0.13",
		},
		{
			// A client cannot spoof its source with X-Forwarded-For when no
			// trusted proxy is configured.
			Name:          "spoofed xff ignored by default",
			RemoteAddr:    "203.0.113.7:5555",
			XForwardedFor: "8.8.8.8",
			ExpectedIP:    "203.0.113.7",
		},
		{
			// Even when the peer happens to be private, an untrusted peer's
			// forwarded header is not honored.
			Name:          "spoofed xff ignored for private peer",
			RemoteAddr:    "192.168.0.13:1234",
			XForwardedFor: "8.8.8.8",
			ExpectedIP:    "192.168.0.13",
		},
		{
			// Peer is a trusted proxy: honor X-Forwarded-For.
			Name:           "trusted proxy honors xff",
			TrustedProxies: []string{"10.0.0.0/8"},
			RemoteAddr:     "10.1.2.3:4444",
			XForwardedFor:  "8.8.8.8",
			ExpectedIP:     "8.8.8.8",
		},
		{
			// Chain of trusted proxies: return the closest address that is not
			// itself a trusted proxy.
			Name:           "trusted proxy chain returns closest untrusted",
			TrustedProxies: []string{"10.0.0.0/8"},
			RemoteAddr:     "10.1.2.3:4444",
			XForwardedFor:  "8.8.8.8, 10.9.9.9",
			ExpectedIP:     "8.8.8.8",
		},
		{
			// A forged inner hop cannot escape the trusted chain: the real
			// client is the closest untrusted address from the right.
			Name:           "forged inner hop does not win",
			TrustedProxies: []string{"10.0.0.0/8"},
			RemoteAddr:     "10.1.2.3:4444",
			XForwardedFor:  "1.1.1.1, 8.8.8.8",
			ExpectedIP:     "8.8.8.8",
		},
		{
			// Single trusted proxy by exact IP.
			Name:           "single trusted proxy ip",
			TrustedProxies: []string{"127.0.0.1"},
			RemoteAddr:     "127.0.0.1:9999",
			XForwardedFor:  "203.0.113.9",
			ExpectedIP:     "203.0.113.9",
		},
		{
			// Trusted proxy with no X-Forwarded-For falls back to the peer.
			Name:           "trusted proxy without xff",
			TrustedProxies: []string{"10.0.0.0/8"},
			RemoteAddr:     "10.1.2.3:4444",
			ExpectedIP:     "10.1.2.3",
		},
		{
			// Every forwarded hop is a trusted proxy: fall back to the peer.
			Name:           "all hops trusted falls back to peer",
			TrustedProxies: []string{"10.0.0.0/8"},
			RemoteAddr:     "10.1.2.3:4444",
			XForwardedFor:  "10.9.9.9, 10.8.8.8",
			ExpectedIP:     "10.1.2.3",
		},
	}
	for _, tc := range testCases {
		tc := tc
		t.Run(tc.Name, func(t *testing.T) {
			require.NoError(t, SetTrustedProxies(tc.TrustedProxies))
			t.Cleanup(func() { _ = SetTrustedProxies(nil) })

			req := httptest.NewRequest("GET", "/", nil)
			req.RemoteAddr = tc.RemoteAddr
			if tc.XForwardedFor != "" {
				req.Header.Set("X-Forwarded-For", tc.XForwardedFor)
			}

			assert.Equal(t, tc.ExpectedIP, RemoteIP(req))
		})
	}
}

func TestSetTrustedProxies(t *testing.T) {
	t.Cleanup(func() { _ = SetTrustedProxies(nil) })

	assert.NoError(t, SetTrustedProxies([]string{"10.0.0.0/8", "192.0.2.1", "  ", "fc00::/7"}))
	assert.Error(t, SetTrustedProxies([]string{"not-an-ip"}))
	assert.Error(t, SetTrustedProxies([]string{"10.0.0.0/33"}))
	assert.NoError(t, SetTrustedProxies(nil))
}
