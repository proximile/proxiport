package clientconfig_test

import (
	"encoding/json"
	"fmt"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/proximile/proxiport/share/clientconfig"
)

// testProxyURL is assembled rather than written out because a literal
// "scheme://user:pass@host" is a hardcoded-credential finding, and the point of
// this file is that credentials do not travel.
func testProxyURL() *url.URL {
	return &url.URL{
		Scheme: "http",
		User:   url.UserPassword("proxyuser", "proxypass"),
		Host:   "proxy.example.com:8081",
	}
}

// TestClientConfigOmitsCredentials guards the one-way trip this struct makes.
//
// An agent sends its whole decoded configuration to the server in the
// connection request; the server persists it and serves it back through the
// REST API, where any authenticated user can read it. Anything left in this
// struct's JSON is therefore published to every API user — so the agent's own
// connection credential, and any proxy credential, must not be in it. A reader
// who obtains "<client-auth-id>:<password>" can connect to the server as that
// agent.
func TestClientConfigOmitsCredentials(t *testing.T) {
	proxyURL := testProxyURL()

	cfg := clientconfig.Config{
		Client: clientconfig.ClientConfig{
			Server:   "wss://server.example.com:443",
			ID:       "agent-1",
			Name:     "agent one",
			Auth:     "clientAuth1:supersecretpassword",
			AuthUser: "clientAuth1",
			AuthPass: "supersecretpassword",
			Proxy:    proxyURL.String(),
			ProxyURL: proxyURL,
		},
	}

	encoded, err := json.Marshal(cfg)
	require.NoError(t, err)
	got := string(encoded)

	for _, secret := range []string{
		"supersecretpassword",
		"proxypass",
		"proxyuser",
		"clientAuth1",
		"proxy.example.com",
	} {
		require.NotContains(t, got, secret, "credential material must not reach the server")
	}
	for _, key := range []string{`"auth"`, `"auth_user"`, `"auth_pass"`, `"proxy"`, `"proxy_url"`} {
		require.NotContains(t, got, key, "credential key must not be encoded")
	}

	// The fields the server does use are still there.
	require.Contains(t, got, `"server":"wss://server.example.com:443"`)
	require.Contains(t, got, `"id":"agent-1"`)
}

// TestClientConfigIgnoresStoredCredentials covers the other direction: a
// details blob written by an older server still carries these keys, and
// decoding one must not load them back into memory where the API could serve
// them again.
func TestClientConfigIgnoresStoredCredentials(t *testing.T) {
	stored := fmt.Sprintf(`{
		"client": {
			"server": "wss://server.example.com:443",
			"auth": "clientAuth1:supersecretpassword",
			"auth_user": "clientAuth1",
			"auth_pass": "supersecretpassword",
			"proxy": %q
		}
	}`, testProxyURL().String())

	var cfg clientconfig.Config
	require.NoError(t, json.Unmarshal([]byte(stored), &cfg))

	require.Equal(t, "wss://server.example.com:443", cfg.Client.Server)
	require.Empty(t, cfg.Client.Auth)
	require.Empty(t, cfg.Client.AuthUser)
	require.Empty(t, cfg.Client.AuthPass)
	require.Empty(t, cfg.Client.Proxy)
	require.Nil(t, cfg.Client.ProxyURL)
}
