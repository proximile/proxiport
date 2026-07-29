package logger

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stretchr/testify/assert"
)

// TestLogOutputJSONRoundTrip guards a startup regression: the agent config
// (which embeds LogOutput) is JSON-serialized into the server's clients DB and
// read back on restart. File is a non-serializable io.Writer, so it must stay
// json:"-"; otherwise a stored config with a File value fails to unmarshal and
// the server refuses to start.
func TestLogOutputJSONRoundTrip(t *testing.T) {
	out := LogOutput{File: os.Stdout, Rotation: RotationConfig{MaxSizeMB: 100, Compress: true}}

	b, err := json.Marshal(out)
	require.NoError(t, err)
	assert.NotContains(t, string(b), "File", "live File writer must not be serialized")

	var got LogOutput
	require.NoError(t, json.Unmarshal(b, &got))

	// Older rows serialized File as an object ({}) — reading them back must not error.
	require.NoError(t, json.Unmarshal([]byte(`{"File":{},"Rotation":{}}`), &got),
		"a stored config with a legacy File object must still deserialize")
}

func TestLogger(t *testing.T) {
	logfile := t.TempDir() + "/test.log"
	l, err := os.OpenFile(logfile, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0444)
	require.NoError(t, err, "error creating log file")
	defer l.Close()

	logger := NewLogger("test", LogOutput{File: l}, LogLevelDebug)
	logger.Debugf("Debug %s", "Debug")
	logger.Infof("Info %s", "Info")
	logger.Errorf("Error %s", "Error")

	log, err := os.ReadFile(logfile)

	require.NoError(t, err, "error reading log file")
	assert.Contains(t, string(log), "debug: test: Debug Debug")
	assert.Contains(t, string(log), "info: test: Info Info")
	assert.Contains(t, string(log), "error: test: Error Error")
}
