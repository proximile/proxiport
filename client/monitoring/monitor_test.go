package monitoring

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"

	"github.com/proximile/proxiport/share/logger"
	"github.com/proximile/proxiport/share/models"
)

type mockSSHConn struct {
	ssh.Conn

	mtx  sync.Mutex
	sent int
}

func (c *mockSSHConn) SendRequest(string, bool, []byte) (bool, []byte, error) {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	c.sent++
	return false, nil, nil
}

func (c *mockSSHConn) sends() int {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	return c.sent
}

func newTestMonitor() *Monitor {
	return &Monitor{
		logger:      logger.NewLogger("test", logger.NewLogOutput(""), logger.LogLevelDebug),
		measurement: &models.Measurement{},
	}
}

// TestSendMeasurementUsesTheConnectionUntilStopClearsIt is the deterministic
// half: it proves the mock is wired to the code under test, so the concurrent
// test below is not passing because both goroutines are no-ops.
func TestSendMeasurementUsesTheConnectionUntilStopClearsIt(t *testing.T) {
	monitor := newTestMonitor()
	conn := &mockSSHConn{}
	monitor.SetConn(conn)

	monitor.sendMeasurement()
	require.Equal(t, 1, conn.sends())

	monitor.Stop()

	monitor.sendMeasurement()
	assert.Equal(t, 1, conn.sends(), "Stop must have dropped the connection")
}

// TestStopIsSynchronizedWithSendMeasurement covers the same unlocked-write
// shape as the ip_addresses fetcher: sendMeasurement reads conn under the read
// half of the monitor's mutex and SetConn writes it under the write half, but
// Stop used to clear it with no lock at all. Which goroutine wins is not the
// point and is deliberately not asserted -- the race detector is the gate, and
// it reports the unsynchronized write whether or not the nil ever lands.
func TestStopIsSynchronizedWithSendMeasurement(t *testing.T) {
	const rounds = 200

	for i := 0; i < rounds; i++ {
		monitor := newTestMonitor()
		monitor.SetConn(&mockSSHConn{})

		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); monitor.sendMeasurement() }()
		go func() { defer wg.Done(); monitor.Stop() }()
		wg.Wait()
	}
}
