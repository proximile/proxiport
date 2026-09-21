package clienttunnel

import (
	"context"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/proximile/proxiport/share/comm"
	"github.com/proximile/proxiport/share/logger"
	"github.com/proximile/proxiport/share/models"
)

var udpReadTimeout = time.Second

type tunnelUDP struct {
	*logger.Logger
	models.Remote
	sshConn     ssh.Conn
	acl         atomic.Pointer[TunnelACL] // parsed Remote.ACL field
	idleTimeout time.Duration

	conn    *net.UDPConn
	channel *comm.UDPChannel
	done    chan struct{}
	cancel  func()

	// peers are the addresses this tunnel has received datagrams from. The
	// outbound destination comes from the agent, so it is checked against
	// this rather than trusted.
	peers *udpPeerTable

	// sshChan is the agent channel runOutbound reads. Kept so it can be
	// closed: comm.UDPChannel.Decode takes no context, so canceling alone
	// leaves runOutbound parked on it -- along with the agent's own
	// per-channel goroutine -- for the rest of the connection. nil when
	// start() is driven directly with a pipe, as the tests do.
	sshChan io.Closer

	mtx        sync.Mutex
	lastActive time.Time
}

func newTunnelUDP(logger *logger.Logger, ssh ssh.Conn, remote models.Remote, acl *TunnelACL) *tunnelUDP {
	t := &tunnelUDP{
		Logger:      logger,
		Remote:      remote,
		sshConn:     ssh,
		done:        make(chan struct{}),
		peers:       newUDPPeerTable(),
		lastActive:  time.Now(),
		idleTimeout: time.Duration(remote.IdleTimeoutMinutes) * time.Minute,
	}
	t.SetACL(acl)
	return t
}

func (t *tunnelUDP) Start(ctx context.Context) error {
	t.Debugf("Starting udp tunnel...")
	remoteAddr := t.Remote.Remote() + "/udp"
	sshChan, reqs, err := t.sshConn.OpenChannel("rport", []byte(remoteAddr))
	if err != nil {
		return err
	}
	go ssh.DiscardRequests(reqs)
	t.sshChan = sshChan

	return t.start(ctx, sshChan)
}

func (t *tunnelUDP) start(ctx context.Context, sshChan io.ReadWriter) error {
	a, err := net.ResolveUDPAddr("udp", t.Local())
	if err != nil {
		return err
	}
	conn, err := net.ListenUDP("udp", a)
	if err != nil {
		return err
	}
	t.conn = conn

	ctx, t.cancel = context.WithCancel(ctx)

	t.channel = comm.NewUDPChannel(sshChan)

	go func() {
		err := t.runInbound(ctx)
		if err != nil {
			t.Errorf("Error receiving UDP: %v", err)
		}
	}()
	go func() {
		err := t.runOutbound(ctx)
		if err != nil {
			t.Errorf("Error sending UDP: %v", err)
		}
	}()

	// Terminate cancels ctx, and so does the agent's connection going away.
	// Either way the channel has to be closed or runOutbound never returns:
	// it is blocked in a gob decode that no context can interrupt.
	go func() {
		<-ctx.Done()
		if t.sshChan != nil {
			_ = t.sshChan.Close()
		}
	}()

	return nil
}

func (t *tunnelUDP) runInbound(ctx context.Context) error {
	defer func() { _ = t.conn.Close() }()
	defer close(t.done)

	const maxMTU = 9012
	buff := make([]byte, maxMTU)
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		err := t.conn.SetReadDeadline(time.Now().Add(udpReadTimeout))
		if err != nil {
			return err
		}

		n, sourceAddr, err := t.conn.ReadFromUDP(buff)
		if e, ok := err.(net.Error); ok && (e.Timeout() || e.Temporary()) {
			continue
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}

		t.setLastActive()

		acl := t.acl.Load()
		if acl != nil {
			if !acl.CheckAccess(sourceAddr.IP) {
				t.Debugf("Access rejected. Remote addr: %s", sourceAddr)
				continue
			}
		}

		// Only a peer the ACL admits becomes repliable.
		t.peers.remember(sourceAddr)

		err = t.channel.Encode(sourceAddr, buff[:n])
		if err != nil {
			return err
		}
	}
}

func (t *tunnelUDP) runOutbound(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		addr, data, err := t.channel.Decode()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}

		// The destination is whatever the agent encoded, and the socket is
		// unconnected and wildcard-bound, so it would otherwise accept any
		// destination: the server's own loopback services, or a network the
		// agent cannot route to but the server can, with the datagram
		// carrying the server's source address. An honest agent only ever
		// echoes back an address the server gave it.
		if !t.peers.known(addr) {
			t.Debugf("Dropping outbound datagram to %s: no peer at that address has used this tunnel", addr)
			continue
		}

		// Re-checked here, not just on the way in: the ACL can be narrowed
		// while a peer is mid-exchange, and revoking access has to stop the
		// replies too.
		if acl := t.acl.Load(); acl != nil && !acl.CheckAccess(addr.IP) {
			t.Debugf("Dropping outbound datagram to %s: not permitted by the tunnel ACL", addr)
			continue
		}

		// After the checks, so a stream of rejected datagrams cannot hold the
		// tunnel open past its idle timeout.
		t.setLastActive()

		_, err = t.conn.WriteToUDP(data, addr)
		if err != nil {
			return err
		}
	}
}

// CanTerminate reports whether Terminate would proceed. A UDP tunnel has no
// connection state to refuse on, so it always can -- which is precisely why
// MultiProtocolTunnel must ask the TCP half before stopping this one.
func (t *tunnelUDP) CanTerminate(force bool) error {
	return nil
}

func (t *tunnelUDP) Terminate(force bool) error {
	t.cancel()
	<-t.done

	return nil
}

func (t *tunnelUDP) LastActive() time.Time {
	t.mtx.Lock()
	defer t.mtx.Unlock()

	return t.lastActive
}

func (t *tunnelUDP) setLastActive() {
	t.mtx.Lock()
	defer t.mtx.Unlock()

	t.lastActive = time.Now()
}

func (t *tunnelUDP) SetACL(acl *TunnelACL) {
	t.acl.Store(acl)
}
