package execution

import (
	"os"
	"sync"
)

const maxCacheEntries = 100

// fileState contains parsed text for one exact raw-byte version. Mtime is
// advisory ordering for parent/child cache merges, never a validity check.
type fileState struct {
	Version [32]byte
	Lines   []string
	Mtime   int64
}

// FileStateCache caches parsed file content; ReadEvidence owns read authority.
type FileStateCache struct {
	mu      sync.RWMutex
	entries map[string]*fileState
	order   []string
}

func NewFileStateCache() *FileStateCache {
	return &FileStateCache{entries: make(map[string]*fileState)}
}

// Lines returns parsed text only for the caller's current raw-byte version.
func (c *FileStateCache) Lines(path string, version [32]byte) ([]string, bool) {
	key := normalizePath(path)
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, ok := c.entries[key]
	if !ok || entry.Lines == nil || entry.Version != version {
		return nil, false
	}
	return entry.Lines, true
}

// Put stores parsed text for the exact version read by the caller.
func (c *FileStateCache) Put(path string, lines []string, version [32]byte) {
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	key := normalizePath(path)
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, exists := c.entries[key]
	if !exists {
		entry = &fileState{}
		c.entries[key] = entry
		c.order = append(c.order, key)
	}
	entry.Version, entry.Lines, entry.Mtime = version, lines, info.ModTime().UnixNano()
	c.evictIfNeeded()
}

func (c *FileStateCache) Invalidate(path string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.remove(normalizePath(path))
}

func (c *FileStateCache) remove(path string) {
	delete(c.entries, path)
	for i, p := range c.order {
		if p == path {
			c.order = append(c.order[:i], c.order[i+1:]...)
			return
		}
	}
}

func (c *FileStateCache) evictIfNeeded() {
	for len(c.entries) > maxCacheEntries && len(c.order) > 0 {
		delete(c.entries, c.order[0])
		c.order = c.order[1:]
	}
}
