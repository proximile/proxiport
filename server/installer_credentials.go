package chserver

import (
	"crypto/rand"
	"os"
	"path/filepath"
)

// installerCredentialFiles are the cleartext credential files the packaged
// installer writes into the data dir for the operator to read once at first
// login. They are obsolete the moment an admin successfully authenticates with
// a password, so the server shreds them then (see shredConsumedInstallerCreds).
var installerCredentialFiles = []string{
	"initial-admin-password",
	"initial-client-auth",
}

// shredConsumedInstallerCreds best-effort overwrites and removes the installer's
// cleartext credential files from the data dir. It is called after a successful
// password login: at that point the operator has retrieved the credentials, so
// leaving them on disk only widens the stolen-disk exposure. It is idempotent
// and cheap — once the files are gone it does nothing — and never blocks or
// fails a login (all errors are logged at debug and swallowed).
func (al *APIListener) shredConsumedInstallerCreds() {
	dataDir := al.config.Server.DataDir
	if dataDir == "" {
		return
	}
	for _, name := range installerCredentialFiles {
		path := filepath.Join(dataDir, name)
		info, err := os.Stat(path)
		if err != nil {
			continue // not present (already shredded, or never written)
		}
		if err := shredFile(path, info.Size()); err != nil {
			al.Debugf("could not shred installer credential file %q: %v", path, err)
			continue
		}
		al.Infof("shredded consumed installer credential file %q after admin login", path)
	}
}

// shredFile overwrites a file's contents with random bytes, flushes, and removes
// it. On journalling/copy-on-write filesystems the overwrite is not a guaranteed
// secure erase, but it removes the plaintext from the live file and is a strict
// improvement over an unlink alone.
func shredFile(path string, size int64) error {
	if size > 0 {
		f, err := os.OpenFile(path, os.O_WRONLY, 0) //nolint:gosec // path is data_dir + a fixed installer-file name

		if err != nil {
			return err
		}
		buf := make([]byte, size)
		if _, err := rand.Read(buf); err == nil {
			_, _ = f.WriteAt(buf, 0)
			_ = f.Sync()
		}
		_ = f.Close()
	}
	return os.Remove(path)
}
