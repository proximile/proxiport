package clientsauth

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/patrickmn/go-cache"

	"github.com/proximile/proxiport/share/enums"
	"github.com/proximile/proxiport/share/query"
)

// FileProvider is the file-based client auth provider.
//
// Every operation holds mu from start to end. Agents authenticate
// concurrently, each successful login may rewrite the file (the upgrade of a
// stored credential to a hash), and the API adds and removes credentials at
// the same time. Without the lock two writers interleave their
// read-change-write steps: one overwrites what the other just wrote, which
// brings a removed credential back, or both write the file at once and leave
// it unreadable. The cache is read and written under the same lock, so a
// lookup that read the file before a removal cannot cache the credential
// after it.
type FileProvider struct {
	mu       sync.Mutex
	fileName string
	cache    *cache.Cache
}

var _ Provider = &FileProvider{}

func NewFileProvider(fileName string, cache *cache.Cache) *FileProvider {
	return &FileProvider{
		fileName: fileName,
		cache:    cache,
	}
}

// getAll returns proxiport clients auth credentials from a given file.
func (c *FileProvider) getAll() ([]*ClientAuth, error) {
	idPswdPairs, err := c.load()
	if err != nil {
		return nil, fmt.Errorf("failed to decode proxiport clients auth file: %v", err)
	}

	var res []*ClientAuth
	for id, pswd := range idPswdPairs {
		if id == "" || pswd == "" {
			return nil, errors.New("empty client auth ID or password is not allowed")
		}
		res = append(res, &ClientAuth{ID: id, Password: pswd})
	}

	return res, nil
}

func (c *FileProvider) GetFiltered(filter *query.ListOptions) ([]*ClientAuth, int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	var ca []*ClientAuth
	ca, err := c.getAll()
	if err != nil {
		return nil, 0, err
	}
	if len(filter.Filters) > 0 {
		var filtered = []*ClientAuth{}
		for _, v := range ca {
			match, err := query.MatchesFilters(v, filter.Filters)
			if err != nil {
				return nil, 0, err
			}
			if match {
				filtered = append(filtered, &ClientAuth{v.ID, v.Password})
			}
		}
		ca = filtered
	}
	c.SortByID(ca, false)
	l := len(ca)
	start, end := filter.Pagination.GetStartEnd(l)
	return ca[start:end], l, nil
}

// Get returns a copy. The cached entry lives for an hour, so a caller that
// wrote to the object it received would corrupt every agent authentication for
// that id until the entry expired.
func (c *FileProvider) Get(id string) (*ClientAuth, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if val, _ := c.cache.Get(c.CacheKey(id)); val != nil {
		credential := *val.(*ClientAuth)
		return &credential, nil
	}
	idPswdPairs, err := c.load()
	if err != nil {
		return nil, fmt.Errorf("failed to decode proxiport clients auth file: %v", err)
	}
	if _, ok := idPswdPairs[id]; ok {
		ca := &ClientAuth{ID: id, Password: idPswdPairs[id]}
		c.cache.Set(c.CacheKey(id), ca, 60*time.Minute)
		credential := *ca
		return &credential, nil
	}
	return nil, nil
}

func (c *FileProvider) Add(clientAuth *ClientAuth) (bool, error) {
	if err := validateStorableCredential(clientAuth); err != nil {
		return false, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	idPswdPairs, err := c.load()
	if err != nil {
		return false, fmt.Errorf("failed to decode proxiport clients auth file: %v", err)
	}

	clientID := clientAuth.ID

	if _, ok := idPswdPairs[clientID]; ok {
		return false, nil
	}

	idPswdPairs[clientID] = clientAuth.Password

	if err := c.save(idPswdPairs); err != nil {
		return false, fmt.Errorf("failed to encode proxiport clients auth file: %v", err)
	}

	return true, nil
}

func (c *FileProvider) Update(clientAuth *ClientAuth) error {
	if err := validateStorableCredential(clientAuth); err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	idPswdPairs, err := c.load()
	if err != nil {
		return fmt.Errorf("failed to decode proxiport clients auth file: %v", err)
	}

	if _, ok := idPswdPairs[clientAuth.ID]; !ok {
		return fmt.Errorf("client auth with ID %q not found", clientAuth.ID)
	}

	idPswdPairs[clientAuth.ID] = clientAuth.Password
	c.cache.Delete(c.CacheKey(clientAuth.ID))

	if err := c.save(idPswdPairs); err != nil {
		return fmt.Errorf("failed to encode proxiport clients auth file: %v", err)
	}

	return nil
}

func (c *FileProvider) Delete(id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	idPswdPairs, err := c.load()
	if err != nil {
		return fmt.Errorf("failed to decode proxiport clients auth file: %v", err)
	}

	delete(idPswdPairs, id)
	c.cache.Delete(c.CacheKey(id))

	if err := c.save(idPswdPairs); err != nil {
		return fmt.Errorf("failed to encode proxiport clients auth file: %v", err)
	}

	return nil
}

func (c *FileProvider) IsWriteable() bool {
	return true
}

func (c *FileProvider) load() (map[string]string, error) {
	b, err := os.ReadFile(c.fileName)
	if err != nil {
		return nil, fmt.Errorf("failed to read proxiport clients auth file %q: %s", c.fileName, err)
	}

	var idPswdPairs map[string]string
	if err := json.Unmarshal(b, &idPswdPairs); err != nil {
		return nil, err
	}

	return idPswdPairs, nil
}

// save writes the credentials to the auth file. The caller holds mu.
//
// The new content goes into a temporary file beside the auth file, which
// then takes its place in one step, so a reader never sees a half-written
// file and a crash leaves either the old content or the new. Where the file
// cannot be replaced that way it is rewritten in place: see saveInPlace.
func (c *FileProvider) save(idPswdPairs map[string]string) error {
	content, err := json.MarshalIndent(idPswdPairs, "", "	")
	if err != nil {
		return fmt.Errorf("failed to write proxiport clients auth: %v", err)
	}
	content = append(content, '\n')

	// Follow a symbolic link, so that the link stays and its target changes.
	target, err := filepath.EvalSymlinks(c.fileName)
	if err != nil {
		return fmt.Errorf("failed to open proxiport clients auth file: %v", err)
	}
	// The file must exist: a save never creates it, as before.
	info, err := os.Stat(target)
	if err != nil {
		return fmt.Errorf("failed to open proxiport clients auth file: %v", err)
	}

	replaced, err := replaceFile(target, info, content)
	if err != nil {
		return fmt.Errorf("failed to write proxiport clients auth: %v", err)
	}
	if replaced {
		return nil
	}
	if err := saveInPlace(target, content); err != nil {
		return fmt.Errorf("failed to write proxiport clients auth: %v", err)
	}
	return nil
}

// replaceFile writes content to a temporary file in target's directory and
// renames it over target. It reports false, with no error and target
// untouched, when target cannot be replaced like that:
//
//   - the directory is not writable by the server (a credentials file the
//     server owns inside a directory it does not);
//   - the temporary file cannot be given target's owner and group, so
//     replacing would change who can read the credentials;
//   - the rename is refused, as it is for a single file mounted into a
//     container.
//
// An error means the new content could not be written out in full, for
// example on a full disk. target is untouched then too.
func replaceFile(target string, info os.FileInfo, content []byte) (bool, error) {
	tmp, err := os.CreateTemp(filepath.Dir(target), "."+filepath.Base(target)+".tmp-*")
	if err != nil {
		return false, nil //nolint:nilerr // not replaceable here: the caller writes in place
	}
	tmpName := tmp.Name()
	keep := false
	defer func() {
		if !keep {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return false, err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return false, err
	}
	// A failed close can mean the credentials did not reach the disk.
	if err := tmp.Close(); err != nil {
		return false, err
	}

	// CreateTemp makes the file 0600 and owned by the server. Give it the
	// mode and ownership of the file it replaces.
	if err := os.Chmod(tmpName, info.Mode().Perm()); err != nil {
		return false, nil //nolint:nilerr // not replaceable here: the caller writes in place
	}
	if !matchOwner(tmpName, info) {
		return false, nil
	}

	if err := os.Rename(tmpName, target); err != nil {
		return false, nil //nolint:nilerr // not replaceable here: the caller writes in place
	}
	keep = true

	// Make the rename itself durable. Not every platform can sync a
	// directory, and the content is already on disk, so a failure here is
	// not reported.
	if dir, err := os.Open(filepath.Dir(target)); err == nil { //nolint:gosec // G304: the directory of the configured auth file
		_ = dir.Sync()
		_ = dir.Close()
	}
	return true, nil
}

// saveInPlace overwrites the file where it is. Readers in this process are
// held off by the provider's lock. The content is written over the old one
// before the file is cut to its new length, so the file is never empty on
// the way; a crash in the middle can still leave a mix of old and new.
func saveInPlace(target string, content []byte) error {
	file, err := os.OpenFile(target, os.O_WRONLY, 0o600) //nolint:gosec // G304: the configured auth file
	if err != nil {
		return err
	}
	if _, err := file.WriteAt(content, 0); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Truncate(int64(len(content))); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	// A failed close can mean the credentials did not reach the disk.
	return file.Close()
}

func (c *FileProvider) Source() enums.ProviderSource {
	return enums.ProviderSourceFile
}

func (c *FileProvider) SortByID(a []*ClientAuth, desc bool) {
	sort.Slice(a, func(i, j int) bool {
		less := a[i].ID < a[j].ID
		if desc {
			return !less
		}
		return less
	})
}

func (c *FileProvider) CacheKey(id string) string {
	return "client-auth-" + id
}
