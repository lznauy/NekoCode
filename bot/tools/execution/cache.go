package execution

import (
	"os"
	"sync"
)

const maxCacheEntries = 100

// lineRange is a 1-based, inclusive interval of lines.
type lineRange struct{ Start, End int }

// fileState holds the full file content and the set of line ranges already returned.
type fileState struct {
	Lines  []string
	Mtime  int64
	Size   int64
	Ranges []lineRange
}

// FileStateCache caches file content and tracks which line ranges have been read.
type FileStateCache struct {
	mu      sync.RWMutex
	entries map[string]*fileState
	order   []string
}

func NewFileStateCache() *FileStateCache {
	return &FileStateCache{entries: make(map[string]*fileState)}
}

// Lines returns the cached full-file content if the file hasn't changed.
func (c *FileStateCache) Lines(path string) ([]string, bool) {
	key := normalizePath(path)

	c.mu.RLock()
	e, ok := c.entries[key]
	if !ok || e.Lines == nil {
		c.mu.RUnlock()
		return nil, false
	}
	mtime, size := e.Mtime, e.Size
	lines := e.Lines
	c.mu.RUnlock()

	info, err := os.Stat(path)
	if err != nil || info.ModTime().UnixNano() != mtime || info.Size() != size {
		c.mu.Lock()
		if err == nil {
			newMtime := info.ModTime().UnixNano()
			newSize := info.Size()
			if cur, exists := c.entries[key]; exists && (cur.Mtime != newMtime || cur.Size != newSize) {
				c.remove(key)
			}
		} else {
			c.remove(key)
		}
		c.mu.Unlock()
		return nil, false
	}
	return lines, true
}

// Put stores the full file content and marks [startLine, endLine] as read.
func (c *FileStateCache) Put(path string, lines []string, startLine, endLine int) {
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	mtime := info.ModTime().UnixNano()
	size := info.Size()

	c.mu.Lock()
	defer c.mu.Unlock()

	key := normalizePath(path)
	e, ok := c.entries[key]
	if !ok || e.Mtime != mtime || e.Size != size {
		e = &fileState{
			Lines: lines,
			Mtime: mtime,
			Size:  size,
		}
		c.entries[key] = e
		c.order = append(c.order, key)
	}
	e.Ranges = mergeRanges(e.Ranges, lineRange{startLine, endLine})
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
