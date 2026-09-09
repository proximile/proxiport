package chshare

import (
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// wsServer upgrades one connection, hands the wrapped net.Conn to fn, and
// returns the URL to dial and a channel carrying fn's error.
func wsServer(t *testing.T, fn func(net.Conn) error) (string, <-chan error) {
	t.Helper()

	errs := make(chan error, 1)
	upgrader := websocket.Upgrader{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			errs <- err
			return
		}
		errs <- fn(NewWebSocketConn(ws))
	}))
	t.Cleanup(srv.Close)

	return "ws" + strings.TrimPrefix(srv.URL, "http"), errs
}

// A peer that completes the WebSocket upgrade and then sends nothing used to
// hold a slot in the handshake pool forever, because ssh.NewServerConn blocks
// reading the version banner and nothing set a deadline. This is the mechanism
// the fix relies on: the deadline has to reach the underlying socket through
// the wsConn wrapper, and gorilla's Conn has no SetDeadline of its own.
func TestWebSocketConnDeadlineInterruptsASilentPeer(t *testing.T) {
	url, errs := wsServer(t, func(conn net.Conn) error {
		if err := conn.SetDeadline(time.Now().Add(150 * time.Millisecond)); err != nil {
			return err
		}
		_, err := io.ReadFull(conn, make([]byte, 8))
		return err
	})

	client, _, err := websocket.DefaultDialer.Dial(url, nil)
	require.NoError(t, err)
	defer func() { _ = client.Close() }()

	// The client sends nothing at all, the way a stalled peer would not.
	select {
	case err := <-errs:
		require.Error(t, err, "a read from a silent peer must not block past the deadline")
		var netErr net.Error
		require.True(t, errors.As(err, &netErr), "expected a net.Error, got %T: %v", err, err)
		assert.True(t, netErr.Timeout(), "the deadline must surface as a timeout, got %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("the read never returned: the deadline did not reach the socket")
	}
}

// Clearing the deadline is what lets a connection be long-lived once the
// handshake is done. Setting it and never clearing it would disconnect every
// idle agent instead, so the clear is as load-bearing as the set.
func TestWebSocketConnDeadlineCanBeCleared(t *testing.T) {
	url, errs := wsServer(t, func(conn net.Conn) error {
		if err := conn.SetDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
			return err
		}
		if err := conn.SetDeadline(time.Time{}); err != nil {
			return err
		}
		_, err := io.ReadFull(conn, make([]byte, 4))
		return err
	})

	client, _, err := websocket.DefaultDialer.Dial(url, nil)
	require.NoError(t, err)
	defer func() { _ = client.Close() }()

	// Send after the deadline that was set would have fired.
	time.Sleep(300 * time.Millisecond)
	require.NoError(t, client.WriteMessage(websocket.BinaryMessage, []byte("ping")))

	select {
	case err := <-errs:
		assert.NoError(t, err, "a cleared deadline must not expire")
	case <-time.After(5 * time.Second):
		t.Fatal("the read never returned")
	}
}

// gorilla/websocket defaults to no read limit, so ReadMessage allocates
// whatever the peer's frame header claims. The agent listener reads from an
// unauthenticated peer before the SSH handshake, so one frame was enough to
// exhaust the server's memory.
func TestWebSocketConnRefusesAnOversizedFrame(t *testing.T) {
	url, errs := wsServer(t, func(conn net.Conn) error {
		_, err := io.ReadFull(conn, make([]byte, 8))
		return err
	})

	client, _, err := websocket.DefaultDialer.Dial(url, nil)
	require.NoError(t, err)
	defer func() { _ = client.Close() }()

	require.NoError(t, client.WriteMessage(websocket.BinaryMessage, make([]byte, MaxWebSocketMessageBytes+1)))

	select {
	case err := <-errs:
		require.Error(t, err, "a frame over the limit must not be read into memory")
		// gorilla surfaces this on the reading side as ErrReadLimit and sends
		// the peer a CloseMessageTooBig; the server-side error is the one that
		// matters here, because it is what stops the allocation.
		assert.ErrorIs(t, err, websocket.ErrReadLimit)
	case <-time.After(5 * time.Second):
		t.Fatal("the read never returned")
	}
}

// A frame at the limit is legitimate traffic and must still be delivered --
// the limit is there to stop an allocation, not to break the transport.
func TestWebSocketConnAcceptsAFrameAtTheLimit(t *testing.T) {
	url, errs := wsServer(t, func(conn net.Conn) error {
		buf := make([]byte, MaxWebSocketMessageBytes)
		_, err := io.ReadFull(conn, buf)
		return err
	})

	client, _, err := websocket.DefaultDialer.Dial(url, nil)
	require.NoError(t, err)
	defer func() { _ = client.Close() }()

	require.NoError(t, client.WriteMessage(websocket.BinaryMessage, make([]byte, MaxWebSocketMessageBytes)))

	select {
	case err := <-errs:
		assert.NoError(t, err, "a frame at exactly the limit is legitimate traffic")
	case <-time.After(5 * time.Second):
		t.Fatal("the read never returned")
	}
}
