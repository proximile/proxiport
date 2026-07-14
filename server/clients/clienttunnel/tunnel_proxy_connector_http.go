package clienttunnel

import (
	"crypto/tls"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/gorilla/mux"
)

// TunnelProxyConnectorHTTP uses the standard ReverseProxy from package httputil to connect to HTTP/HTTPS server on tunnel endpoint
type TunnelProxyConnectorHTTP struct {
	tunnelProxy  *InternalTunnelProxy
	reverseProxy *httputil.ReverseProxy
}

func NewTunnelConnectorHTTP(tp *InternalTunnelProxy) *TunnelProxyConnectorHTTP {
	return &TunnelProxyConnectorHTTP{tunnelProxy: tp}
}

func (tc *TunnelProxyConnectorHTTP) InitRouter(router *mux.Router) *mux.Router {
	router.PathPrefix("/").HandlerFunc(tc.serveHTTP)

	if tc.tunnelProxy.Tunnel.Remote.HostHeader != "" {
		tc.tunnelProxy.Logger.Debugf("using host header %s", tc.tunnelProxy.Tunnel.HostHeader)
		router.Use(tc.addHostHeader)
	}

	tc.createReverseProxy()

	return router
}

func (tc *TunnelProxyConnectorHTTP) createReverseProxy() {
	tunnelURL := url.URL{
		Scheme: *tc.tunnelProxy.Tunnel.Remote.Scheme,
		Host:   tc.tunnelProxy.TunnelAddr(),
	}

	tc.tunnelProxy.Logger.Infof("create https reverse proxy with ssl offloading forwarding to %s", tunnelURL.String())
	tc.reverseProxy = httputil.NewSingleHostReverseProxy(&tunnelURL)
	tc.reverseProxy.Transport = &http.Transport{
		TLSClientConfig: tc.targetTLSConfig(),
	}
	tc.reverseProxy.ErrorHandler = tc.tunnelProxy.handleProxyError
}

// targetTLSConfig builds the TLS config the offloading proxy uses to
// re-originate to the tunnel target. The dial address is the loopback end of
// the SSH tunnel, so the certificate the target presents can only be verified
// against the target's real hostname — ServerName is set to that (the operator
// host_header when given, otherwise the tunnel's remote host), never the
// 127.0.0.1 dial address. Verification is on by default; an operator can opt a
// single tunnel out (for a target with a self-signed cert) with the
// skip_tls_verify flag, and can supply a private CA bundle server-wide via
// tunnel_proxy_target_ca_file.
func (tc *TunnelProxyConnectorHTTP) targetTLSConfig() *tls.Config {
	remote := tc.tunnelProxy.Tunnel.Remote

	serverName := remote.HostHeader
	if serverName == "" {
		serverName = remote.RemoteHost
	}
	// A ServerName must be a bare host; strip any port that rode in on a
	// host_header so verification matches the certificate's SAN.
	if host, _, err := net.SplitHostPort(serverName); err == nil {
		serverName = host
	}

	cfg := &tls.Config{
		ServerName: serverName,
		RootCAs:    tc.tunnelProxy.Config.TargetCAPool(),
	}
	if remote.SkipTLSVerify {
		cfg.InsecureSkipVerify = true //nolint:gosec // explicit, per-tunnel operator opt-out for self-signed targets
		tc.tunnelProxy.Logger.Infof("target TLS certificate verification DISABLED for this tunnel (skip_tls_verify)")
	}
	return cfg
}

func (tc *TunnelProxyConnectorHTTP) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if tc.tunnelProxy.Tunnel.Remote.AuthUser != "" && tc.tunnelProxy.Tunnel.Remote.AuthPassword != "" {
		user, password, ok := r.BasicAuth()
		if !ok || user != tc.tunnelProxy.Tunnel.Remote.AuthUser || password != tc.tunnelProxy.Tunnel.Remote.AuthPassword {
			w.Header().Set("WWW-Authenticate", `Basic realm="restricted", charset="UTF-8"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
	}
	tc.reverseProxy.ServeHTTP(w, r)
}

func (tc *TunnelProxyConnectorHTTP) addHostHeader(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set("Host", tc.tunnelProxy.Tunnel.Remote.HostHeader)
		r.Host = tc.tunnelProxy.Tunnel.Remote.HostHeader

		next.ServeHTTP(w, r)
	})
}
