package chclient

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The mode comes from the server and the agent applies it, usually as root.
// Anything above 0777 is setuid, setgid or sticky -- and a setuid binary
// written by a file push is a root shell on the managed host for anyone who can
// run it. Under this project's untrusted-server threat model the server is not
// entitled to that.
func TestChmodFileRefusesSetuidAndFriends(t *testing.T) {
	um := &UploadManager{Logger: testLog}

	for _, mode := range []os.FileMode{
		os.FileMode(0o4755), // setuid
		os.FileMode(0o2755), // setgid
		os.FileMode(0o1777), // sticky
		os.FileMode(0o6755), // setuid + setgid
	} {
		err := um.chmodFile("/tmp/does-not-matter", mode)
		require.Error(t, err, "mode %#o must be refused", mode)
		assert.Contains(t, err.Error(), "only permission bits")
	}
}
