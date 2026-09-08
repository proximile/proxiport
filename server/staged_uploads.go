package chserver

import "sync"

// stagedUploadRegistry records which staged file each agent has been told to
// fetch, so the SFTP endpoint can serve that file and nothing else.
//
// The agent transport carries an SSH server, and an agent opening a "session"
// channel used to get a pkg/sftp server rooted at the filesystem: every path it
// asked for was passed straight to os.Open as the daemon's own user. One agent
// credential therefore read proxiportd.conf -- key_seed and jwt_secret -- and
// every database under the data directory.
//
// Scoping to the staging directory alone would still let an agent read a file
// staged for a different client if it guessed the name. Recording the path per
// client removes the guess: an agent can read exactly the file the server is
// asking it to collect, while it is being asked.
type stagedUploadRegistry struct {
	mu      sync.RWMutex
	byOwner map[string]map[string]int
}

func newStagedUploadRegistry() *stagedUploadRegistry {
	return &stagedUploadRegistry{byOwner: map[string]map[string]int{}}
}

// Allow grants clientID permission to read path, and returns the function that
// takes it away again. The count handles the same file being pushed to the same
// client twice at once.
func (r *stagedUploadRegistry) Allow(clientID, path string) (release func()) {
	if r == nil || clientID == "" || path == "" {
		return func() {}
	}

	r.mu.Lock()
	paths, ok := r.byOwner[clientID]
	if !ok {
		paths = map[string]int{}
		r.byOwner[clientID] = paths
	}
	paths[path]++
	r.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			r.mu.Lock()
			defer r.mu.Unlock()
			if paths, ok := r.byOwner[clientID]; ok {
				paths[path]--
				if paths[path] <= 0 {
					delete(paths, path)
				}
				if len(paths) == 0 {
					delete(r.byOwner, clientID)
				}
			}
		})
	}
}

// IsAllowed reports whether clientID is currently being asked to collect path.
func (r *stagedUploadRegistry) IsAllowed(clientID, path string) bool {
	if r == nil {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	paths, ok := r.byOwner[clientID]
	if !ok {
		return false
	}
	_, ok = paths[path]
	return ok
}
