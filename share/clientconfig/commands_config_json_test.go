package clientconfig

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The server stores each client's reported configuration as JSON and reads
// it back at start-up. Compiled command patterns are written as strings
// today. Records written by older servers hold them as empty JSON objects,
// which is how a compiled pattern was encoded before it could be written as
// text. Such a record must still load: the pattern is not recoverable from
// it, and the client reports its configuration again when it connects.
func TestCommandsConfigReadsPatternsWrittenByOlderServers(t *testing.T) {
	stored := `{"enabled":true,"send_back_limit":4194304,"allow":["^/usr/bin/.*"],"deny":["(\\||<|>|;|,|\\n|&)"],"order":["allow","deny"],"allow_regexp":[{}],"deny_regexp":[{},{}]}`

	var cfg CommandsConfig
	require.NoError(t, json.Unmarshal([]byte(stored), &cfg))

	assert.True(t, cfg.Enabled)
	assert.Equal(t, 4194304, cfg.SendBackLimit)
	assert.Equal(t, []string{"^/usr/bin/.*"}, cfg.Allow)
	assert.Equal(t, [2]string{"allow", "deny"}, cfg.Order)
	assert.Empty(t, cfg.AllowRegexp)
	assert.Empty(t, cfg.DenyRegexp)
}

func TestCommandsConfigReadsPatternsWrittenAsText(t *testing.T) {
	stored := `{"enabled":true,"allow":["^/usr/bin/.*"],"allow_regexp":["^/usr/bin/.*"],"deny_regexp":["(\\||;)", "rm -rf"]}`

	var cfg CommandsConfig
	require.NoError(t, json.Unmarshal([]byte(stored), &cfg))

	require.Len(t, cfg.AllowRegexp, 1)
	assert.Equal(t, "^/usr/bin/.*", cfg.AllowRegexp[0].String())
	require.Len(t, cfg.DenyRegexp, 2)
	assert.Equal(t, "rm -rf", cfg.DenyRegexp[1].String())
}

func TestCommandsConfigReadsMissingAndNullPatterns(t *testing.T) {
	for _, stored := range []string{
		`{"enabled":false}`,
		`{"enabled":false,"allow_regexp":null,"deny_regexp":null}`,
		`{"enabled":false,"allow_regexp":[],"deny_regexp":[null]}`,
	} {
		var cfg CommandsConfig
		require.NoError(t, json.Unmarshal([]byte(stored), &cfg), stored)
		assert.Empty(t, cfg.AllowRegexp, stored)
		assert.Empty(t, cfg.DenyRegexp, stored)
	}
}

// A pattern that does not compile is dropped like one that cannot be read.
// The server never matches commands against these copies; the agent applies
// its own.
func TestCommandsConfigSkipsAPatternThatDoesNotCompile(t *testing.T) {
	var cfg CommandsConfig
	require.NoError(t, json.Unmarshal([]byte(`{"allow_regexp":["(", "^ok$", 7]}`), &cfg))
	require.Len(t, cfg.AllowRegexp, 1)
	assert.Equal(t, "^ok$", cfg.AllowRegexp[0].String())
}

// What is written is unchanged: patterns as strings, readable again.
func TestCommandsConfigRoundTrip(t *testing.T) {
	var cfg CommandsConfig
	require.NoError(t, json.Unmarshal([]byte(`{"enabled":true,"allow":["^a$"],"order":["allow","deny"],"allow_regexp":["^a$"],"deny_regexp":["b"]}`), &cfg))

	out, err := json.Marshal(cfg)
	require.NoError(t, err)
	assert.JSONEq(t, `{"enabled":true,"send_back_limit":0,"allow":["^a$"],"deny":null,"order":["allow","deny"],"allow_regexp":["^a$"],"deny_regexp":["b"]}`, string(out))

	var again CommandsConfig
	require.NoError(t, json.Unmarshal(out, &again))
	assert.Equal(t, cfg.AllowRegexp[0].String(), again.AllowRegexp[0].String())
}
