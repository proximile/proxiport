package clienttunnel

import (
	"context"
	"io"
	"net"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/proximile/proxiport/share/logger"
	"github.com/proximile/proxiport/share/models"
)

func lifecycleTestLogger() *logger.Logger {
	return logger.NewLogger("tunnel-lifecycle-test", logger.LogOutput{File: os.Stdout}, logger.LogLevelError)
}

// settledGoroutines waits for the runtime to drop back to a stable count, so a
// leak assertion is not racing goroutines that are on their way out anyway.
func settledGoroutines(t *testing.T, want int) int {
	t.Helper()
	var n int
	for i := 0; i < 100; i++ {
		runtime.Gosched()
		n = runtime.NumGoroutine()
		if n <= want {
			return n
		}
		time.Sleep(10 * time.Millisecond)
	}
	return n
}

type nopConn struct{ closed bool }

func (c *nopConn) Read(p []byte) (int, error)  { return 0, io.EOF }
func (c *nopConn) Write(p []byte) (int, error) { return len(p), nil }
func (c *nopConn) Close() error                { c.closed = true; return nil }

// tunnelTCP.accept starts a watcher goroutine whose only exits are ctx.Done()
// and close(done). Both early returns -- a nil sshConn, and OpenChannel failing
// because the agent rejected the destination or its transport has dropped --
// used to return without closing done, parking one goroutine per inbound
// connection for the tunnel's whole lifetime. A random-port tunnel binds
// 0.0.0.0 and an absent ACL means allow-all, so the connections need not come
// from an authenticated party.
func TestTCPAcceptDoesNotLeakAGoroutinePerFailedConnection(t *testing.T) {
	tunnel := newTunnelTCP(lifecycleTestLogger(), nil, models.Remote{
		LocalHost: "127.0.0.1", LocalPort: "1", RemoteHost: "127.0.0.1", RemotePort: "22",
	}, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	baseline := settledGoroutines(t, 0)

	const connections = 200
	for i := 0; i < connections; i++ {
		tunnel.accept(ctx, &nopConn{})
	}

	// Anything proportional to `connections` is the leak. The small slack
	// covers goroutines the test binary itself is running.
	after := settledGoroutines(t, baseline+10)
	assert.LessOrEqual(t, after, baseline+10,
		"accept leaked roughly one goroutine per connection: %d -> %d over %d connections",
		baseline, after, connections)
}

// tunnelUDP.Terminate canceled the context and waited for runInbound, but
// nothing closed the agent channel runOutbound is blocked reading -- and
// comm.UDPChannel.Decode takes no context. So runOutbound, its gob decoder and
// the live SSH channel on both ends survived every create/delete cycle for as
// long as the agent stayed connected.
func TestTunnelUDPTerminateClosesTheAgentChannel(t *testing.T) {
	udpReadTimeout = time.Millisecond

	serverSide, agentSide := net.Pipe()
	defer func() { _ = agentSide.Close() }()

	tunnel := newTunnelUDP(lifecycleTestLogger(), nil, models.Remote{}, nil)
	// What Start does with the real ssh.Channel.
	tunnel.sshChan = serverSide

	baseline := settledGoroutines(t, 0)
	require.NoError(t, tunnel.start(context.Background(), serverSide))

	// Set before Terminate: once the server end is closed, setting a deadline
	// on the peer is itself an error.
	require.NoError(t, agentSide.SetWriteDeadline(time.Now().Add(2*time.Second)))

	require.NoError(t, tunnel.Terminate(false))

	// The error has to be the RIGHT error. A write to a closed net.Pipe fails
	// immediately with io.ErrClosedPipe; a write to a pipe nobody is reading
	// fails too, but only when the deadline expires. Asserting merely that the
	// write failed passes either way -- it did, on the first version of this
	// test, against the unfixed code.
	_, err := agentSide.Write([]byte{1, 2, 3})
	require.ErrorIs(t, err, io.ErrClosedPipe,
		"expected the agent channel to be closed; a timeout here means it is still open and runOutbound is still parked on it")

	after := settledGoroutines(t, baseline)
	assert.LessOrEqual(t, after, baseline,
		"a goroutine survived Terminate: %d -> %d", baseline, after)
}

// A protocol that refuses and one that always closes, in the order
// newTunnelTCP/newTunnelUDP are appended for a tcp+udp tunnel.
type fakeProtocol struct {
	refuse       error
	terminated   bool
	canTermCalls int
}

func (f *fakeProtocol) Start(ctx context.Context) error { return nil }
func (f *fakeProtocol) CanTerminate(force bool) error {
	f.canTermCalls++
	if force {
		return nil
	}
	return f.refuse
}
func (f *fakeProtocol) Terminate(force bool) error {
	if err := f.CanTerminate(force); err != nil {
		return err
	}
	f.terminated = true
	return nil
}
func (f *fakeProtocol) LastActive() time.Time { return time.Time{} }
func (f *fakeProtocol) SetACL(*TunnelACL)     {}

// MultiProtocolTunnel.Terminate ran straight through the list, appending
// errors. tunnelTCP honors force and refuses while connections are live;
// tunnelUDP ignores force and always closes. So a non-force DELETE of a tcp+udp
// tunnel with a live TCP session answered 409 -- which reads as "nothing
// changed" -- while having already killed the UDP half for good, leaving a
// tunnel record advertising a port the next Refresh would hand to someone else.
func TestMultiProtocolTerminateIsAllOrNothing(t *testing.T) {
	refusing := &fakeProtocol{refuse: assertRefusal}
	always := &fakeProtocol{}
	mt := &MultiProtocolTunnel{Protocols: []TunnelProtocol{refusing, always}}

	err := mt.Terminate(false)

	require.Error(t, err, "a refusing protocol must make the whole tunnel refuse")
	assert.False(t, always.terminated,
		"the protocol that cannot refuse was torn down anyway, while the operator was told the delete failed")
	assert.False(t, refusing.terminated)

	// force must still stop everything.
	require.NoError(t, mt.Terminate(true))
	assert.True(t, refusing.terminated)
	assert.True(t, always.terminated)
}

var assertRefusal = io.ErrUnexpectedEOF

// InternalTunnelProxy.Start handed the listener to a goroutine and returned nil
// unconditionally; a bind failure was only Debug-logged, below the default
// level. The caller's rollback branch -- which exists to terminate the
// underlying tunnel when its proxy cannot start -- was therefore unreachable,
// so the API answered 200 with a tunnel object naming a proxy address that
// refuses every connection, while the server kept the underlying listener up
// and its port marked reserved.
func TestInternalTunnelProxyStartReportsABindFailure(t *testing.T) {
	// Hold the port the proxy will try to bind.
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = occupied.Close() }()

	host, port, err := net.SplitHostPort(occupied.Addr().String())
	require.NoError(t, err)

	scheme := "http"
	tunnel := &Tunnel{Remote: models.Remote{
		LocalHost: "127.0.0.1", LocalPort: "1", RemoteHost: "127.0.0.1", RemotePort: "22",
		Scheme: &scheme,
	}}
	config := &InternalTunnelProxyConfig{
		Host:     host,
		CertFile: "../../../testdata/certs/tunnels.proxiport.test.crt",
		KeyFile:  "../../../testdata/certs/tunnels.proxiport.test.key",
	}

	proxy := NewInternalTunnelProxy(tunnel, lifecycleTestLogger(), config, host, port, nil, nil)

	err = proxy.Start(context.Background())
	require.Error(t, err,
		"Start reported success for a proxy that could not bind, so the caller's rollback never runs")
	assert.Contains(t, err.Error(), "address already in use")
}
