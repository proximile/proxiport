package clientconfig_test

import (
	"strings"
	"testing"
	"time"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	chshare "github.com/proximile/proxiport/share"
	"github.com/proximile/proxiport/share/clientconfig"
)

// TestTunnelsConfigBinds guards that the [tunnels] block decodes from the config
// file. The fields previously carried only json tags and no mapstructure tags,
// so viper silently dropped reverse_proxy and host_header (their field names
// have no underscore, so field-name matching never matched the snake_case
// keys). That made the server-side TLS reverse proxy impossible to enable for
// permanent, config-defined tunnels.
func TestTunnelsConfigBinds(t *testing.T) {
	const body = `
[tunnels]
scheme = "https"
reverse_proxy = true
host_header = "app.internal"
`
	v := viper.New()
	v.SetConfigType("toml")
	var cfg clientconfig.Config
	require.NoError(t, chshare.DecodeViperConfig(v, &cfg, strings.NewReader(body)))

	assert.Equal(t, "https", cfg.Tunnels.Scheme)
	assert.True(t, cfg.Tunnels.ReverseProxy, "reverse_proxy must bind from the config file")
	assert.Equal(t, "app.internal", cfg.Tunnels.HostHeader, "host_header must bind from the config file")
}

// TestTransportsConfigBinds guards that the egress chain decodes from the
// config file. A field without a mapstructure tag is silently dropped by viper
// — the same failure the tunnels block above exists to catch — and a silently
// dropped transports list would leave the agent dialing direct while its config
// says otherwise, which is the worst possible way for this key to fail.
func TestTransportsConfigBinds(t *testing.T) {
	const body = `
[client]
transports = ["socks5h://127.0.0.1:9050", "direct"]
transport_dial_timeout = "20s"
`
	v := viper.New()
	v.SetConfigType("toml")
	var cfg clientconfig.Config
	require.NoError(t, chshare.DecodeViperConfig(v, &cfg, strings.NewReader(body)))

	assert.Equal(t, []string{"socks5h://127.0.0.1:9050", "direct"}, cfg.Client.Transports,
		"transports must bind from the config file")
	assert.Equal(t, 20*time.Second, cfg.Client.TransportDialTimeout,
		"transport_dial_timeout must bind from the config file")
}
