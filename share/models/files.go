package models

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/proximile/proxiport/share/logger"

	errors2 "github.com/pkg/errors"
)

const (
	uploadedFileDestinationPathKey = "dest"
	uploadedFileOwnerKey           = "user"
	uploadedFileOwnerGroupKey      = "group"
	uploadedFileModeKey            = "mode"
	fileWriteForcedKey             = "force"
	fileSyncdKey                   = "sync"
	IDKey                          = "id"
)

type UploadedFile struct {
	ID                   string
	SourceFilePath       string
	DestinationPath      string
	DestinationFileMode  os.FileMode
	DestinationFileOwner string
	DestinationFileGroup string
	ForceWrite           bool
	Sync                 bool
	Md5Checksum          []byte
}

// MaxPushedFileMode is the highest mode a pushed file may be given: permission
// bits only.
//
// The mode travels from the API caller, through the server, to the agent, which
// applies it -- usually as root. Anything above 0777 is the setuid, setgid or
// sticky bit, and a setuid binary written by a file push is a root shell on the
// managed host for whoever can run it. That is a larger grant than pushing a
// file, and it must not be reachable either by an API user who holds only the
// uploads permission or by a server the operator does not fully trust.
const MaxPushedFileMode = os.FileMode(0o777)

// Validate is the chokepoint both directions pass through: the server calls it
// on the incoming request, and the agent calls it again on what arrives over
// the transport.
func (uf UploadedFile) Validate() error {
	if uf.SourceFilePath == "" {
		return errors.New("empty source file name")
	}

	if uf.DestinationPath == "" {
		return errors.New("empty destination file path")
	}

	if uf.DestinationFileMode&^MaxPushedFileMode != 0 {
		return fmt.Errorf(
			"file mode %#o is not allowed on a pushed file: only permission bits up to %#o may be set",
			uf.DestinationFileMode, MaxPushedFileMode)
	}

	return nil
}

// ValidateDestinationPath rejects a push whose destination matches one of the
// agent's protected patterns.
//
// A pattern is either a filepath.Match glob, tested against both the
// destination and its directory, or a subtree: a pattern ending in "/**"
// protects that directory and everything below it. The subtree form exists
// because filepath.Match does not cross a separator, so a glob can only ever
// name one level -- which is the wrong shape for something like a systemd unit
// directory, where the dangerous file may be several levels down.
//
// Matching is case-insensitive on Windows, where the filesystem is.
func (uf UploadedFile) ValidateDestinationPath(globPatters []string, log *logger.Logger) error {
	destination := filepath.Clean(uf.DestinationPath)
	destinationDir := filepath.Dir(destination)

	for _, p := range globPatters {
		if subtree, isSubtree := strings.CutSuffix(p, "/**"); isSubtree {
			if pathIsWithin(destination, filepath.Clean(subtree)) {
				return fmt.Errorf("target path %s is inside protected directory %s, therefore the file push request is rejected", destination, subtree)
			}
			continue
		}

		matchedDir, err := filepath.Match(p, destinationDir)
		if err != nil {
			log.Errorf("failed to match glob pattern %s against destination directory %s: %v", p, uf.DestinationPath, err)
			continue
		}
		if matchedDir {
			return fmt.Errorf("target path %s matches protected pattern %s, therefore the file push request is rejected", destinationDir, p)
		}

		matchedFile, err := filepath.Match(p, destination)
		if err != nil {
			log.Errorf("failed to match glob pattern %s against file name %s: %v", p, destination, err)
			continue
		}

		if matchedFile {
			return fmt.Errorf("target path %s matches protected pattern %s, therefore the file push request is rejected", destination, p)
		}
	}

	return nil
}

func (uf *UploadedFile) FromMultipartRequest(req *http.Request) error {
	var err error
	if req.MultipartForm == nil {
		return nil
	}

	if len(req.MultipartForm.Value[uploadedFileDestinationPathKey]) > 0 {
		uf.DestinationPath = req.MultipartForm.Value[uploadedFileDestinationPathKey][0]
	}

	if len(req.MultipartForm.Value[uploadedFileOwnerKey]) > 0 {
		uf.DestinationFileOwner = req.MultipartForm.Value[uploadedFileOwnerKey][0]
	}

	if len(req.MultipartForm.Value[uploadedFileOwnerGroupKey]) > 0 {
		uf.DestinationFileGroup = req.MultipartForm.Value[uploadedFileOwnerGroupKey][0]
	}

	if len(req.MultipartForm.Value[uploadedFileModeKey]) > 0 {
		rawMode := req.MultipartForm.Value[uploadedFileModeKey][0]
		fileModeInt, err := strconv.ParseInt(rawMode, 8, 32)
		if err != nil {
			return errors2.Wrapf(err, "failed to parse file mode value %s", rawMode)
		}
		// A pushed file's mode is a permission, nothing more. Anything above
		// 0o777 is a setuid, setgid or sticky bit, and the agent applies the
		// mode as root -- a setuid binary written this way is a root shell for
		// whoever can run it, which is a bigger grant than the push itself.
		if fileModeInt < 0 || fileModeInt > 0o777 {
			return errors2.Errorf("file mode %s is out of range: only permission bits up to 0777 may be set on a pushed file", rawMode)
		}
		uf.DestinationFileMode = os.FileMode(uint32(fileModeInt))
	}

	if len(req.MultipartForm.Value[fileWriteForcedKey]) > 0 {
		uf.ForceWrite, err = strconv.ParseBool(req.MultipartForm.Value[fileWriteForcedKey][0])
		if err != nil {
			return err
		}
	}

	if len(req.MultipartForm.Value[fileSyncdKey]) > 0 {
		uf.Sync, err = strconv.ParseBool(req.MultipartForm.Value[fileSyncdKey][0])
		if err != nil {
			return err
		}
	}
	if len(req.MultipartForm.Value[IDKey]) > 0 {
		uf.ID = req.MultipartForm.Value[IDKey][0]
	}

	return nil
}

func (uf *UploadedFile) FromBytes(rawData []byte) error {
	return json.Unmarshal(rawData, uf)
}

func (uf *UploadedFile) ToBytes() (data []byte, err error) {
	return json.Marshal(uf)
}

type UploadResponse struct {
	UploadResponseShort
	Message string `json:"message"`
	Status  string `json:"status"`
}

type UploadResponseShort struct {
	ID        string `json:"uuid"`
	Filepath  string `json:"filepath"`
	SizeBytes int64  `json:"size"`
}

// pathIsWithin reports whether target is the root itself or sits underneath it.
//
// The root may contain glob metacharacters -- "/home/*/.ssh" has to cover every
// user -- so this walks target and its ancestors and filepath.Match-es each
// against the root. Testing whole ancestor paths is also what keeps
// "/etc/cron.daily-reports" from being treated as inside "/etc/cron.d".
func pathIsWithin(target, root string) bool {
	if runtime.GOOS == "windows" {
		target = strings.ToLower(target)
		root = strings.ToLower(root)
	}

	for current := target; ; {
		if matched, err := filepath.Match(root, current); err == nil && matched {
			return true
		}
		parent := filepath.Dir(current)
		if parent == current {
			return false
		}
		current = parent
	}
}
