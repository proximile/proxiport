//go:build !windows
// +build !windows

package chclient

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/proximile/proxiport/share/logger"
	"github.com/proximile/proxiport/share/models"
)

var globTestLog = logger.NewLogger("glob-test", logger.LogOutput{File: os.Stdout}, logger.LogLevelError)

func refused(t *testing.T, destination string) bool {
	t.Helper()
	uf := models.UploadedFile{DestinationPath: destination}
	return uf.ValidateDestinationPath(FileReceptionGlobs, globTestLog) != nil
}

// The shipped default list is the only thing standing between a hostile server
// and code execution on the agent, so it is exercised as shipped rather than
// through a hand-written sample.
//
// The executable-search-path and pseudo-filesystem entries were plain globs,
// and filepath.Match cannot cross a separator -- so "/run" protected nothing
// under /run, and "/usr/lib*" protected a file written directly into /usr/lib
// but not one in /usr/lib/x86_64-linux-gnu/. Every path below is one level or
// more past the protected element.
func TestFileReceptionGlobsProtectBelowTheFirstLevel(t *testing.T) {
	blocked := []string{
		// A systemd unit search path that outranks /usr/lib/systemd/system.
		"/run/systemd/system/backdoor.service",
		// Shared objects loaded into every process, and into sudo.
		"/usr/lib/x86_64-linux-gnu/libnss_files.so.2",
		"/lib/x86_64-linux-gnu/libc.so.6",
		"/usr/lib/sudo/sudoers.so",
		// Boot and kernel surfaces.
		"/boot/grub/grub.cfg",
		"/proc/sys/kernel/core_pattern",
		"/sys/kernel/uevent_helper",
		"/dev/shm/payload",
		// The first level must stay protected too.
		"/bin/ls",
		"/usr/local/sbin/thing",
	}
	for _, destination := range blocked {
		assert.True(t, refused(t, destination), "%s must be refused", destination)
	}
}

// FileReceptionGlobs is built under //go:build !windows, so it is the default
// on the shipped darwin_amd64/darwin_arm64 agents as well -- but its
// "Anything that runs at boot, or defines a service" block listed only systemd
// and SysV paths. launchd is macOS's equivalent and was absent entirely, so the
// one class of destination the list exists to block was open there.
func TestFileReceptionGlobsProtectLaunchdAndPeriodic(t *testing.T) {
	blocked := []string{
		"/Library/LaunchDaemons/com.vendor.updater.plist",
		"/Library/LaunchAgents/com.vendor.updater.plist",
		"/System/Library/LaunchDaemons/com.apple.thing.plist",
		"/Users/operator/Library/LaunchAgents/com.vendor.updater.plist",
		"/Library/StartupItems/evil/evil",
		"/etc/periodic/daily/999.backdoor",
	}
	for _, destination := range blocked {
		assert.True(t, refused(t, destination), "%s must be refused", destination)
	}
}

// Anti-vacuity. A protected list that refuses everything is useless: a file
// push is a supported feature, and these are the ordinary destinations it
// exists for.
func TestFileReceptionGlobsStillAllowOrdinaryDestinations(t *testing.T) {
	allowed := []string{
		"/srv/app/config.yaml",
		"/opt/vendor/app/data.txt",
		"/home/operator/report.csv",
		"/var/tmp/payload.bin",
		"/usr/share/app/template.html",
	}
	for _, destination := range allowed {
		assert.False(t, refused(t, destination), "%s must be allowed", destination)
	}
	require.NotEmpty(t, FileReceptionGlobs)
}
