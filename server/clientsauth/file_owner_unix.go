//go:build unix

package clientsauth

import (
	"os"
	"syscall"
)

// matchOwner gives the file at name the owner and group of the file that
// info describes. It reports whether they match afterwards.
func matchOwner(name string, info os.FileInfo) bool {
	want, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return true
	}
	cur, err := os.Stat(name)
	if err != nil {
		return false
	}
	have, ok := cur.Sys().(*syscall.Stat_t)
	if !ok {
		return true
	}
	if have.Uid == want.Uid && have.Gid == want.Gid {
		return true
	}
	return os.Chown(name, int(want.Uid), int(want.Gid)) == nil
}
