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
// auth id) and a remote address, which is all authUser reads.
type authConnMock struct {
	user string
}

func (m *authConnMock) User() string          { return m.user }
func (m *authConnMock) SessionID() []byte     { return []byte("session") }
func (m *authConnMock) ClientVersion() []byte { return []byte("SSH-2.0-test") }
func (m *authConnMock) ServerVersion() []byte { return []byte("SSH-2.0-test") }
func (m *authConnMock) LocalAddr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 8080}
}
func (m *authConnMock) RemoteAddr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 54321}
}

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

// TestAuthUserBcryptCredentialAuthenticates is the direct end-to-end guard for
// field report #1 ("API-created credentials can't authenticate"): a credential
// stored as a bcrypt hash must authenticate an agent that presents the matching
// plaintext. This is the scenario an API-created credential produces.
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

// TestAuthUserFailureIsLoggedAtInfo guards field report #3: the reason for a
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
