package models

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/proximile/proxiport/share/logger"
)

var protectedTestLog = logger.NewLogger("files-test", logger.LogOutput{File: os.Stdout}, logger.LogLevelError)

// A file push carries a mode and an owner, and the agent will chown as root, so
// a destination that grants a login or runs code is a way around the command
// and script permissions rather than a use of them. An operator holding only
// the uploads permission must not be able to reach any of these.
func TestValidateDestinationPathBlocksRoutesToExecution(t *testing.T) {
	protected := []string{
		"/bin", "/etc/cron.d/**", "/etc/sudoers.d/**", "/etc/systemd/**",
		"/etc/profile.d/**", "/root/**", "/home/*/.ssh/**", "/etc/proxiport/**",
	}

	blocked := []string{
		"/bin/ls",
		"/etc/cron.d/backdoor",
		"/etc/sudoers.d/00-operator",
		"/etc/systemd/system/backdoor.service",
		"/etc/systemd/system/sshd.service.d/override.conf",
		"/etc/profile.d/backdoor.sh",
		"/root/.ssh/authorized_keys",
		"/root/.bashrc",
		"/home/operator/.ssh/authorized_keys",
		"/etc/proxiport/proxiport.conf",

		// The subtree form has to survive a traversal that resolves back inside.
		"/etc/cron.d/../cron.d/backdoor",
		"/etc/systemd/system/../system/backdoor.service",
	}
	for _, destination := range blocked {
		uf := UploadedFile{DestinationPath: destination}
		assert.Error(t, uf.ValidateDestinationPath(protected, protectedTestLog),
			"%s should be refused", destination)
	}

	// File reception is for pushing files; a list that blocks the ordinary case
	// would just be turned off.
	allowed := []string{
		"/opt/app/config.yaml",
		"/var/lib/app/data.db",
		"/home/operator/report.csv",
		"/tmp/upload.tar.gz",
		"/etc/myapp/settings.conf",
	}
	for _, destination := range allowed {
		uf := UploadedFile{DestinationPath: destination}
		assert.NoError(t, uf.ValidateDestinationPath(protected, protectedTestLog),
			"%s should be allowed", destination)
	}
}

// filepath.Match does not cross a separator, so a plain glob can only ever name
// one level. That is the wrong shape for a unit directory, where the dangerous
// file may be several levels down -- which is why the subtree form exists.
func TestValidateDestinationPathSubtreeReachesBelowOneLevel(t *testing.T) {
	deep := UploadedFile{DestinationPath: "/etc/systemd/system/foo.service.d/override.conf"}

	require.NoError(t, deep.ValidateDestinationPath([]string{"/etc/systemd/*"}, protectedTestLog),
		"a single-level glob does not reach this far: that is the gap the subtree form closes")
	assert.Error(t, deep.ValidateDestinationPath([]string{"/etc/systemd/**"}, protectedTestLog))
}

// A protected directory must not shadow a sibling whose name it prefixes.
func TestValidateDestinationPathSubtreeMatchesWholeElements(t *testing.T) {
	sibling := UploadedFile{DestinationPath: "/etc/cron.daily-reports/summary.txt"}
	assert.NoError(t, sibling.ValidateDestinationPath([]string{"/etc/cron.d/**"}, protectedTestLog))
}

// The mode travels from the API caller through the server to the agent, which
// applies it as root. Anything above 0777 is setuid, setgid or sticky, and a
// setuid binary written by a file push is a root shell for whoever can run it.
// Validate is the chokepoint both sides pass through: the server on the
// incoming request, the agent again on what arrives over the transport.
func TestUploadedFileValidateRejectsSetuidModes(t *testing.T) {
	for _, mode := range []os.FileMode{0o4755, 0o2755, 0o1777, 0o6755} {
		uf := UploadedFile{
			SourceFilePath:      "/var/lib/proxiport/filepush/abc",
			DestinationPath:     "/opt/app/bin/tool",
			DestinationFileMode: mode,
		}
		err := uf.Validate()
		require.Error(t, err, "mode %#o must be refused", mode)
		assert.Contains(t, err.Error(), "only permission bits")
	}

	// Ordinary modes, and an unset one, still work.
	for _, mode := range []os.FileMode{0, 0o644, 0o600, 0o755} {
		uf := UploadedFile{
			SourceFilePath:      "/var/lib/proxiport/filepush/abc",
			DestinationPath:     "/opt/app/config.yaml",
			DestinationFileMode: mode,
		}
		assert.NoError(t, uf.Validate(), "mode %#o should be allowed", mode)
	}
}

// A relative destination matched no rule on either side: every entry in the
// server's denied-prefix list and in the agent's FileReceptionGlobs is
// absolute, and nothing required the destination to be. The agent then handed
// the name to os.Rename, which resolves it against the process working
// directory -- "/" under the shipped systemd unit, which sets no
// WorkingDirectory=. So "etc/sudoers.d/x" reached /etc/sudoers.d/x with both
// filters bypassed. This is the regression guard for that.
func TestValidateRejectsRelativeDestination(t *testing.T) {
	relative := []string{
		"etc/sudoers.d/operator",
		"./etc/cron.d/backdoor",
		"etc/crontab",
		"etc/ld.so.preload",
		"usr/lib/systemd/system/backdoor.service",
		"etc/proxiport/proxiport.conf",
		"root/.ssh/authorized_keys",
		"../../etc/shadow",
		"file.txt",
		// Rooted but driveless on Windows: resolves against the current
		// drive, which is the same class of surprise as a relative path.
		`\Windows\System32\drivers\etc\hosts`,
	}
	for _, destination := range relative {
		uf := UploadedFile{SourceFilePath: "/tmp/src", DestinationPath: destination}
		assert.Error(t, uf.Validate(), "%s must be refused as a relative destination", destination)

		// And again at the last-resort filter, which a hostile server reaches
		// without going through the server-side validator at all.
		assert.Error(t, uf.ValidateDestinationPath([]string{"/etc/sudoers.d/**"}, protectedTestLog),
			"%s must be refused by the protected-path filter too", destination)
	}
}

func TestValidateAcceptsAbsoluteDestination(t *testing.T) {
	absolute := []string{
		"/tmp/report.txt",
		"/home/operator/data.csv",
		`C:\Users\operator\report.txt`,
		`c:/Users/operator/report.txt`,
		`\\fileserver\share\report.txt`,
	}
	for _, destination := range absolute {
		uf := UploadedFile{SourceFilePath: "/tmp/src", DestinationPath: destination}
		assert.NoError(t, uf.Validate(), "%s is absolute and must be accepted", destination)
	}
}

func TestIsAbsoluteDestination(t *testing.T) {
	for _, c := range []struct {
		path string
		want bool
	}{
		{"/etc/passwd", true},
		{`C:\Windows`, true},
		{"C:/Windows", true},
		{`\\server\share`, true},
		{"etc/passwd", false},
		{"./etc/passwd", false},
		{"../etc/passwd", false},
		{`\Windows`, false},
		{"C:", false},
		{"1:/nope", false},
		{"", false},
	} {
		assert.Equal(t, c.want, IsAbsoluteDestination(c.path), "IsAbsoluteDestination(%q)", c.path)
	}
}
