package ws

import (
	"encoding/json"
	"io"
	"sync"

	"github.com/proximile/proxiport/server/api"
	"github.com/proximile/proxiport/share/logger"
)

type Conn interface {
	NextReader() (messageType int, r io.Reader, err error)
	ReadMessage() (messageType int, p []byte, err error)
	WriteMessage(messageType int, data []byte) error
	WriteJSON(v interface{}) error
	Close() error
}

type ConcurrentWebSocket struct {
	conn              Conn
	mu                sync.Mutex
	log               *logger.Logger
	writesBeforeClose int
}

func NewConcurrentWebSocket(conn Conn, log *logger.Logger) *ConcurrentWebSocket {
	return &ConcurrentWebSocket{
		conn:              conn,
		log:               log,
		writesBeforeClose: 1,
	}
}

func (ws *ConcurrentWebSocket) ReadJSON(inboundMsg interface{}) error {
	_, r, err := ws.conn.NextReader()
	if err != nil {
		return err
	}
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	return dec.Decode(inboundMsg)
}

func (ws *ConcurrentWebSocket) ReadMessage() (messageType int, p []byte, err error) {
	return ws.conn.ReadMessage()
}

func (ws *ConcurrentWebSocket) WriteError(title string, err error) {
	var errMsg string
	if err != nil {
		errMsg = err.Error()
	}
	_ = ws.WriteJSON(api.NewErrAPIPayloadFromMessage("", title, errMsg))
}

// WriteJSON write json message to websocket, counting towards writes before close
func (ws *ConcurrentWebSocket) WriteJSON(jsonOutboundMsg interface{}) error {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	// LIFO: dec() runs before the unlock, so the countdown is mutated under
	// the same lock that guards the write. It used to run after
	// WriteNonFinalJSON had already released the lock, which made
	// writesBeforeClose an unsynchronised read-modify-write whenever two
	// goroutines wrote to one socket -- the exact situation the type exists
	// to make safe. WriteMessage already had it this way round.
	defer ws.dec()
	return ws.writeJSONLocked(jsonOutboundMsg)
}

// WriteNonFinalJSON write json message to websocket, not counting towards writes before close
func (ws *ConcurrentWebSocket) WriteNonFinalJSON(jsonOutboundMsg interface{}) error {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	return ws.writeJSONLocked(jsonOutboundMsg)
}

// writeJSONLocked writes with ws.mu already held.
func (ws *ConcurrentWebSocket) writeJSONLocked(jsonOutboundMsg interface{}) error {
	err := ws.conn.WriteJSON(jsonOutboundMsg)
	if err != nil {
		ws.log.Errorf("Error WS json write: %v", err)
	}
	return err
}

func (ws *ConcurrentWebSocket) WriteMessage(messageType int, data []byte) error {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	defer ws.dec()
	return ws.conn.WriteMessage(messageType, data)
}

func (ws *ConcurrentWebSocket) SetWritesBeforeClose(n int) {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	ws.writesBeforeClose = n
}

// dec counts one write towards the close threshold and closes the socket when
// it is reached. Callers must hold ws.mu.
func (ws *ConcurrentWebSocket) dec() {
	ws.writesBeforeClose--
	if ws.writesBeforeClose == 0 {
		err := ws.conn.Close()
		if err != nil {
			ws.log.Errorf("Close ws on dec(): %v", err)
		} else {
			ws.log.Debugf("Close ws on dec()")
		}
	}
}

func (ws *ConcurrentWebSocket) Close() error {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	err := ws.conn.Close()
	if err != nil {
		ws.log.Errorf("Error on Close ws: %v", err)
	} else {
		ws.log.Debugf("Close ws")
	}
	return err
}

func NewWebSocketCache() WebSocketCache {
	return WebSocketCache{
		m: map[string]*ConcurrentWebSocket{},
	}
}

type WebSocketCache struct {
	m  map[string]*ConcurrentWebSocket
	mu sync.RWMutex
}

func (c *WebSocketCache) Get(key string) *ConcurrentWebSocket {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.m[key]
}

func (c *WebSocketCache) Set(key string, ws *ConcurrentWebSocket) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[key] = ws
}

func (c *WebSocketCache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.m, key)
}

// CloseConnections closes every cached socket. It reads the map under the lock
// -- every other method on this type does, and shutdown runs concurrently with
// live handlers still calling Set and Delete -- and returns the first close
// error rather than always reporting success to the shutdown waitgroup.
func (c *WebSocketCache) CloseConnections() error {
	c.mu.RLock()
	conns := make([]*ConcurrentWebSocket, 0, len(c.m))
	for _, conn := range c.m {
		conns = append(conns, conn)
	}
	c.mu.RUnlock()

	var firstErr error
	for _, conn := range conns {
		if err := conn.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
