package e2e

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"

	"github.com/proximile/proxiport/share/logger"
)

var testLog = logger.NewLogger("e2e", logger.LogOutput{File: os.Stdout}, logger.LogLevelDebug)

func newKey(t *testing.T) ssh.Signer {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	s, err := ssh.NewSignerFromKey(priv)
	require.NoError(t, err)
	return s
}

// echoServer accepts connections and writes back what it reads.
func echoServer(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				_, _ = io.Copy(c, c)
				_ = c.Close()
			}()
		}
	}()
	return l.Addr().String()
}

type fixture struct {
	srv      *Server
	dir      string
	operator ssh.Signer
	hostKey  ssh.PublicKey
}

func startServer(t *testing.T, allowed func(string) bool) *fixture {
	t.Helper()
	dir := t.TempDir()
	operator := newKey(t)
	authPath := filepath.Join(dir, "authorized_keys")
	require.NoError(t, os.WriteFile(authPath, ssh.MarshalAuthorizedKey(operator.PublicKey()), 0o600))

	srv, err := New(Config{
		Listen:             "127.0.0.1:0",
		HostKeyFile:        filepath.Join(dir, "host_key"),
		AuthorizedKeysFile: authPath,
		Allowed:            allowed,
	}, testLog)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	require.NoError(t, srv.Start(ctx))
	t.Cleanup(func() {
		cancel()
		srv.Wait()
	})

	_, hostKey, err := Fingerprint(filepath.Join(dir, "host_key"))
	require.NoError(t, err)
	return &fixture{srv: srv, dir: dir, operator: operator, hostKey: hostKey}
}

func (f *fixture) dial(signer ssh.Signer, pinned ssh.PublicKey) (*ssh.Client, error) {
	return ssh.Dial("tcp", f.srv.Addr().String(), &ssh.ClientConfig{
		User:            "operator",
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.FixedHostKey(pinned),
	})
}

func allowAll(string) bool { return true }

func TestForwardThroughPinnedSession(t *testing.T) {
	target := echoServer(t)
	f := startServer(t, allowAll)

	client, err := f.dial(f.operator, f.hostKey)
	require.NoError(t, err)
	defer func() { _ = client.Close() }()

	conn, err := client.Dial("tcp", target)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	_, err = conn.Write([]byte("payload"))
	require.NoError(t, err)
	buf := make([]byte, len("payload"))
	_, err = io.ReadFull(conn, buf)
	require.NoError(t, err)
	assert.Equal(t, "payload", string(buf))
}

// The point of the feature: a relay that answers with any other host key,
// as a server intercepting the session would have to, is refused.
func TestWrongHostKeyIsRefused(t *testing.T) {
	f := startServer(t, allowAll)

	_, err := f.dial(f.operator, newKey(t).PublicKey())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "host key mismatch")
}

func TestUnknownOperatorKeyIsRefused(t *testing.T) {
	f := startServer(t, allowAll)

	_, err := f.dial(newKey(t), f.hostKey)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unable to authenticate")
}

// authorized_keys is read on every attempt, so a key added later works
// without a restart and a removed key stops working.
func TestAuthorizedKeysAreReread(t *testing.T) {
	f := startServer(t, allowAll)
	second := newKey(t)
	authPath := filepath.Join(f.dir, "authorized_keys")

	_, err := f.dial(second, f.hostKey)
	require.Error(t, err)

	require.NoError(t, os.WriteFile(authPath, ssh.MarshalAuthorizedKey(second.PublicKey()), 0o600))
	client, err := f.dial(second, f.hostKey)
	require.NoError(t, err)
	_ = client.Close()

	_, err = f.dial(f.operator, f.hostKey)
	require.Error(t, err, "the removed key must no longer authenticate")
}

func TestForwardRefusedByPolicy(t *testing.T) {
	target := echoServer(t)
	var asked string
	f := startServer(t, func(remote string) bool {
		asked = remote
		return false
	})

	client, err := f.dial(f.operator, f.hostKey)
	require.NoError(t, err)
	defer func() { _ = client.Close() }()

	_, err = client.Dial("tcp", target)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not allowed")
	assert.Equal(t, target, asked)
}

func TestShellAndReverseForwardsAreRefused(t *testing.T) {
	f := startServer(t, allowAll)

	client, err := f.dial(f.operator, f.hostKey)
	require.NoError(t, err)
	defer func() { _ = client.Close() }()

	_, err = client.NewSession()
	require.Error(t, err)

	_, err = client.Listen("tcp", "127.0.0.1:0")
	require.Error(t, err)
}

func TestHostKeyIsCreatedOnceAndKept(t *testing.T) {
	path := filepath.Join(t.TempDir(), "host_key")

	_, _, err := Fingerprint(path)
	require.Error(t, err, "Fingerprint must not create a key")

	created, err := LoadOrCreateHostKey(path)
	require.NoError(t, err)
	fp, pub, err := Fingerprint(path)
	require.NoError(t, err)
	assert.Equal(t, ssh.FingerprintSHA256(created.PublicKey()), fp)
	assert.Equal(t, created.PublicKey().Marshal(), pub.Marshal())

	again, err := LoadOrCreateHostKey(path)
	require.NoError(t, err)
	assert.Equal(t, created.PublicKey().Marshal(), again.PublicKey().Marshal(), "an existing key is kept")

	if runtime.GOOS != "windows" {
		st, err := os.Stat(path)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), st.Mode().Perm())
	}
}

func TestReadableHostKeyIsRefused(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits are not checked on Windows")
	}
	path := filepath.Join(t.TempDir(), "host_key")
	_, err := LoadOrCreateHostKey(path)
	require.NoError(t, err)
	require.NoError(t, os.Chmod(path, 0o644)) //nolint:gosec // G302: the test makes the key readable on purpose

	_, err = LoadOrCreateHostKey(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "accessible by other users")
}

func TestValidateListen(t *testing.T) {
	for _, ok := range []string{"127.0.0.1:7222", "[::1]:7222", "127.0.0.2:22"} {
		assert.NoError(t, ValidateListen(ok), ok)
	}
	for _, bad := range []string{"0.0.0.0:7222", "192.0.2.10:7222", "localhost:7222", "127.0.0.1", "127.0.0.1:0", "127.0.0.1:70000"} {
		assert.Error(t, ValidateListen(bad), bad)
	}
}
