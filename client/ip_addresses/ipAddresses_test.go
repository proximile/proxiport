package ipAddresses

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"

	chshare "github.com/proximile/proxiport/share/logger"
	"github.com/proximile/proxiport/share/models"
)

type mockSSHConn struct {
	ssh.Conn

	mtx      sync.Mutex
	requests [][]byte
}

func (c *mockSSHConn) SendRequest(_ string, _ bool, data []byte) (bool, []byte, error) {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	c.requests = append(c.requests, data)
	return false, nil, nil
}

func (c *mockSSHConn) sent() [][]byte {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	out := make([][]byte, len(c.requests))
	copy(out, c.requests)
	return out
}

func newTestFetcher(t *testing.T) *Fetcher {
	t.Helper()

	return NewFetcher(
		chshare.NewLogger("test", chshare.NewLogOutput(""), chshare.LogLevelDebug),
		"http://ip.example.invalid/",
		1,
	)
}

// TestStopDuringTheLookupDoesNotReachTheOldConnection is M14. Against the
// unfixed tree this panics: sendIPAddresses read conn before the lookup, Stop
// nils it while the lookup is in flight, and SendRequest is then called on a
// nil ssh.Conn from a bare goroutine -- which kills the whole agent, at a
// moment the server chooses, on every reconnect it likes.
func TestStopDuringTheLookupDoesNotReachTheOldConnection(t *testing.T) {
	fetcher := newTestFetcher(t)
	conn := &mockSSHConn{}
	fetcher.SetConn(conn)

	inLookup := make(chan struct{})
	release := make(chan struct{})
	fetcher.fetchIPs = func(context.Context, string) (*models.IPAddresses, error) {
		close(inLookup)
		<-release
		return &models.IPAddresses{IPv4: "198.51.100.7", IPv6: "2001:db8::7"}, nil
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		fetcher.sendIPAddresses(context.Background())
	}()

	<-inLookup
	fetcher.Stop()
	close(release)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("sendIPAddresses did not return")
	}

	assert.Empty(t, conn.sent(), "nothing may be sent over a connection Stop already dropped")
}

func TestSendIPAddressesSendsOverALiveConnection(t *testing.T) {
	fetcher := newTestFetcher(t)
	conn := &mockSSHConn{}
	fetcher.SetConn(conn)

	ips := &models.IPAddresses{IPv4: "198.51.100.7", IPv6: "2001:db8::7"}
	fetcher.fetchIPs = func(context.Context, string) (*models.IPAddresses, error) {
		return ips, nil
	}

	fetcher.sendIPAddresses(context.Background())

	sent := conn.sent()
	require.Len(t, sent, 1)
	var got models.IPAddresses
	require.NoError(t, json.Unmarshal(sent[0], &got))
	assert.Equal(t, "198.51.100.7", got.IPv4)
	assert.Equal(t, "2001:db8::7", got.IPv6)

	// Unchanged addresses are not resent -- and because that state now lives on
	// the fetcher rather than in a package var, a second fetcher in the same
	// process is unaffected by the first one's history.
	fetcher.sendIPAddresses(context.Background())
	assert.Len(t, conn.sent(), 1)

	other := newTestFetcher(t)
	otherConn := &mockSSHConn{}
	other.SetConn(otherConn)
	other.fetchIPs = fetcher.fetchIPs
	other.sendIPAddresses(context.Background())
	assert.Len(t, otherConn.sent(), 1, "a second fetcher starts with no history of its own")
}

// TestConnAccessIsSynchronized is the general form: whatever order the
// connection lifecycle runs in, no goroutine may touch conn without the mutex.
// It is the race detector, not the assertions, that does the work here -- the
// unfixed Stop() is an unsynchronized write to a field two other methods read
// under the lock, and -race reports it whether or not the nil ever lands.
func TestConnAccessIsSynchronized(t *testing.T) {
	fetcher := newTestFetcher(t)
	fetcher.fetchIPs = func(context.Context, string) (*models.IPAddresses, error) {
		return &models.IPAddresses{IPv4: "198.51.100.7"}, nil
	}

	const rounds = 200
	var wg sync.WaitGroup
	for i := 0; i < rounds; i++ {
		conn := &mockSSHConn{}
		wg.Add(3)
		go func() { defer wg.Done(); fetcher.SetConn(conn) }()
		go func() { defer wg.Done(); fetcher.sendIPAddresses(context.Background()) }()
		go func() { defer wg.Done(); fetcher.Stop() }()
	}
	wg.Wait()
}
