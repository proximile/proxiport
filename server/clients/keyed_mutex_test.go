package clients

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestKeyedMutexSerializesSameKey verifies that concurrent Lock calls for the
// same key are mutually exclusive: many goroutines each append to a shared,
// unsynchronized slice while holding the lock for one key. If the lock did not
// serialize them the appends would race (flagged by -race) and lose updates;
// with correct serialization the slice ends with exactly n elements.
func TestKeyedMutexSerializesSameKey(t *testing.T) {
	var km KeyedMutex
	const n = 500
	var shared []int

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			unlock := km.Lock("same-key")
			defer unlock()
			shared = append(shared, i)
		}(i)
	}
	wg.Wait()

	assert.Len(t, shared, n, "concurrent same-key sections were not serialized")
}

// TestKeyedMutexDifferentKeysAreIndependent verifies that holding one key does
// not block another: a goroutine holds key "a" while the main goroutine locks
// and unlocks key "b" and signals completion.
func TestKeyedMutexDifferentKeysAreIndependent(t *testing.T) {
	var km KeyedMutex

	held := make(chan struct{})
	release := make(chan struct{})
	go func() {
		unlock := km.Lock("a")
		close(held)
		<-release
		unlock()
	}()

	<-held // key "a" is now held and stays held
	// Locking a different key must not block on "a".
	unlockB := km.Lock("b")
	unlockB()
	close(release)
}
