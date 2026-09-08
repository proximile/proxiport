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
