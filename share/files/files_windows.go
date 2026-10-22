//go:build windows
// +build windows

package files

import (
	"errors"
	"os"
	"syscall"
)

// errorNotSameDevice is ERROR_NOT_SAME_DEVICE, which a move between volumes
// returns.
const errorNotSameDevice = syscall.Errno(17)

func ChangeOwner(path, owner, group string) error {
	return nil
}

func Rename(oldPath, newPath string) error {
	err := rename(oldPath, newPath)
	if errors.Is(err, errorNotSameDevice) {
		return copyReplace(oldPath, newPath)
	}

	return err
}

func copyOwner(_ os.FileInfo, _ *os.File) error {
	return nil
}

func GetFileUIDAndGID(file string) (uid, gid uint32, err error) {
	return 0, 0, nil
}
