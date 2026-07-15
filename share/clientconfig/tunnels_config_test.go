package clientconfig_test

import (
	"strings"
	"testing"

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
