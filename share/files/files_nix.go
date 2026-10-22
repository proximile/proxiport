//go:build !windows
// +build !windows

package files

import (
	stderrors "errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"syscall"

	"github.com/pkg/errors"
)

// geteuid is os.Geteuid, swappable so tests can take either branch.
var geteuid = os.Geteuid

// ChangeOwner sets the owner and/or group of path; an empty name leaves that
// side unchanged. An agent running as root changes them directly. Otherwise it
// goes through "sudo -n chown", which needs a passwordless sudo rule.
func ChangeOwner(path, owner, group string) error {
	if owner == "" && group == "" {
		return nil
	}

	if geteuid() != 0 {
		return ChangeOwnerExecWithSudo(path, owner, group)
	}

	uid, gid, err := lookupOwnerAndGroup(owner, group)
	if err != nil {
		return err
	}

	return os.Chown(path, uid, gid)
}

// lookupOwnerAndGroup resolves user and group names, or numeric ids, to the ids
// os.Chown takes. An empty group with a user given resolves to that user's login
// group, as "chown user:" does on the sudo path; any other empty name resolves
// to -1, which leaves that side as it is.
func lookupOwnerAndGroup(owner, group string) (uid, gid int, err error) {
	uid, gid = -1, -1

	if owner != "" {
		var loginGID string
		uid, err = lookupID(owner, func(name string) (string, error) {
			u, err := user.Lookup(name)
			if err != nil {
				return "", err
			}
			loginGID = u.Gid
			return u.Uid, nil
		})
		if err != nil {
			return 0, 0, errors.Wrapf(err, "unknown user %q", owner)
		}
		if loginGID == "" {
			if u, lookupErr := user.LookupId(strconv.Itoa(uid)); lookupErr == nil {
				loginGID = u.Gid
			}
		}
		if group == "" && loginGID != "" {
			if gid, err = strconv.Atoi(loginGID); err != nil {
				return 0, 0, err
			}
		}
	}

	if group != "" {
		gid, err = lookupID(group, func(name string) (string, error) {
			g, err := user.LookupGroup(name)
			if err != nil {
				return "", err
			}
			return g.Gid, nil
		})
		if err != nil {
			return 0, 0, errors.Wrapf(err, "unknown group %q", group)
		}
	}

	return uid, gid, nil
}

func lookupID(name string, lookup func(string) (string, error)) (int, error) {
	idStr, err := lookup(name)
	if err != nil {
		// chown accepts a numeric id that has no name, and so does this.
		if id, convErr := strconv.Atoi(name); convErr == nil && id >= 0 {
			return id, nil
		}
		return 0, err
	}

	return strconv.Atoi(idStr)
}

func GetFileUIDAndGID(file string) (uid, gid uint32, err error) {
	fi, err := os.Stat(file)
	if err != nil {
		return 0, 0, err
	}

	if statt, ok := fi.Sys().(*syscall.Stat_t); ok {
		return statt.Uid, statt.Gid, nil
	}

	return 0, 0, nil
}

func ChangeOwnerExecWithSudo(path, owner, group string) error {
	if owner == "" && group == "" {
		return nil
	}

	chownFullPath, err := exec.LookPath("chown")
	if err != nil {
		return err
	}

	args := []string{
		"sudo",
		"-n",
		chownFullPath,
		fmt.Sprintf("%s:%s", owner, group),
		path,
	}

	cmd := exec.Command(args[0], args[1:]...) //nolint:gosec

	output, err := cmd.CombinedOutput()

	if err != nil {
		return errors.Wrapf(err, "failed to execute %s: %s", cmd.String(), string(output))
	}

	return nil
}

// Rename moves oldPath to newPath. When the two are on different filesystems
// it copies the file instead (see copyReplace). A non-root agent that is not
// allowed to write the destination falls back to "sudo -n mv".
func Rename(oldPath, newPath string) error {
	err := rename(oldPath, newPath)
	if stderrors.Is(err, syscall.EXDEV) {
		err = copyReplace(oldPath, newPath)
	}
	if stderrors.Is(err, fs.ErrPermission) && geteuid() != 0 {
		return MoveExecWithSudo(oldPath, newPath)
	}

	return err
}

// copyOwner gives the copy made by copyReplace the owner and group of the
// original, which a copy does not carry over by itself.
func copyOwner(srcInfo os.FileInfo, dst *os.File) error {
	srcStat, ok := srcInfo.Sys().(*syscall.Stat_t)
	if !ok {
		return nil
	}
	dstInfo, err := dst.Stat()
	if err != nil {
		return err
	}
	if dstStat, ok := dstInfo.Sys().(*syscall.Stat_t); ok && dstStat.Uid == srcStat.Uid && dstStat.Gid == srcStat.Gid {
		return nil
	}

	err = dst.Chown(int(srcStat.Uid), int(srcStat.Gid))
	if stderrors.Is(err, fs.ErrPermission) && geteuid() != 0 {
		return ChangeOwnerExecWithSudo(dst.Name(), strconv.FormatUint(uint64(srcStat.Uid), 10), strconv.FormatUint(uint64(srcStat.Gid), 10))
	}

	return err
}

func MoveExecWithSudo(sourcePath, targetPath string) error {
	mvFullPath, err := exec.LookPath("mv")
	if err != nil {
		return err
	}

	args := []string{
		"sudo",
		"-n",
		mvFullPath,
		sourcePath,
		targetPath,
	}

	cmd := exec.Command(args[0], args[1:]...) //nolint:gosec

	output, err := cmd.CombinedOutput()

	if err != nil {
		return errors.Wrapf(err, "failed to execute %s: %s", cmd.String(), string(output))
	}

	return nil
}
