//go:build !unix

package clientsauth

import "os"

// matchOwner has nothing to compare on platforms without numeric file
// owners; a replaced file gets the access rules of its directory.
func matchOwner(_ string, _ os.FileInfo) bool {
	return true
}
