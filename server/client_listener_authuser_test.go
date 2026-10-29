package chserver

import (
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/proximile/proxiport/server/chconfig"
	"github.com/proximile/proxiport/server/clientsauth"
	"github.com/proximile/proxiport/share/logger"
	"github.com/proximile/proxiport/share/security"
)

// authConnMock is a minimal ssh.ConnMetadata carrying just the user (the client
// auth id) and a remote address, which is all authUser reads. The remote
// address defaults to 127.0.0.1.
type authConnMock struct {
	user   string
	remote net.IP
}

func (m *authConnMock) User() string          { return m.user }
func (m *authConnMock) SessionID() []byte     { return []byte("session") }
func (m *authConnMock) ClientVersion() []byte { return []byte("SSH-2.0-test") }
func (m *authConnMock) ServerVersion() []byte { return []byte("SSH-2.0-test") }
func (m *authConnMock) LocalAddr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 8080}
}
func (m *authConnMock) RemoteAddr() net.Addr {
	ip := m.remote
	if ip == nil {
		ip = net.IPv4(127, 0, 0, 1)
	}
	return &net.TCPAddr{IP: ip, Port: 54321}
}

// remoteAgent is the address the IP-ban tests log in from: an ordinary,
// non-loopback peer.
var remoteAgent = net.ParseIP("203.0.113.7")

// newAuthTestListener builds a ClientListener wired with just enough to exercise
// authUser: a logger, an empty ban list, and a server holding the given
// credential provider.
func newAuthTestListener(t *testing.T, provider clientsauth.Provider, log *logger.Logger) *ClientListener {
	t.Helper()
	return &ClientListener{
		logger:            log,
		bannedClientAuths: security.NewBanList(time.Minute),
		// bannedIPs left nil — authUser guards it.
		server: &Server{
			config:             &chconfig.Config{},
			clientAuthProvider: provider,
		},
	}
}

// TestAuthUserBcryptCredentialAuthenticates is the direct end-to-end guard: a
// credential stored as a bcrypt hash must authenticate an agent that presents
// the matching plaintext. This is the scenario an API-created credential
// produces.
func TestAuthUserBcryptCredentialAuthenticates(t *testing.T) {
	const id, pw = "agent-api", "s3cret-from-api" //nolint:gosec // test fixture, not a real credential
	hashed, err := clientsauth.HashPassword(pw)
	require.NoError(t, err)
	require.True(t, clientsauth.IsHashed(hashed), "HashPassword must produce a hashed credential")

	log := logger.NewLogger("authuser-test", logger.LogOutput{File: os.Stdout}, logger.LogLevelInfo)
	cl := newAuthTestListener(t, clientsauth.NewSingleProvider(id, hashed), log)

	_, err = cl.authUser(&authConnMock{user: id}, []byte(pw))
	assert.NoError(t, err, "a bcrypt-hashed credential must authenticate the matching plaintext")
}

// TestAuthUserBcryptWrongPasswordRejected: the same hashed credential must
// reject a wrong password.
func TestAuthUserBcryptWrongPasswordRejected(t *testing.T) {
	const id, pw = "agent-api", "s3cret-from-api" //nolint:gosec // test fixture, not a real credential
	hashed, err := clientsauth.HashPassword(pw)
	require.NoError(t, err)

	log := logger.NewLogger("authuser-test", logger.LogOutput{File: os.Stdout}, logger.LogLevelInfo)
	cl := newAuthTestListener(t, clientsauth.NewSingleProvider(id, hashed), log)

	_, err = cl.authUser(&authConnMock{user: id}, []byte("wrong-password"))
	assert.Error(t, err, "a wrong password must be rejected")
}

// TestAuthUserLegacyPlaintextCredentialAuthenticates: a legacy plaintext
// credential at rest must still authenticate via the constant-time path.
func TestAuthUserLegacyPlaintextCredentialAuthenticates(t *testing.T) {
	const id, pw = "agent-legacy", "plain-text-pw" //nolint:gosec // test fixture, not a real credential
	require.False(t, clientsauth.IsHashed(pw), "test fixture must be plaintext")

	log := logger.NewLogger("authuser-test", logger.LogOutput{File: os.Stdout}, logger.LogLevelInfo)
	cl := newAuthTestListener(t, clientsauth.NewSingleProvider(id, pw), log)

	_, err := cl.authUser(&authConnMock{user: id}, []byte(pw))
	assert.NoError(t, err, "a legacy plaintext credential must authenticate")
}

// TestAuthUserUnknownIDRejected: an unknown client auth id must be rejected.
func TestAuthUserUnknownIDRejected(t *testing.T) {
	log := logger.NewLogger("authuser-test", logger.LogOutput{File: os.Stdout}, logger.LogLevelInfo)
	cl := newAuthTestListener(t, clientsauth.NewSingleProvider("known-agent", "pw"), log)

	_, err := cl.authUser(&authConnMock{user: "nobody"}, []byte("pw"))
	assert.Error(t, err, "an unknown client auth id must be rejected")
}

// newAuthTestListenerWithIPBan builds a listener whose per-auth-id wait is
// disabled (NewBanList(0) never bans) so every authUser call reaches the
// IP-level ban list, letting these tests exercise the distinct-credential
// counting directly rather than being short-circuited by the per-id wait.
func newAuthTestListenerWithIPBan(t *testing.T, provider clientsauth.Provider, ipBan *security.MaxBadAttemptsBanList) *ClientListener {
	t.Helper()
	log := logger.NewLogger("authuser-test", logger.LogOutput{File: os.Stdout}, logger.LogLevelInfo)
	return &ClientListener{
		logger:            log,
		bannedClientAuths: security.NewBanList(0),
		bannedIPs:         ipBan,
		server: &Server{
			config:             &chconfig.Config{},
			clientAuthProvider: provider,
		},
	}
}

// TestAuthUserSameWrongCredentialDoesNotSelfBanIP guards the reconnect loop:
// an agent retrying the SAME wrong credential (its own reconnect loop) must not
// ban its own IP, no matter how many times it retries.
func TestAuthUserSameWrongCredentialDoesNotSelfBanIP(t *testing.T) {
	hashed, err := clientsauth.HashPassword("correct-pw")
	require.NoError(t, err)
	ipBan := security.NewMaxBadAttemptsBanList(3, time.Hour, nil)
	cl := newAuthTestListenerWithIPBan(t, clientsauth.NewSingleProvider("agent-x", hashed), ipBan)

	for i := 0; i < 10; i++ {
		_, err := cl.authUser(&authConnMock{user: "agent-x", remote: remoteAgent}, []byte("wrong-pw"))
		require.Error(t, err)
	}
	assert.False(t, ipBan.IsBanned(remoteAgent.String()),
		"an agent retrying the same wrong credential must not ban its own IP")
}

// TestAuthUserDistinctWrongCredentialsBanIP confirms brute-force protection is
// preserved: enough DISTINCT wrong credentials from an IP still ban it.
func TestAuthUserDistinctWrongCredentialsBanIP(t *testing.T) {
	hashed, err := clientsauth.HashPassword("correct-pw")
	require.NoError(t, err)
	ipBan := security.NewMaxBadAttemptsBanList(3, time.Hour, nil)
	cl := newAuthTestListenerWithIPBan(t, clientsauth.NewSingleProvider("agent-x", hashed), ipBan)

	for _, pw := range []string{"guess-1", "guess-2", "guess-3"} {
		_, err := cl.authUser(&authConnMock{user: "agent-x", remote: remoteAgent}, []byte(pw))
		require.Error(t, err)
	}
	assert.True(t, ipBan.IsBanned(remoteAgent.String()),
		"three distinct wrong credentials must ban the IP")
}

// TestAuthUserLoopbackPeerIsNeverIPBanned guards agents behind a relay on the
// server's own host -- a Tor onion service or a local reverse proxy. They all
// arrive from one loopback address, so banning it would let a few made-up
// credentials lock every one of them out.
func TestAuthUserLoopbackPeerIsNeverIPBanned(t *testing.T) {
	hashed, err := clientsauth.HashPassword("correct-pw")
	require.NoError(t, err)

	for _, peer := range []string{"127.0.0.1", "127.0.0.53", "::1", "::ffff:127.0.0.1"} {
		t.Run(peer, func(t *testing.T) {
			ipBan := security.NewMaxBadAttemptsBanList(3, time.Hour, nil)
			cl := newAuthTestListenerWithIPBan(t, clientsauth.NewSingleProvider("agent-x", hashed), ipBan)
			remote := net.ParseIP(peer)

			for _, id := range []string{"guess-1", "guess-2", "guess-3", "guess-4", "guess-5"} {
				_, err := cl.authUser(&authConnMock{user: id, remote: remote}, []byte("made-up"))
				require.Error(t, err)
			}
			assert.False(t, ipBan.IsBanned(cl.getIP(&net.TCPAddr{IP: remote})),
				"a loopback relay address must not be banned")

			_, err := cl.authUser(&authConnMock{user: "agent-x", remote: remote}, []byte("correct-pw"))
			assert.NoError(t, err, "a genuine agent behind the relay must still log in")
		})
	}
}

// TestAuthUserLoopbackPeerStillWaitsPerAuthID confirms what still throttles
// guessing through a relay: a failed login makes that client auth id wait.
func TestAuthUserLoopbackPeerStillWaitsPerAuthID(t *testing.T) {
	hashed, err := clientsauth.HashPassword("correct-pw")
	require.NoError(t, err)
	cl := newAuthTestListener(t, clientsauth.NewSingleProvider("agent-x", hashed),
		logger.NewLogger("authuser-test", logger.LogOutput{File: os.Stdout}, logger.LogLevelInfo))

	_, err = cl.authUser(&authConnMock{user: "agent-x"}, []byte("guess-1"))
	require.Error(t, err)
	_, err = cl.authUser(&authConnMock{user: "agent-x"}, []byte("correct-pw"))
	assert.ErrorIs(t, err, ErrTooManyRequests, "the per-auth-id wait must still apply to a loopback peer")
}

// TestAuthUserFailureIsLoggedAtInfo guards the log level: the reason for a
// failed login must be visible at the default INFO log level (it used to log at
// DEBUG, i.e. invisibly, which made an empty/wrong credential look like a
// network fault). The reason is captured from a log file written at INFO level.
func TestAuthUserFailureIsLoggedAtInfo(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "server.log")
	f, err := os.Create(logPath) //nolint:gosec // test-controlled temp path
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	// INFO level: a Debugf message would NOT appear, so this asserts the bump.
	log := logger.NewLogger("authuser-test", logger.LogOutput{File: f}, logger.LogLevelInfo)

	t.Run("bad password", func(t *testing.T) {
		hashed, err := clientsauth.HashPassword("right")
		require.NoError(t, err)
		cl := newAuthTestListener(t, clientsauth.NewSingleProvider("agent-x", hashed), log)
		_, err = cl.authUser(&authConnMock{user: "agent-x"}, []byte("wrong"))
		require.Error(t, err)
	})

	t.Run("unknown id", func(t *testing.T) {
		cl := newAuthTestListener(t, clientsauth.NewSingleProvider("agent-x", "pw"), log)
		_, err := cl.authUser(&authConnMock{user: "ghost"}, []byte("pw"))
		require.Error(t, err)
	})

	require.NoError(t, f.Sync())
	data, err := os.ReadFile(logPath) //nolint:gosec // test-controlled temp path
	require.NoError(t, err)
	out := string(data)

	assert.Contains(t, out, "Login failed for client auth id", "failure must be logged at INFO level")
	assert.Contains(t, out, "bad password", "the bad-password reason must be visible")
	assert.Contains(t, out, "unknown client auth id", "the unknown-id reason must be visible")
	// The presented password must never be logged.
	assert.NotContains(t, out, "wrong", "the presented password must not be logged")
}
