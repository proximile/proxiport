package chclient

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"

	chshare "github.com/proximile/proxiport/share"
	"github.com/proximile/proxiport/share/clientconfig"
	"github.com/proximile/proxiport/share/logger"
)

func testHostKey(t *testing.T) ssh.PublicKey {
	t.Helper()
	pemBytes, err := chshare.GenerateKey("fingerprint-test-seed")
	require.NoError(t, err)
	signer, err := ssh.ParsePrivateKey(pemBytes)
	require.NoError(t, err)
	return signer.PublicKey()
}

func clientWithPin(pin string) *Client {
	return &Client{
		Logger: logger.NewLogger("fp-test", logger.LogOutput{File: os.Stdout}, logger.LogLevelError),
		configHolder: &ClientConfigHolder{
			Config: &clientconfig.Config{
				Client: clientconfig.ClientConfig{Fingerprint: pin},
			},
		},
	}
}

func TestVerifyServerFingerprint(t *testing.T) {
	key := testHostKey(t)
	sha256FP := chshare.FingerprintKeySHA256(key)
	md5FP := chshare.FingerprintKey(key)

	t.Run("correct SHA-256 pin verifies", func(t *testing.T) {
		require.NoError(t, clientWithPin(sha256FP).verifyServer("h", nil, key))
	})

	t.Run("wrong SHA-256 pin is rejected", func(t *testing.T) {
		bad := sha256FP[:len(sha256FP)-2] + "xy"
		require.Error(t, clientWithPin(bad).verifyServer("h", nil, key))
	})

	t.Run("truncated SHA-256 pin is rejected (no prefix match)", func(t *testing.T) {
		require.Error(t, clientWithPin(sha256FP[:20]).verifyServer("h", nil, key))
	})

	t.Run("empty pin is accepted on trust", func(t *testing.T) {
		require.NoError(t, clientWithPin("").verifyServer("h", nil, key))
	})

	t.Run("legacy full MD5 pin still verifies", func(t *testing.T) {
		require.NoError(t, clientWithPin(md5FP).verifyServer("h", nil, key))
	})

	t.Run("legacy MD5 prefix still verifies (back-compat)", func(t *testing.T) {
		require.NoError(t, clientWithPin(md5FP[:8]).verifyServer("h", nil, key))
	})

	t.Run("wrong MD5 pin is rejected", func(t *testing.T) {
		require.Error(t, clientWithPin("00:11:22:33").verifyServer("h", nil, key))
	})
}
