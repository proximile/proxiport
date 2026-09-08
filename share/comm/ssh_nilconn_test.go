package comm

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/proximile/proxiport/share/logger"
)

// Callers reach this from detached goroutines -- the file-push fan-out spawns
// one per target client -- so a nil connection here is a crash of the whole
// process, not one failed request. And a nil connection is routine: a Client
// restored from storage has none, the file-push target list is built with
// GetByID which does not filter disconnected clients, and every client is
// disconnected immediately after a server restart.
func TestSendRequestAndGetResponseRefusesNilConn(t *testing.T) {
	log := logger.NewLogger("comm-test", logger.LogOutput{File: os.Stdout}, logger.LogLevelError)

	assert.NotPanics(t, func() {
		err := SendRequestAndGetResponse(nil, "upload", map[string]string{"a": "b"}, nil, log)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not connected")
	})
}
