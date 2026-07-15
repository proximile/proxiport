package clients

import "sync"

// KeyedMutex provides per-key mutual exclusion. Locking two different keys
// proceeds concurrently; locking the same key serializes. It is used to make a
// multi-step operation on a single client (e.g. the check-then-register of a
// connecting client, or the check-then-create of a tunnel) atomic against other
// requests for that same client without blocking requests for other clients.
//
// Entries are kept for the lifetime of the process. That is bounded by the
// number of distinct keys seen (the client fleet size), so it is not an
// unbounded leak, and it keeps Lock allocation-free on the steady-state path.
// The zero value is ready to use.
type KeyedMutex struct {
	locks sync.Map // key string -> *sync.Mutex
}

// Lock acquires the mutex for key and returns a function that releases it.
// Callers should defer the returned function.
func (k *KeyedMutex) Lock(key string) (unlock func()) {
	m, _ := k.locks.LoadOrStore(key, &sync.Mutex{})
	mu := m.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}
