package chserver

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// The handshake pool is held for the whole of ssh.NewServerConn, which blocks
// reading the peer's version banner. Nothing set a deadline, so a peer that
// completed the WebSocket upgrade and then sent nothing held its slot forever
// -- for the cost of one idle socket, and invisibly, since the handler never
// logs past "Handling inbound web socket connection".
//
// This is an invariant check on the two constants, not a behaviour test: it
// cannot see whether the deadline reaches the socket. That is covered in
// share, by TestWebSocketConnDeadlineInterruptsASilentPeer and
// TestWebSocketConnDeadlineCanBeCleared, which drive a real WebSocket peer
// through the same wsConn wrapper the listener wraps its connection in.
//
// What is worth pinning here is the relationship: an unbounded handshake is
// the bug, and an unbounded queue of goroutines waiting for a slot is the bug
// the pool exists to prevent, so the wait must not outlive the handshake.
func TestHandshakeBoundsAreSet(t *testing.T) {
	assert.NotZero(t, sshHandshakeTimeout, "an unbounded handshake holds a pool slot forever")
	assert.LessOrEqual(t, sshHandshakeTimeout, 30*time.Second,
		"a slot held this long is a slot an attacker can hold")

	assert.NotZero(t, sshHandshakeQueueWait, "waiting forever for a slot is an unbounded goroutine queue")
	assert.LessOrEqual(t, sshHandshakeQueueWait, sshHandshakeTimeout,
		"waiting longer than a handshake takes means the queue outlives the thing it waits for")
}
