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
	overwritten, err := shredFile(p, info.Size())
	require.NoError(t, err)
	assert.True(t, overwritten, "a writable file should be overwritten before it is unlinked")
	assert.NoFileExists(t, p)
}

// TestShredFileRemovesEvenWhenItCannotOverwrite is H11's shape. The packaged
// installer wrote these files 0640 root:proxiport while the daemon ran as
// proxiport, so the O_WRONLY open returned EACCES -- and the old code returned
// on that error before ever reaching the unlink. The cleartext admin password
// therefore stayed on disk on EVERY packaged install, while docs/install.md
// said it was gone after the first login.
//
// Unlinking needs write permission on the DIRECTORY, not on the file, so it
// would have succeeded all along. A directory target is used to make the open
// fail deterministically on any builder, including one running as root, where
// a permission-based refusal cannot be constructed at all.
func TestShredFileRemovesEvenWhenItCannotOverwrite(t *testing.T) {
	target := filepath.Join(t.TempDir(), "cannot-open-for-writing")
	require.NoError(t, os.Mkdir(target, 0o750))

	info, err := os.Stat(target)
	require.NoError(t, err)
	require.Positive(t, info.Size(), "the size>0 branch is the one that opens the file")

	overwritten, err := shredFile(target, info.Size())
	require.NoError(t, err, "a failed overwrite must not stop the removal")
	assert.False(t, overwritten)
	assert.NoFileExists(t, target)
}

// TestShredFileOnAFileItCannotWrite is the real permission case. It needs a
// non-root builder, because root can open anything for writing; the directory
// case above is what keeps this covered when that is not available.
func TestShredFileOnAFileItCannotWrite(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: a permission-denied open cannot be constructed")
	}

	dir := t.TempDir()
	p := filepath.Join(dir, "read-only-secret")
	require.NoError(t, os.WriteFile(p, []byte("admin:s3cret\n"), 0600))
	require.NoError(t, os.Chmod(p, 0400))

	info, err := os.Stat(p)
	require.NoError(t, err)

	overwritten, err := shredFile(p, info.Size())
	require.NoError(t, err)
	assert.False(t, overwritten)
	assert.NoFileExists(t, p, "the credential must be gone even when it could not be overwritten")
}
