package clienttunnel

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/proximile/proxiport/share/logger"
	"github.com/proximile/proxiport/share/models"
)

func testConnector(t *testing.T, remote models.Remote, cfg *InternalTunnelProxyConfig) *TunnelProxyConnectorHTTP {
	t.Helper()
	if cfg == nil {
		cfg = &InternalTunnelProxyConfig{}
	}
	tp := &InternalTunnelProxy{
		Tunnel: &Tunnel{Remote: remote},
		Config: cfg,
		Logger: logger.NewLogger("connector-test", logger.LogOutput{File: os.Stdout}, logger.LogLevelError),
	}
	return NewTunnelConnectorHTTP(tp)
}

// The offloading proxy dials the loopback end of the SSH tunnel, so the target
// certificate can only be verified against the target's real hostname. These
// tests pin that the ServerName is derived from the tunnel target (never the
// 127.0.0.1 dial address) and that verification is on unless the operator opts
// a single tunnel out.
func TestTargetTLSConfig(t *testing.T) {
	t.Run("verifies by default with target host as ServerName", func(t *testing.T) {
		tc := testConnector(t, models.Remote{RemoteHost: "internal.example.com", RemotePort: "8443"}, nil)
		cfg := tc.targetTLSConfig()
		assert.False(t, cfg.InsecureSkipVerify, "verification must be on by default")
		assert.Equal(t, "internal.example.com", cfg.ServerName, "ServerName must be the real target host, not the loopback dial address")
	})

	t.Run("host_header overrides ServerName", func(t *testing.T) {
		tc := testConnector(t, models.Remote{RemoteHost: "10.0.0.5", RemotePort: "8443", HostHeader: "app.example.com"}, nil)
		cfg := tc.targetTLSConfig()
		assert.Equal(t, "app.example.com", cfg.ServerName)
	})

	t.Run("host_header port is stripped for SNI", func(t *testing.T) {
		tc := testConnector(t, models.Remote{RemoteHost: "10.0.0.5", RemotePort: "8443", HostHeader: "app.example.com:8443"}, nil)
		cfg := tc.targetTLSConfig()
		assert.Equal(t, "app.example.com", cfg.ServerName)
	})

	t.Run("skip_tls_verify opts a single tunnel out", func(t *testing.T) {
		tc := testConnector(t, models.Remote{RemoteHost: "self-signed.example.com", RemotePort: "8443", SkipTLSVerify: true}, nil)
		cfg := tc.targetTLSConfig()
		assert.True(t, cfg.InsecureSkipVerify)
	})

	t.Run("configured CA pool is used as RootCAs", func(t *testing.T) {
		cfg := &InternalTunnelProxyConfig{
			Host:         "example.com",
			EnableAcme:   true,
			TargetCAFile: "../../../testdata/certs/proxiport.test.crt",
		}
		require.NoError(t, cfg.ParseAndValidate())
		tc := testConnector(t, models.Remote{RemoteHost: "internal.example.com", RemotePort: "8443"}, cfg)
		tlsCfg := tc.targetTLSConfig()
		assert.NotNil(t, tlsCfg.RootCAs, "the operator-configured CA bundle must be used as the trust root")
	})
}

func TestTargetCAFileValidation(t *testing.T) {
	t.Run("valid CA bundle loads a pool", func(t *testing.T) {
		cfg := &InternalTunnelProxyConfig{
			Host:         "example.com",
			EnableAcme:   true,
			TargetCAFile: "../../../testdata/certs/proxiport.test.crt",
		}
		require.NoError(t, cfg.ParseAndValidate())
		assert.NotNil(t, cfg.TargetCAPool())
	})

	t.Run("missing CA file fails closed", func(t *testing.T) {
		cfg := &InternalTunnelProxyConfig{
			Host:         "example.com",
			EnableAcme:   true,
			TargetCAFile: "../../../testdata/certs/does-not-exist.crt",
		}
		err := cfg.ParseAndValidate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "tunnel_proxy_target_ca_file")
	})

	t.Run("non-PEM CA file fails closed", func(t *testing.T) {
		f, err := os.CreateTemp(t.TempDir(), "not-a-cert-*.pem")
		require.NoError(t, err)
		_, err = f.WriteString("this is not a certificate\n")
		require.NoError(t, err)
		require.NoError(t, f.Close())

		cfg := &InternalTunnelProxyConfig{
			Host:         "example.com",
			EnableAcme:   true,
			TargetCAFile: f.Name(),
		}
		err = cfg.ParseAndValidate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no valid PEM certificates")
	})

	t.Run("no CA file means system trust store (nil pool)", func(t *testing.T) {
		cfg := &InternalTunnelProxyConfig{Host: "example.com", EnableAcme: true}
		require.NoError(t, cfg.ParseAndValidate())
		assert.Nil(t, cfg.TargetCAPool())
	})
}
