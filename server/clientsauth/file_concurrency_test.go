package clientsauth

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func readAuthFile(t *testing.T, name string) map[string]string {
	t.Helper()
	b, err := os.ReadFile(name) //nolint:gosec // G304: a file under t.TempDir()
	require.NoError(t, err)
	var pairs map[string]string
	require.NoError(t, json.Unmarshal(b, &pairs), "the auth file is not valid JSON: %q", string(b))
	return pairs
}

// Agents that log in at the same time each rewrite the auth file when their
// stored credential is upgraded to a hash, while an administrator may be
// removing a credential through the API. The file must stay valid, no lookup
// may fail, and a removed credential must stay removed, in the file and in
// what Get returns.
func TestFileProviderConcurrentWritersKeepTheFileValid(t *testing.T) {
	const agents = 40
	for round := 0; round < 10; round++ {
		initial := []*ClientAuth{{ID: "revoked", Password: "revoked-password"}}
		for i := 0; i < agents; i++ {
			// Different lengths, so that overlapping in-place writes would
			// leave the tail of a longer document behind a shorter one.
			initial = append(initial, &ClientAuth{ID: fmt.Sprintf("agent-%02d", i), Password: fmt.Sprintf("plain-%0*d", i%7+1, i)})
		}
		p := NewMockFileProvider(initial, t)

		var wg sync.WaitGroup
		errs := make(chan error, 3*agents+1)
		start := make(chan struct{})
		for i := 0; i < agents; i++ {
			id := fmt.Sprintf("agent-%02d", i)
			wg.Add(2)
			go func() {
				defer wg.Done()
				<-start
				if err := p.Update(&ClientAuth{ID: id, Password: "$2a$10$" + id + "-a-stand-in-for-a-hash-of-some-length"}); err != nil {
					errs <- fmt.Errorf("update %s: %w", id, err)
				}
			}()
			go func() {
				defer wg.Done()
				<-start
				got, err := p.Get(id)
				if err != nil {
					errs <- fmt.Errorf("get %s: %w", id, err)
				} else if got == nil {
					errs <- fmt.Errorf("get %s: not found", id)
				}
				// A lookup of the credential being removed, racing the removal.
				if _, err := p.Get("revoked"); err != nil {
					errs <- fmt.Errorf("get revoked: %w", err)
				}
			}()
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if err := p.Delete("revoked"); err != nil {
				errs <- fmt.Errorf("delete: %w", err)
			}
		}()
		close(start)
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Errorf("round %d: %v", round, err)
		}

		pairs := readAuthFile(t, p.fileName)
		assert.Len(t, pairs, agents, "round %d", round)
		_, stillThere := pairs["revoked"]
		assert.False(t, stillThere, "round %d: the removed credential is back in the file", round)
		for i := 0; i < agents; i++ {
			id := fmt.Sprintf("agent-%02d", i)
			assert.Contains(t, pairs[id], "$2a$10$", "round %d: the update of %s was lost", round, id)
		}

		got, err := p.Get("revoked")
		require.NoError(t, err)
		assert.Nil(t, got, "round %d: Get still returns the removed credential", round)
		if t.Failed() {
			return
		}
	}
}

// A save replaces the file in one step and leaves nothing else behind.
func TestFileProviderSaveLeavesNoTemporaryFiles(t *testing.T) {
	p := NewMockFileProvider([]*ClientAuth{{ID: "agent-1", Password: "password-1"}}, t)
	added, err := p.Add(&ClientAuth{ID: "agent-2", Password: "password-2"})
	require.NoError(t, err)
	require.True(t, added)
	require.NoError(t, p.Update(&ClientAuth{ID: "agent-1", Password: "password-1b"}))
	require.NoError(t, p.Delete("agent-2"))

	entries, err := os.ReadDir(filepath.Dir(p.fileName))
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, filepath.Base(p.fileName), entries[0].Name())
	assert.Equal(t, map[string]string{"agent-1": "password-1b"}, readAuthFile(t, p.fileName))
}

// The file keeps the mode its administrator gave it.
func TestFileProviderSaveKeepsTheFileMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file modes are not meaningful on Windows")
	}
	p := NewMockFileProvider([]*ClientAuth{{ID: "agent-1", Password: "password-1"}}, t)
	require.NoError(t, os.Chmod(p.fileName, 0o640)) //nolint:gosec // G302: a group-readable mode is what the test is about
	require.NoError(t, p.Update(&ClientAuth{ID: "agent-1", Password: "password-1b"}))

	info, err := os.Stat(p.fileName)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o640), info.Mode().Perm())
}

// When the configured path is a symbolic link, the link stays and the file
// it points to is the one that changes.
func TestFileProviderSaveFollowsASymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symbolic links needs a privilege on Windows")
	}
	p := NewMockFileProvider([]*ClientAuth{{ID: "agent-1", Password: "password-1"}}, t)
	target := p.fileName
	link := filepath.Join(t.TempDir(), "auth-link.json")
	require.NoError(t, os.Symlink(target, link))
	p.fileName = link

	require.NoError(t, p.Update(&ClientAuth{ID: "agent-1", Password: "password-1b"}))

	info, err := os.Lstat(link)
	require.NoError(t, err)
	assert.NotZero(t, info.Mode()&os.ModeSymlink, "the link was replaced by a regular file")
	assert.Equal(t, map[string]string{"agent-1": "password-1b"}, readAuthFile(t, target))
}

// A file that cannot be replaced, because its directory is not writable by
// the server or because it is a single file mounted into a container, is
// still updated: the provider falls back to writing it in place.
func TestFileProviderSaveFallsBackWhenTheDirectoryIsNotWritable(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a directory the test user cannot write to")
	}
	p := NewMockFileProvider([]*ClientAuth{{ID: "agent-1", Password: "a-rather-long-first-password"}}, t)
	dir := filepath.Dir(p.fileName)
	require.NoError(t, os.Chmod(dir, 0o500))       //nolint:gosec // G302: a directory needs its search bit
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) }) //nolint:gosec // G302: restored so the test directory can be removed

	require.NoError(t, p.Update(&ClientAuth{ID: "agent-1", Password: "short"}))
	assert.Equal(t, map[string]string{"agent-1": "short"}, readAuthFile(t, p.fileName))

	added, err := p.Add(&ClientAuth{ID: "agent-2", Password: "password-2"})
	require.NoError(t, err)
	require.True(t, added)
	assert.Equal(t, map[string]string{"agent-1": "short", "agent-2": "password-2"}, readAuthFile(t, p.fileName))
}
