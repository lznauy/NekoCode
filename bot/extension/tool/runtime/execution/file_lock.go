package execution

import (
	"context"
	"sync"
)

type fileLock struct {
	gate  chan struct{}
	users int
}

var fileLocks = struct {
	sync.Mutex
	entries map[string]*fileLock
}{entries: make(map[string]*fileLock)}

// LockFile serializes cooperating read/write/edit operations on a resolved
// target. Waiters are cancellable and unused entries are removed immediately.
func LockFile(ctx context.Context, path string) (func(), error) {
	key := normalizePath(path)
	fileLocks.Lock()
	entry := fileLocks.entries[key]
	if entry == nil {
		entry = &fileLock{gate: make(chan struct{}, 1)}
		entry.gate <- struct{}{}
		fileLocks.entries[key] = entry
	}
	entry.users++
	fileLocks.Unlock()
	releaseReference := func() {
		fileLocks.Lock()
		entry.users--
		if entry.users == 0 {
			delete(fileLocks.entries, key)
		}
		fileLocks.Unlock()
	}
	select {
	case <-ctx.Done():
		releaseReference()
		return nil, ctx.Err()
	case <-entry.gate:
		if err := ctx.Err(); err != nil {
			entry.gate <- struct{}{}
			releaseReference()
			return nil, err
		}
		var once sync.Once
		return func() { once.Do(func() { entry.gate <- struct{}{}; releaseReference() }) }, nil
	}
}
