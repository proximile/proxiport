package chserver

import (
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"

	"github.com/pkg/sftp"

	"github.com/proximile/proxiport/share/logger"
)

// errNotEntitled is what an agent gets for any path it was not asked to fetch.
// It carries no detail: an agent must not be able to probe the control plane's
// filesystem for what exists by reading the error text.
var errNotEntitled = errors.New("permission denied")

// stagedUploadFS is the filesystem an agent sees over SFTP: the single file the
// server is currently asking it to collect, and nothing else.
//
// Previously this was sftp.NewServer(stream, sftp.ReadOnly()), which is backed
// by the real filesystem with no root. pkg/sftp's toLocalPath only joins its
// working directory onto RELATIVE paths, so even sftp.WithServerWorkingDirectory
// would not have contained an absolute one -- the whole filesystem was readable
// as the daemon's user, which reaches proxiportd.conf (key_seed, jwt_secret)
// and every database.
//
// Reads are the only operation. Directory listing is refused as well, because
// every client's staged uploads share one directory and a listing would name
// other clients' files even if it could not open them.
type stagedUploadFS struct {
	clientID string
	registry *stagedUploadRegistry
	log      *logger.Logger
}

// resolve maps an SFTP path to a local one, refusing anything this agent has
// not been asked to collect.
func (fs *stagedUploadFS) resolve(requested string) (string, error) {
	if requested == "" {
		return "", errNotEntitled
	}

	// SFTP paths are slash-separated regardless of the server's OS.
	local := filepath.FromSlash(path.Clean(requested))
	if !filepath.IsAbs(local) {
		return "", errNotEntitled
	}

	if !fs.registry.IsAllowed(fs.clientID, local) {
		fs.log.Infof("agent %q asked for %q over sftp, which it was not asked to collect", fs.clientID, requested)
		return "", errNotEntitled
	}
	return local, nil
}

func (fs *stagedUploadFS) Fileread(r *sftp.Request) (io.ReaderAt, error) {
	local, err := fs.resolve(r.Filepath)
	if err != nil {
		return nil, err
	}

	file, err := os.Open(local) //nolint:gosec // path comes from the registry, not from the request
	if err != nil {
		return nil, errNotEntitled
	}

	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, errNotEntitled
	}
	return file, nil
}

// Filewrite refuses everything: the agent pulls a staged file, it never pushes.
func (fs *stagedUploadFS) Filewrite(*sftp.Request) (io.WriterAt, error) {
	return nil, errNotEntitled
}

// Filecmd refuses everything: rename, remove, mkdir, symlink, setstat.
func (fs *stagedUploadFS) Filecmd(*sftp.Request) error {
	return errNotEntitled
}

// Filelist answers Stat for an entitled file, so the client can size it, and
// refuses List so one agent cannot enumerate another's staged uploads.
func (fs *stagedUploadFS) Filelist(r *sftp.Request) (sftp.ListerAt, error) {
	if r.Method != "Stat" && r.Method != "Lstat" {
		return nil, errNotEntitled
	}

	local, err := fs.resolve(r.Filepath)
	if err != nil {
		return nil, err
	}

	info, err := os.Stat(local)
	if err != nil || !info.Mode().IsRegular() {
		return nil, errNotEntitled
	}
	return singleFileLister{info}, nil
}

type singleFileLister struct {
	info os.FileInfo
}

func (l singleFileLister) ListAt(entries []os.FileInfo, offset int64) (int, error) {
	if offset >= 1 {
		return 0, io.EOF
	}
	if len(entries) == 0 {
		return 0, nil
	}
	entries[0] = l.info
	return 1, io.EOF
}
