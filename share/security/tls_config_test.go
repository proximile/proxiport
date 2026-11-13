package security

import (
	"crypto/tls"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// handshake connects a TLS client with the given key-exchange preferences
// (nil for the Go defaults) to a server built from TLSConfig and returns the
// group both sides agreed on.
func handshake(t *testing.T, tlsMin string, clientCurves []tls.CurveID) (tls.CurveID, error) {
	t.Helper()

	cert, err := tls.LoadX509KeyPair("../../testdata/certs/proxiport.test.crt", "../../testdata/certs/proxiport.test.key")
	require.NoError(t, err)

	serverConfig := TLSConfig(tlsMin)
	serverConfig.Certificates = []tls.Certificate{cert}

	listener, err := tls.Listen("tcp", "127.0.0.1:0", serverConfig)
	require.NoError(t, err)
	defer func() { _ = listener.Close() }()

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		_ = conn.(*tls.Conn).Handshake()
		_ = conn.Close()
	}()

	clientConfig := &tls.Config{
		InsecureSkipVerify: true, //nolint:gosec // G402: the test certificate is self-signed; only the key exchange is under test
		CurvePreferences:   clientCurves,
	}
	raw, err := net.Dial("tcp", listener.Addr().String())
	require.NoError(t, err)
	defer func() { _ = raw.Close() }()

	client := tls.Client(raw, clientConfig)
	if err := client.Handshake(); err != nil {
		return 0, err
	}
	return client.ConnectionState().CurveID, nil
}

// The server leaves the choice of key-exchange groups to Go's defaults,
// which put the hybrid post-quantum group first. A list of its own would
// have to be kept current by hand; the one that used to be set here
// allowed only the three NIST curves, so no connection could use the
// post-quantum group and a client offering only X25519 was refused.
func TestServerNegotiatesThePostQuantumGroupWithACurrentClient(t *testing.T) {
	for _, tlsMin := range []string{"", "1.2", "1.3"} {
		group, err := handshake(t, tlsMin, nil)
		require.NoError(t, err, "tls_min %q", tlsMin)
		assert.Equal(t, tls.X25519MLKEM768, group, "tls_min %q", tlsMin)
	}
}

func TestServerStillAcceptsClassicalClients(t *testing.T) {
	for _, curve := range []tls.CurveID{tls.X25519, tls.CurveP256, tls.CurveP384, tls.CurveP521} {
		group, err := handshake(t, "", []tls.CurveID{curve})
		require.NoError(t, err, "a client that offers only %v", curve)
		assert.Equal(t, curve, group)
	}
}

func TestTLSMinVersion(t *testing.T) {
	assert.Equal(t, uint16(tls.VersionTLS13), TLSConfig("").MinVersion)
	assert.Equal(t, uint16(tls.VersionTLS13), TLSConfig("1.3").MinVersion)
	assert.Equal(t, uint16(tls.VersionTLS12), TLSConfig("1.2").MinVersion)
}
