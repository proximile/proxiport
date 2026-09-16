package clienttunnel

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jpillora/sizestr"
	"golang.org/x/crypto/ssh"

	chshare "github.com/proximile/proxiport/share"
	"github.com/proximile/proxiport/share/logger"
	"github.com/proximile/proxiport/share/models"
)

type tunnelTCP struct {
	// Declare 64-bit integer before 32-bit for alignment when compiling Go on 32-bit ARM platforms
	lastConnClose int64 // time stored as int64 so it can be used with atomic
	*logger.Logger
	models.Remote
	sshConn ssh.Conn
	acl     atomic.Pointer[TunnelACL] // parsed Remote.ACL field

	stopFn                    func()
	connectionIDAutoIncrement int32
	connCount                 int32
	wg                        sync.WaitGroup // TODO: verify whether wait group is needed here
}

func newTunnelTCP(logger *logger.Logger, ssh ssh.Conn, remote models.Remote, acl *TunnelACL) *tunnelTCP {
	t := &tunnelTCP{
		Logger:  logger,
		Remote:  remote,
		sshConn: ssh,
	}
	t.SetACL(acl)
	return t
}

func (t *tunnelTCP) Start(ctx context.Context) error {
	t.Debugf("starting tcp tunnel...")
	t.Debugf("listening on %+v", t.Local())

	// TODO(m-terel): consider to use ListenTCP
	l, err := net.Listen("tcp", t.Local())
	if err != nil {
		return fmt.Errorf("%s: %s", t.Prefix(), err)
	}

	ctx, t.stopFn = context.WithCancel(ctx)
	t.wg.Add(1)
	go t.listen(ctx, l)
	return nil
}

func (t *tunnelTCP) CanTerminate(force bool) error {
	if n := atomic.LoadInt32(&t.connCount); !force && n > 0 {
		return fmt.Errorf("tunnel has %d active connection(s)", n)
	}
	return nil
}

func (t *tunnelTCP) Terminate(force bool) error {
	if err := t.CanTerminate(force); err != nil {
		return err
	}
	if t.stopFn == nil {
		return nil
	}

	t.stopFn()
	t.wg.Wait()
	t.Infof("stopped")
	t.stopFn = nil
	return nil
}

func (t *tunnelTCP) listen(ctx context.Context, l net.Listener) {
	defer func() {
		t.wg.Done()
	}()

	t.Infof("tunnel listening")

	// background goroutine to close the listener when context is canceled
	go func() {
		<-ctx.Done()
		if err := l.Close(); err != nil {
			t.Errorf("Failed to close tunnel listener: %v", err)
			return
		}
		t.Debugf("tunnel listener closed")
	}()

	for {
		conn, err := l.Accept()
		if err != nil {
			// If Done channel was closed then listener was closed by the background goroutine. It causes
			// Accept to return an err. Check the Done channel to see whether it should continue or quit.
			select {
			case <-ctx.Done():
				//listener closed
			default:
				t.Errorf("Failed to accept connection: %v", err)
			}
			return
		}

		acl := t.acl.Load()
		if acl != nil {
			tcpAddr, ok := conn.RemoteAddr().(*net.TCPAddr)
			if !ok {
				t.Errorf("Unsupported remote address type. Expected net.TCPAddr. %v", conn.RemoteAddr())
				_ = conn.Close()
				continue
			}

			if !acl.CheckAccess(tcpAddr.IP) {
				t.Debugf("Access rejected. Remote addr: %s", tcpAddr)
				_ = conn.Close()
				continue
			}
		}

		t.wg.Add(1)
		go func() {
			t.accept(ctx, conn)
			t.wg.Done()
			atomic.StoreInt64(&t.lastConnClose, time.Now().Unix())
		}()
	}
}

func (t *tunnelTCP) LastActive() time.Time {
	if atomic.LoadInt32(&t.connCount) > 0 {
		return time.Now()
	}
	return time.Unix(atomic.LoadInt64(&t.lastConnClose), 0)
}

func (t *tunnelTCP) accept(ctx context.Context, src io.ReadWriteCloser) {
	defer func() { _ = src.Close() }()
	cid := atomic.AddInt32(&t.connectionIDAutoIncrement, 1)
	atomic.AddInt32(&t.connCount, 1)
	defer atomic.AddInt32(&t.connCount, -1)

	l := t.Fork("conn#%d", cid)

	l.Debugf("Accept")

	done := make(chan bool)
	// Release the watcher below on EVERY path, not just the happy one. Its
	// only exits are ctx.Done() and done, and both early returns here -- a nil
	// sshConn, and OpenChannel failing because the agent rejected the
	// destination under its own tunnel_allowed policy or its transport has
	// dropped -- used to return without closing done. That parked one
	// goroutine, plus its closure and forked logger, for the tunnel's whole
	// lifetime, at whatever rate anyone able to reach the listening socket
	// cared to open connections. A random-port tunnel binds 0.0.0.0 and an
	// absent ACL means allow-all, so that need not be an authenticated party.
	defer close(done)
	// link ctx to conn
	go func() {
		select {
		case <-ctx.Done():
			if src.Close() == nil {
				l.Debugf("closed")
			}
		case <-done:
			// do nothing
		}
	}()

	if t.sshConn == nil {
		l.Debugf("No remote connection")
		return
	}
	// ssh request to open connection to this tunnel's remote
	dst, reqs, err := t.sshConn.OpenChannel("rport", []byte(t.Remote.Remote()))
	if err != nil {
		l.Errorf("Could not establish TCP tunnel: %v", err)
		return
	}

	l.Debugf("SSH channel open")
	l.Debugf("from %+v", t.sshConn.RemoteAddr())

	go ssh.DiscardRequests(reqs)
	//then pipe
	s, r := chshare.Pipe(src, dst)
	l.Debugf("Close (sent %s received %s)", sizestr.ToString(s), sizestr.ToString(r))
}

func (t *tunnelTCP) SetACL(acl *TunnelACL) {
	t.acl.Store(acl)
}
