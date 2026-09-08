package chshare

import (
	"net"
	"time"

	"github.com/gorilla/websocket"
)

type wsConn struct {
	*websocket.Conn
	buff []byte
}

// MaxWebSocketMessageBytes bounds a single frame on the agent transport.
//
// gorilla/websocket defaults to no limit, so ReadMessage allocates whatever the
// peer says the frame is -- and the agent listener reads from an unauthenticated
// peer, before the SSH handshake. One frame claiming a large size was enough to
// exhaust the server's memory.
//
// Every message here is one SSH packet, and x/crypto/ssh caps those well below
// this, so the limit is generous for anything legitimate.
const MaxWebSocketMessageBytes = 1 << 20

func NewWebSocketConn(websocketConn *websocket.Conn) net.Conn {
	websocketConn.SetReadLimit(MaxWebSocketMessageBytes)
	c := wsConn{
		Conn: websocketConn,
	}
	return &c
}

// Read is not thread-safe though that's okay since there
// should never be more than one reader
func (c *wsConn) Read(dst []byte) (int, error) {
	ldst := len(dst)
	//use buffer or read new message
	var src []byte
	if len(c.buff) > 0 {
		src = c.buff
		c.buff = nil
	} else if _, msg, err := c.ReadMessage(); err == nil {
		src = msg
	} else {
		return 0, err
	}
	//copy src->dest
	var n int
	if len(src) > ldst {
		//copy as much as possible of src into dst
		n = copy(dst, src[:ldst])
		//copy remainder into buffer
		r := src[ldst:]
		lr := len(r)
		c.buff = make([]byte, lr)
		copy(c.buff, r)
	} else {
		//copy all of src into dst
		n = copy(dst, src)
	}
	//return bytes copied
	return n, nil
}

func (c *wsConn) Write(b []byte) (int, error) {
	if err := c.WriteMessage(websocket.BinaryMessage, b); err != nil {
		return 0, err
	}
	n := len(b)
	return n, nil
}

func (c *wsConn) SetDeadline(t time.Time) error {
	if err := c.SetReadDeadline(t); err != nil {
		return err
	}
	return c.SetWriteDeadline(t)
}
