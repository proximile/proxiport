package chclient

import (
	"errors"
	"testing"
	"time"

	"github.com/jpillora/backoff"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/proximile/proxiport/share/comm"
)

// A server that accepts the TCP/WS connection but does not answer the
// new_connection request in time yields a comm.TimeoutError. The timeout branch
// used to read backoff.Attempt() *after* Reset() had zeroed it, so every single
// server timeout hit rand.Intn(0) and panicked — killing the agent at exactly
// the moment it was trying to reconnect.
func TestHandleConnectionErrorOnServerTimeoutDoesNotPanic(t *testing.T) {
	configCopy := getDefaultValidMinConfig()
	configCopy.Connection.MaxRetryCount = -1
	c := Client{
		Logger:       testLog,
		configHolder: &configCopy,
	}

	b := &backoff.Backoff{
		Min:    time.Millisecond,
		Max:    time.Millisecond,
		Factor: 2,
	}

	for i := 0; i < 3; i++ {
		require.NotPanics(t, func() {
			stop := c.handleConnectionError(b, comm.TimeoutError{})
			assert.False(t, stop)
		}, "attempt %d", i)
	}
}

// A non-timeout error must still consume the backoff normally.
func TestHandleConnectionErrorStopsAtMaxRetryCount(t *testing.T) {
	configCopy := getDefaultValidMinConfig()
	configCopy.Connection.MaxRetryCount = 0
	c := Client{
		Logger:       testLog,
		configHolder: &configCopy,
	}

	b := &backoff.Backoff{Min: time.Millisecond, Max: time.Millisecond}
	assert.True(t, c.handleConnectionError(b, errors.New("dial tcp: connection refused")))
}
