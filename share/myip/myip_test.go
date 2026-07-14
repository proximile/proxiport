package myip

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const port = 23985

// startServer serves the stub endpoints on every loopback address — GetMyIPs
// asks for the same URL over IPv4 and over IPv6 — and returns only once the
// listener is bound, so a request cannot race the bind.
func startServer(t *testing.T) {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("/good", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, userAgent, r.UserAgent())
		if strings.HasPrefix(r.RemoteAddr, "127.0.0.1") {
			// Handle the ipv4 request
			fmt.Fprintf(w, `{"ip":"127.0.0.1"}`)
			return
		}
		// Handle the ipv6 request
		fmt.Fprintf(w, `{"ip":"::1"}`)
	})
	mux.HandleFunc("/bad", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "something went wrong")
	})

	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	require.NoError(t, err)

	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 3 * time.Second,
	}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
}

func TestGetMyIps(t *testing.T) {
	startServer(t)
	ctx := context.Background()
	ips, err := GetMyIPs(ctx, fmt.Sprintf("http://localhost:%d/good", port))

	require.NoError(t, err)
	assert.Equal(t, "127.0.0.1", ips.IPv4)
	assert.Equal(t, "::1", ips.IPv6)
}

func TestGetMyIpsFailing(t *testing.T) {
	startServer(t)
	ctx := context.Background()
	_, err := GetMyIPs(ctx, fmt.Sprintf("http://localhost:%d/bad", port))

	assert.ErrorContains(t, err, "400: something went wrong")
}
