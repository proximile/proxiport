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

	// Every protected-path rule on both sides -- the server's denied-prefix
	// list and the agent's FileReceptionGlobs -- is written as an absolute
	// path, and a relative destination matches none of them. It is not
	// harmless for having dodged the filters: the agent hands the name
	// straight to os.Rename, which resolves it against the agent process's
	// working directory, and the shipped systemd unit sets no
	// WorkingDirectory= -- so systemd's default of "/" makes
	// "etc/sudoers.d/x" land on /etc/sudoers.d/x with every filter bypassed.
	// Requiring an absolute path is what makes the lists mean what they say.
	if !IsAbsoluteDestination(uf.DestinationPath) {
		return fmt.Errorf(
			"destination path %q must be absolute: a relative path is resolved "+
				"against the agent's working directory and is matched by no "+
				"protected-path rule", uf.DestinationPath)
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
	// Every pattern below is absolute, so a relative destination matches none
	// of them and would sail through. Validate() rejects that earlier on both
	// the server and the agent; this is the last-resort filter against a
	// hostile server, so it does not rely on an earlier caller having run.
	if !IsAbsoluteDestination(uf.DestinationPath) {
		return fmt.Errorf(
			"target path %s is not absolute, therefore the file push request is rejected",
			uf.DestinationPath)
	}

	// Every spelling that reaches the same file has to be checked, not just
	// the one the caller typed. filepath.Clean is purely lexical, so before
	// this a protected path was reachable through any alias of itself: on
	// macOS /etc, /var and /tmp are symlinks into /private, so
	// "/private/etc/sudoers.d/x" matched nothing and landed in the real
	// sudoers.d; on Windows the same held for the \\?\ prefix and 8.3 short
	// names. The alias can sit on either side -- the operator may list the
	// canonical path while the push uses the symlink, or the reverse -- so
	// both the destination and each pattern are resolved and every
	// combination is tested.
	given := stripExtendedLengthPrefix(uf.DestinationPath)
	destinations := distinctPaths(
		filepath.Clean(uf.DestinationPath),
		filepath.Clean(given),
		resolveForMatch(given),
	)

	for _, p := range globPatters {
		if subtree, isSubtree := strings.CutSuffix(p, "/**"); isSubtree {
			for _, root := range distinctPaths(filepath.Clean(subtree), canonicalPattern(subtree)) {
				for _, destination := range destinations {
					if pathIsWithin(destination, root) {
						return fmt.Errorf("target path %s is inside protected directory %s, therefore the file push request is rejected", destination, subtree)
					}
				}
			}
			continue
		}

		for _, pattern := range distinctPaths(p, canonicalPattern(p)) {
			for _, destination := range destinations {
				destinationDir := filepath.Dir(destination)

				matchedDir, err := filepath.Match(pattern, destinationDir)
				if err != nil {
					log.Errorf("failed to match glob pattern %s against destination directory %s: %v", pattern, uf.DestinationPath, err)
					continue
				}
				if matchedDir {
					return fmt.Errorf("target path %s matches protected pattern %s, therefore the file push request is rejected", destinationDir, p)
				}

				matchedFile, err := filepath.Match(pattern, destination)
				if err != nil {
					log.Errorf("failed to match glob pattern %s against file name %s: %v", pattern, destination, err)
					continue
				}
				if matchedFile {
					return fmt.Errorf("target path %s matches protected pattern %s, therefore the file push request is rejected", destination, p)
				}
			}
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

// IsAbsoluteDestination reports whether p is absolute on POSIX or on Windows.
//
// filepath.IsAbs answers only for the platform it is compiled for, which is the
// wrong question on the server: it runs on Linux and validates destinations for
// Windows agents, where "C:\\dir\\file" is absolute and filepath.IsAbs would
// say otherwise. Both shapes are accepted here and the agent's own
// ValidateDestinationPath still applies the platform rules.
//
// A Windows path that is rooted but driveless ("\\Windows\\x") is deliberately
// NOT absolute: it resolves against the current drive, which is the same class
// of surprise as a relative path.
func IsAbsoluteDestination(p string) bool {
	if strings.HasPrefix(p, "/") {
		return true
	}
	// UNC: \\server\share\path
	if strings.HasPrefix(p, `\\`) {
		return true
	}
	// Drive-absolute: C:\path or C:/path
	if len(p) >= 3 && p[1] == ':' && (p[2] == '\\' || p[2] == '/') {
		c := p[0]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
			return true
		}
	}
	return false
}

// pathIsWithin reports whether target is the root itself or sits underneath it.
//
// The root may contain glob metacharacters -- "/home/*/.ssh" has to cover every
// user -- so this walks target and its ancestors and filepath.Match-es each
// against the root. Testing whole ancestor paths is also what keeps
// "/etc/cron.daily-reports" from being treated as inside "/etc/cron.d".
// caseInsensitiveFS reports whether this platform's filesystem is
// case-insensitive by default. Only Windows was considered before, but APFS is
// case-insensitive in its default configuration too, so "/Etc/sudoers.d/x"
// reached the real file on macOS while matching the pattern nowhere. Treating
// darwin as case-insensitive can only refuse more, which is the safe direction
// for a filter of this kind.
// stripExtendedLengthPrefix removes Windows' extended-length path prefix.
//
// `\\?\C:\Windows\System32\x` names exactly the same file as
// `C:\Windows\System32\x`, and IsAbsoluteDestination accepts it because it
// begins with `\\`. filepath.Clean does not remove the prefix on any platform,
// so the pattern `C:\Windows/**` never matched it and every protected Windows
// directory was reachable by prefixing the path. The UNC spelling
// `\\?\UNC\server\share` maps back to `\\server\share`.
//
// This is plain string work with no filesystem access, so it behaves the same
// wherever it runs and is exercised by tests on any platform.
func stripExtendedLengthPrefix(p string) string {
	const (
		extended    = `\\?\`
		extendedUNC = `\\?\UNC\`
	)
	if strings.HasPrefix(p, extendedUNC) {
		return `\\` + p[len(extendedUNC):]
	}
	if strings.HasPrefix(p, extended) {
		return p[len(extended):]
	}
	return p
}

// distinctPaths returns its arguments with empty and duplicate entries removed,
// preserving order. Resolution usually yields the same path it was given, and
// matching it twice is only wasted work.
func distinctPaths(paths ...string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if p == "" {
			continue
		}
		seen := false
		for _, existing := range out {
			if existing == p {
				seen = true
				break
			}
		}
		if !seen {
			out = append(out, p)
		}
	}
	return out
}

// resolveForMatch returns p with symlinks resolved as far as the filesystem
// allows.
//
// filepath.EvalSymlinks fails outright on a path that does not exist, and the
// destination of a push usually does not exist yet -- that is the point of the
// push. So resolve the deepest ancestor that does exist and re-attach the
// unresolved remainder. A path where nothing resolves comes back cleaned but
// otherwise unchanged, so it is still compared lexically rather than skipped.
//
// This runs on the agent, against the agent's own filesystem: the server never
// calls ValidateDestinationPath (client/upload.go is the only caller), so there
// is no question of resolving one host's paths on another.
func resolveForMatch(p string) string {
	cleaned := filepath.Clean(p)

	rest := ""
	for current := cleaned; ; {
		if resolved, err := filepath.EvalSymlinks(current); err == nil {
			if rest == "" {
				return resolved
			}
			return filepath.Join(resolved, rest)
		}

		parent := filepath.Dir(current)
		if parent == current {
			return cleaned
		}
		rest = filepath.Join(filepath.Base(current), rest)
		current = parent
	}
}

// canonicalPattern resolves the leading, wildcard-free part of a protected
// pattern, leaving the glob elements alone. "/etc/sudoers.d" becomes
// "/private/etc/sudoers.d" on macOS, while "/home/*/.ssh" keeps its wildcard
// and only "/home" is resolved.
func canonicalPattern(pattern string) string {
	prefix, rest := literalPrefix(pattern)
	if prefix == "" {
		return pattern
	}

	resolved := resolveForMatch(prefix)
	if resolved == filepath.Clean(prefix) {
		return pattern
	}
	if rest == "" {
		return resolved
	}
	return resolved + string(filepath.Separator) + rest
}

// literalPrefix splits a pattern at its first element containing a glob
// metacharacter. A backslash is not treated as one: it is the separator on
// Windows, where the protected list is written with it.
func literalPrefix(pattern string) (prefix, rest string) {
	sep := string(filepath.Separator)
	elems := strings.Split(pattern, sep)
	for i, e := range elems {
		if strings.ContainsAny(e, "*?[") {
			return strings.Join(elems[:i], sep), strings.Join(elems[i:], sep)
		}
	}
	return pattern, ""
}

func caseInsensitiveFS() bool {
	return runtime.GOOS == "windows" || runtime.GOOS == "darwin"
}

func pathIsWithin(target, root string) bool {
	return pathIsWithinFold(target, root, caseInsensitiveFS())
}

// pathIsWithinFold takes the fold decision as an argument so the case-folding
// branch is reachable from a test on a case-sensitive platform.
func pathIsWithinFold(target, root string, fold bool) bool {
	if fold {
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
