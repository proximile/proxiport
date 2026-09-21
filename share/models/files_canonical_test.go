package models

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The protected-destination filter compared lexical strings, so every protected
// path was reachable by spelling it differently. On macOS /etc, /var and /tmp
// are symlinks into /private, so "/private/etc/sudoers.d/x" matched nothing
// while landing in the real sudoers.d; on Windows the same held for 8.3 short
// names and \\?\ prefixes.
//
// The alias class is tested here with real symlinks in a temp tree rather than
// with macOS paths, so it runs on the CI platform instead of being skipped on
// it -- a skipped test for a filesystem-shaped bug is not a test.
func TestValidateDestinationPathResolvesSymlinkedAliases(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "private", "etc", "sudoers.d")
	require.NoError(t, os.MkdirAll(real, 0o750))
	// root/etc -> root/private/etc, exactly the macOS shape.
	require.NoError(t, os.Symlink(
		filepath.Join(root, "private", "etc"), filepath.Join(root, "etc")))

	protected := []string{filepath.Join(root, "etc", "sudoers.d") + "/**"}

	// The spelling the pattern names.
	direct := UploadedFile{DestinationPath: filepath.Join(root, "etc", "sudoers.d", "99-pwn")}
	assert.Error(t, direct.ValidateDestinationPath(protected, protectedTestLog),
		"the pattern's own spelling must be refused")

	// The alias that reaches the same directory.
	viaAlias := UploadedFile{DestinationPath: filepath.Join(root, "private", "etc", "sudoers.d", "99-pwn")}
	assert.Error(t, viaAlias.ValidateDestinationPath(protected, protectedTestLog),
		"the symlinked alias reaches the same file and must be refused")
}

// The alias can equally be on the pattern's side: an operator may write the
// canonical path in their protected list while the push uses the symlink.
func TestValidateDestinationPathResolvesAliasOnEitherSide(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "private", "etc", "ssh")
	require.NoError(t, os.MkdirAll(real, 0o750))
	require.NoError(t, os.Symlink(
		filepath.Join(root, "private", "etc"), filepath.Join(root, "etc")))

	protected := []string{filepath.Join(root, "private", "etc", "ssh") + "/**"}

	viaSymlink := UploadedFile{DestinationPath: filepath.Join(root, "etc", "ssh", "sshd_config")}
	assert.Error(t, viaSymlink.ValidateDestinationPath(protected, protectedTestLog),
		"a push through the symlink must be refused when the list names the real path")
}

// Anti-vacuity: resolution must not start refusing everything. A destination
// that genuinely sits outside the protected subtree still has to be allowed,
// including one next to it and one reached through the same symlink.
func TestValidateDestinationPathResolutionDoesNotOverBlock(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "private", "etc", "sudoers.d"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "private", "srv", "app"), 0o750))
	require.NoError(t, os.Symlink(
		filepath.Join(root, "private", "etc"), filepath.Join(root, "etc")))

	protected := []string{filepath.Join(root, "etc", "sudoers.d") + "/**"}

	allowed := []string{
		filepath.Join(root, "private", "srv", "app", "config.yaml"),
		filepath.Join(root, "etc", "motd"),
		filepath.Join(root, "private", "etc", "sudoers.d.backup", "notes"),
	}
	for _, destination := range allowed {
		uf := UploadedFile{DestinationPath: destination}
		assert.NoError(t, uf.ValidateDestinationPath(protected, protectedTestLog),
			"%s is outside the protected subtree and must be allowed", destination)
	}
}

// A destination whose parents do not exist cannot be resolved at all. It must
// still be matched lexically rather than sailing through, since a hostile
// server can name any path it likes.
func TestValidateDestinationPathMatchesWhenNothingResolves(t *testing.T) {
	uf := UploadedFile{DestinationPath: "/nonexistent-root-xyz/etc/sudoers.d/99-pwn"}
	assert.Error(t, uf.ValidateDestinationPath(
		[]string{"/nonexistent-root-xyz/etc/sudoers.d/**"}, protectedTestLog),
		"an unresolvable path must still be compared lexically")
}

// Case folding is a property of the filesystem, and macOS is case-insensitive
// by default -- so "/Etc/sudoers.d/x" reached the real file while matching
// nothing. pathIsWithin only folded under GOOS=windows. The fold is exercised
// directly so it is covered on a case-sensitive CI platform too.
func TestPathIsWithinFoldsCaseWhenAsked(t *testing.T) {
	assert.True(t, pathIsWithinFold("/Etc/Sudoers.D/99-pwn", "/etc/sudoers.d", true),
		"a case variant must match when the filesystem is case-insensitive")
	assert.False(t, pathIsWithinFold("/Etc/Sudoers.D/99-pwn", "/etc/sudoers.d", false),
		"a case variant must not match when the filesystem is case-sensitive")
	assert.True(t, pathIsWithinFold("/etc/sudoers.d/99-pwn", "/etc/sudoers.d", false),
		"folding off must not break the exact spelling")
}

// Windows' extended-length prefix names the same file while matching no
// pattern, and IsAbsoluteDestination lets it through because it starts with
// `\\`. Pure string work, so it is tested on any platform.
func TestStripExtendedLengthPrefix(t *testing.T) {
	cases := map[string]string{
		`\\?\C:\Windows\System32\drivers\etc\hosts`: `C:\Windows\System32\drivers\etc\hosts`,
		`\\?\UNC\server\share\payload`:              `\\server\share\payload`,
		`C:\Windows\System32\x`:                     `C:\Windows\System32\x`,
		`/etc/sudoers.d/x`:                          `/etc/sudoers.d/x`,
		`\\server\share\x`:                          `\\server\share\x`,
	}
	for in, want := range cases {
		assert.Equal(t, want, stripExtendedLengthPrefix(in), "input %q", in)
	}
}

// The prefixed spelling must get the same verdict as the plain one -- that
// equivalence is the whole bug, and it holds on every platform.
//
// The discriminating run is on Windows, where "\\" is the separator and the
// plain spelling is refused: before the strip, the prefixed spelling was
// allowed there. On a POSIX test runner "\\" is an ordinary character, so
// neither spelling matches and the assertion passes trivially -- which is why
// TestStripExtendedLengthPrefix above carries the real coverage everywhere, and
// why the Windows test binary is compiled in CI.
func TestValidateDestinationPathTreatsExtendedLengthAliasAlike(t *testing.T) {
	protected := []string{`C:\Windows/**`}

	plain := UploadedFile{DestinationPath: `C:\Windows\System32\x.dll`}
	prefixed := UploadedFile{DestinationPath: `\\?\C:\Windows\System32\x.dll`}

	plainRefused := plain.ValidateDestinationPath(protected, protectedTestLog) != nil
	prefixedRefused := prefixed.ValidateDestinationPath(protected, protectedTestLog) != nil

	assert.Equal(t, plainRefused, prefixedRefused,
		"the two spellings name the same file and must get the same verdict")
}
