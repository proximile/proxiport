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
		overwritten, err := shredFile(path, info.Size())
		if err != nil {
			// Error, not Debug. This is a documented guarantee -- docs/install.md
			// tells the operator these files are gone after the first password
			// login -- and it was failing silently on every packaged install:
			// the files were 0640 root:proxiport while the daemon ran as
			// proxiport, so the O_WRONLY open returned EACCES and the whole
			// function gave up before reaching the unlink. At the default
			// log_level the operator saw neither a success nor a failure.
			al.Errorf("could not remove installer credential file %q, so the cleartext credential is still on disk: %v", path, err)
			continue
		}
		if !overwritten {
			al.Infof("removed consumed installer credential file %q after admin login (could not overwrite it in place first)", path)
			continue
		}
		al.Infof("shredded consumed installer credential file %q after admin login", path)
	}
}

// shredFile overwrites a file's contents with random bytes, flushes, and
// removes it. On journalling/copy-on-write filesystems the overwrite is not a
// guaranteed secure erase, but it removes the plaintext from the live file and
// is a strict improvement over an unlink alone.
//
// It reports whether the overwrite happened, and removing the file is NOT
// conditional on it. Failing to open the file for writing used to abort the
// whole thing, which left the cleartext admin password on disk -- yet the
// unlink needs write permission on the DIRECTORY, not on the file, so it would
// have succeeded. An unlink alone is weaker than a shred and far stronger than
// leaving the file in place.
func shredFile(path string, size int64) (overwritten bool, err error) {
	if size > 0 {
		f, openErr := os.OpenFile(path, os.O_WRONLY, 0) //nolint:gosec // path is data_dir + a fixed installer-file name
		if openErr == nil {
			buf := make([]byte, size)
			if _, rerr := rand.Read(buf); rerr == nil {
				_, _ = f.WriteAt(buf, 0)
				_ = f.Sync()
			}
			_ = f.Close()
			overwritten = true
		}
	}
	return overwritten, os.Remove(path)
}
