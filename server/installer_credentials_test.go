package chserver

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/proximile/proxiport/server/chconfig"
	"github.com/proximile/proxiport/share/logger"
)

func testListenerWithDataDir(t *testing.T, dir string) *APIListener {
	t.Helper()
	cfg := &chconfig.Config{}
	cfg.Server.DataDir = dir
	return &APIListener{
		Logger: logger.NewLogger("test", logger.LogOutput{File: os.Stdout}, logger.LogLevelError),
		Server: &Server{config: cfg},
	}
}

func TestShredConsumedInstallerCreds(t *testing.T) {
	dir := t.TempDir()
	admin := filepath.Join(dir, "initial-admin-password")
	client := filepath.Join(dir, "initial-client-auth")
	require.NoError(t, os.WriteFile(admin, []byte("admin:s3cret\n"), 0600))
	require.NoError(t, os.WriteFile(client, []byte("client1:s3cret\n"), 0600))

	al := testListenerWithDataDir(t, dir)
	al.shredConsumedInstallerCreds()

	assert.NoFileExists(t, admin, "initial-admin-password must be shredded")
	assert.NoFileExists(t, client, "initial-client-auth must be shredded")

	// Idempotent: a second call with the files already gone is a no-op.
	assert.NotPanics(t, func() { al.shredConsumedInstallerCreds() })
}

func TestShredConsumedInstallerCreds_NoDataDir(t *testing.T) {
	al := testListenerWithDataDir(t, "")
	assert.NotPanics(t, func() { al.shredConsumedInstallerCreds() })
}

func TestShredFileRemovesContent(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "secret")
	require.NoError(t, os.WriteFile(p, []byte("supersecret"), 0600))

	info, err := os.Stat(p)
	require.NoError(t, err)
	require.NoError(t, shredFile(p, info.Size()))
	assert.NoFileExists(t, p)
}
