package files_test

import (
	"crypto/md5" //nolint:gosec // matching the checksum the file-push protocol uses
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/proximile/proxiport/share/files"
)

// TestCreateFileReplacesRatherThanOverwritesInPlace is M12.
//
// The server stages a file push at a path derived from a caller-chosen upload
// id, and then computes the md5 the agent verifies against by re-reading that
// file from disk. Without O_TRUNC a shorter second write left the first
// payload's tail in place, so the agent received a spliced file WITH A MATCHING
// CHECKSUM, verified it, and wrote the corrupt result to the managed host
// reporting success. Reusing an id -- automation pushing "id=nginx-conf" twice
// -- was the whole precondition.
func TestCreateFileReplacesRatherThanOverwritesInPlace(t *testing.T) {
	fs := files.NewFileSystem()
	path := filepath.Join(t.TempDir(), "staged_proxiport_filepush")

	first := "benign-PAYLOAD-FROM-FIRST-UPLOAD"
	second := "second"

	written, err := fs.CreateFile(path, strings.NewReader(first))
	require.NoError(t, err)
	require.Equal(t, int64(len(first)), written)

	written, err = fs.CreateFile(path, strings.NewReader(second))
	require.NoError(t, err)
	require.Equal(t, int64(len(second)), written)

	onDisk, err := os.ReadFile(path) //nolint:gosec // path built in this test
	require.NoError(t, err)

	assert.Equal(t, second, string(onDisk), "the second payload must replace the first, not be laid over it")
	assert.NotContains(t, string(onDisk), "FROM-FIRST-UPLOAD")

	// The checksum the agent is handed is taken from the file, so a splice is
	// self-certifying: it is only caught by comparing against the payload the
	// operator actually pushed.
	assert.Equal(t, md5Hex(second), md5Hex(string(onDisk)),
		"the staged file's checksum must be the checksum of what was pushed")

	// And the reported size must be the file's size, not merely the bytes this
	// call wrote -- which is the same number only because the file was replaced.
	stat, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, written, stat.Size())
}

func md5Hex(s string) string {
	sum := md5.Sum([]byte(s)) //nolint:gosec // matching the checksum the file-push protocol uses
	return hex.EncodeToString(sum[:])
}
