package files

import (
	"io"
	"os"
	"path/filepath"

	"github.com/pkg/errors"
)

// rename is os.Rename, swappable so tests can make a move cross filesystems.
var rename = os.Rename

// copyReplace moves src to dst when a rename cannot, because the two are on
// different filesystems. The content goes into a new file in dst's directory,
// which takes src's permission bits and owner, is synced, and is then renamed
// over dst: dst is either the old file or the complete new one, never a partial
// write. src is removed once dst is in place.
func copyReplace(src, dst string) (err error) {
	in, err := os.Open(src) //nolint:gosec // G304: the agent's own staging file
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	srcInfo, err := in.Stat()
	if err != nil {
		return err
	}

	// CreateTemp creates exclusively, so it never writes through a name that
	// already exists in the destination directory.
	tmp, err := os.CreateTemp(filepath.Dir(dst), "."+filepath.Base(dst)+".*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err = io.Copy(tmp, in); err != nil {
		return errors.Wrapf(err, "failed to copy %s to %s", src, tmpPath)
	}
	if err = tmp.Chmod(srcInfo.Mode().Perm()); err != nil {
		return err
	}
	if err = copyOwner(srcInfo, tmp); err != nil {
		return errors.Wrapf(err, "failed to set the owner of %s", tmpPath)
	}
	if err = tmp.Sync(); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	// Both names are in the same directory, so this cannot cross devices.
	if err = os.Rename(tmpPath, dst); err != nil {
		return err
	}

	_ = in.Close()
	if rmErr := os.Remove(src); rmErr != nil && !os.IsNotExist(rmErr) {
		return errors.Wrapf(rmErr, "%s was written, but removing %s failed", dst, src)
	}

	return nil
}
