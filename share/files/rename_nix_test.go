//go:build !windows
// +build !windows

package files

import (
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// crossDevice makes every rename fail as it does between two filesystems.
func crossDevice(t *testing.T) {
	t.Helper()
	orig := rename
	rename = func(oldPath, newPath string) error {
		return &os.LinkError{Op: "rename", Old: oldPath, New: newPath, Err: syscall.EXDEV}
	}
	t.Cleanup(func() { rename = orig })
}

func writeStaged(t *testing.T, dir, content string, mode os.FileMode) string {
	t.Helper()
	src := filepath.Join(dir, "staged")
	require.NoError(t, os.WriteFile(src, []byte(content), 0o600))
	require.NoError(t, os.Chmod(src, mode))
	return src
}

func TestRenameCopiesAcrossFilesystems(t *testing.T) {
	crossDevice(t)
	stage, destDir := t.TempDir(), t.TempDir()
	src := writeStaged(t, stage, "new content", 0o640)
	dst := filepath.Join(destDir, "app.conf")
	require.NoError(t, os.WriteFile(dst, []byte("old content that is longer"), 0o600))

	require.NoError(t, Rename(src, dst))

	got, err := os.ReadFile(dst) //nolint:gosec // G304: a path under t.TempDir
	require.NoError(t, err)
	assert.Equal(t, "new content", string(got))

	fi, err := os.Stat(dst)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o640), fi.Mode().Perm())

	_, err = os.Stat(src)
	assert.True(t, os.IsNotExist(err), "the staged file must be removed once the destination is written")

	entries, err := os.ReadDir(destDir)
	require.NoError(t, err)
	require.Len(t, entries, 1, "no temp file may be left next to the destination")
	assert.Equal(t, "app.conf", entries[0].Name())
}

func TestRenameAcrossFilesystemsFailureLeavesDestinationAlone(t *testing.T) {
	crossDevice(t)
	stage := t.TempDir()
	src := writeStaged(t, stage, "payload", 0o644)
	dst := filepath.Join(t.TempDir(), "missing", "app.conf")

	err := Rename(src, dst)
	require.Error(t, err)

	_, statErr := os.Stat(filepath.Dir(dst))
	assert.True(t, os.IsNotExist(statErr), "Rename must not create the destination directory")
	_, statErr = os.Stat(src)
	assert.NoError(t, statErr, "the staged file stays for the caller to clean up")
}

func TestRenameSameFilesystem(t *testing.T) {
	dir := t.TempDir()
	src := writeStaged(t, dir, "payload", 0o600)
	dst := filepath.Join(dir, "app.conf")

	require.NoError(t, Rename(src, dst))

	got, err := os.ReadFile(dst) //nolint:gosec // G304: a path under t.TempDir
	require.NoError(t, err)
	assert.Equal(t, "payload", string(got))
}

func asRoot(t *testing.T) {
	t.Helper()
	orig := geteuid
	geteuid = func() int { return 0 }
	t.Cleanup(func() { geteuid = orig })
}

// Running as root, the owner is set with a direct chown: there is no sudo
// involved, so it works on a host that has none installed.
func TestChangeOwnerAsRootChownsDirectly(t *testing.T) {
	asRoot(t)
	t.Setenv("PATH", "")
	path := writeStaged(t, t.TempDir(), "payload", 0o644)

	// The current ids are the only ones a test without privileges may set.
	uid, gid := strconv.Itoa(os.Getuid()), strconv.Itoa(os.Getgid())
	require.NoError(t, ChangeOwner(path, uid, gid))
	require.NoError(t, ChangeOwner(path, uid, ""))
	require.NoError(t, ChangeOwner(path, "", gid))

	gotUID, gotGID, err := GetFileUIDAndGID(path)
	require.NoError(t, err)
	assert.Equal(t, uid, strconv.FormatUint(uint64(gotUID), 10))
	assert.Equal(t, gid, strconv.FormatUint(uint64(gotGID), 10))
}

func TestChangeOwnerAsRootReportsUnknownUser(t *testing.T) {
	asRoot(t)
	path := writeStaged(t, t.TempDir(), "payload", 0o644)

	err := ChangeOwner(path, "no-such-user-example", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no-such-user-example")
}

func TestLookupOwnerAndGroupLeavesEmptySideUnchanged(t *testing.T) {
	uid, gid, err := lookupOwnerAndGroup("", "")
	require.NoError(t, err)
	assert.Equal(t, -1, uid)
	assert.Equal(t, -1, gid)

	uid, gid, err = lookupOwnerAndGroup("", "12345")
	require.NoError(t, err)
	assert.Equal(t, -1, uid)
	assert.Equal(t, 12345, gid)
}

// A user without a group gets that user's login group, as "chown user:" does.
func TestLookupOwnerAndGroupUsesLoginGroup(t *testing.T) {
	u, err := user.LookupId("0")
	if err != nil {
		t.Skip("no user with id 0 in this environment")
	}
	wantGID, err := strconv.Atoi(u.Gid)
	require.NoError(t, err)

	for _, owner := range []string{u.Username, "0"} {
		uid, gid, err := lookupOwnerAndGroup(owner, "")
		require.NoError(t, err)
		assert.Equal(t, 0, uid)
		assert.Equal(t, wantGID, gid)
	}
}
