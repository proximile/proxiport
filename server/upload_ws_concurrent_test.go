package chserver

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	"github.com/proximile/proxiport/share/logger"
	"github.com/proximile/proxiport/share/ws"
)

// A file push runs one goroutine per target client (sendFileToClient), and every
// result is published to every listening /ws/uploads socket. Those publishes are
// concurrent, so two overlapping pushes -- or one push to two clients -- write to
// the same connection at the same time. gorilla panics on that, from a goroutine
// with no recover above it, which takes the daemon down rather than failing the
// upload.
//
// The registration path under test is the real handler, so this also covers what
// is stored: Server.Close sweeps upload sockets by asserting
// *ws.ConcurrentWebSocket, and a raw *websocket.Conn matched nothing there.
func TestUploadNotificationsAreConcurrencySafe(t *testing.T) {
	al := &APIListener{
		Logger: logger.NewLogger("upload-ws-test", logger.LogOutput{File: os.Stdout}, logger.LogLevelError),
		Server: &Server{},
	}

	srv := httptest.NewServer(http.HandlerFunc(al.handleUploadsWS))
	defer srv.Close()

	client, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	require.NoError(t, err)
	defer func() { _ = client.Close() }()

	// Drain, or the writers block on the socket buffer and never overlap.
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		for {
			if _, _, rerr := client.ReadMessage(); rerr != nil {
				return
			}
		}
	}()

	var stored interface{}
	require.Eventually(t, func() bool {
		al.uploadWebSockets.Range(func(_, value interface{}) bool {
			stored = value
			return false
		})
		return stored != nil
	}, 5*time.Second, 10*time.Millisecond, "the handler never registered the socket")

	// Guards the shutdown sweep in Server.Close, which asserts this exact type.
	_, isConcurrent := stored.(*ws.ConcurrentWebSocket)
	require.True(t, isConcurrent,
		"upload sockets must be stored as *ws.ConcurrentWebSocket: Server.Close sweeps them by that type, "+
			"and a raw *websocket.Conn is both unserialised for writes and never closed on shutdown")

	const writers = 8
	const perWriter = 150

	panics := make(chan interface{}, writers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(writers)
	for i := 0; i < writers; i++ {
		go func() {
			defer wg.Done()
			// gorilla panics in the writer's own goroutine; in production that
			// goroutine is consumeUploadResults', with nothing to recover it.
			defer func() {
				if r := recover(); r != nil {
					panics <- r
				}
			}()
			<-start
			for j := 0; j < perWriter; j++ {
				al.notifyUploadEventListeners(&UploadOutput{ClientID: "client"})
			}
		}()
	}
	close(start)
	wg.Wait()
	close(panics)

	for p := range panics {
		t.Fatalf("concurrent upload notifications panicked: %v", p)
	}

	require.NoError(t, client.Close())
	<-drained
}
