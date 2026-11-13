package clients

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A client record as an older server wrote it, with the compiled command
// patterns stored as empty objects, loads. Before, this one field kept the
// whole server from starting on a datastore taken over from such a server.
func TestClientDetailsFromAnOlderServerLoad(t *testing.T) {
	stored := `{
		"name": "host01",
		"os": "Microsoft Windows Server 2022",
		"tunnels": [],
		"client_configuration": {
			"client": {"server": "https://proxiport.example.com"},
			"remote_commands": {
				"enabled": true,
				"allow": ["^/usr/bin/.*", "^/usr/local/bin/.*"],
				"deny": ["(\\||<|>|;|,|\\n|&)"],
				"order": ["allow", "deny"],
				"allow_regexp": [{}, {}],
				"deny_regexp": [{}]
			},
			"remote_scripts": {"enabled": false}
		}
	}`

	var details clientDetails
	require.NoError(t, details.Scan(stored))
	assert.Equal(t, "host01", details.Name)
	require.NotNil(t, details.ClientConfig)
	assert.True(t, details.ClientConfig.RemoteCommands.Enabled)
	assert.Equal(t, []string{"^/usr/bin/.*", "^/usr/local/bin/.*"}, details.ClientConfig.RemoteCommands.Allow)
	assert.Empty(t, details.ClientConfig.RemoteCommands.AllowRegexp)
}
