package tools

import (
	"os"
	"path/filepath"
	"sort"
	"sync"
)

const maxCacheEntries = 100

// lineRange is a 1-based, inclusive interval of lines.
type lineRange struct{ Start, End int }

// fileState holds the full file content and the set of line ranges already returned.
type fileState struct {
	Lines  []string    // full file content, one string per line
	Mtime  int64
	Size   int64
	Ranges []lineRange // merged, non-overlapping, sorted by Start
}

// FileStateCache caches file content and tracks which line ranges have been read.
type FileStateCache struct {
	mu      sync.RWMutex
	entries map[string]*fileState
	order   []string // LRU, most recent at end
}

var GlobalFileCache *FileStateCache

func NewFileStateCache() *FileStateCache {
	return &FileStateCache{entries: make(map[string]*fileState)}
}

// Get returns a hint string if [startLine, endLine] is fully covered by the cache.
// Returns ("", false) when the range is not covered, the file changed, or not cached.
func (c *FileStateCache) Get(path string, startLine, endLine int) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	key := normalizePath(path)
	e, ok := c.entries[key]
	if !ok {
		return "", false
	}
	info, err := os.Stat(path)
	if err != nil || info.ModTime().UnixNano() != e.Mtime || info.Size() != e.Size {
		return "", false
	}
	if e.Lines == nil {
		return "", false
	}
	if endLine > len(e.Lines) {
		endLine = len(e.Lines)
	}
	for _, r := range e.Ranges {
		if r.Start <= startLine && endLine <= r.End {
			return "[content already read — see earlier Read output for this file]", true
		}
	}
	return "", false
}

// Lines returns the cached full-file content if the file hasn't changed.
func (c *FileStateCache) Lines(path string) ([]string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	key := normalizePath(path)
	e, ok := c.entries[key]
	if !ok || e.Lines == nil {
		return nil, false
	}
	info, err := os.Stat(path)
	if err != nil || info.ModTime().UnixNano() != e.Mtime || info.Size() != e.Size {
		return nil, false
	}
	return e.Lines, true
}

// Put stores the full file content and marks [startLine, endLine] as read.
func (c *FileStateCache) Put(path string, lines []string, startLine, endLine int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	info, err := os.Stat(path)
	if err != nil {
		return
	}
	key := normalizePath(path)
	e, ok := c.entries[key]
	if !ok || info.ModTime().UnixNano() != e.Mtime || info.Size() != e.Size {
		// Fresh or changed: replace.
		e = &fileState{
			Lines: lines,
			Mtime: info.ModTime().UnixNano(),
			Size:  info.Size(),
		}
		c.entries[key] = e
		c.order = append(c.order, key)
	}
	// Merge the new range.
	e.Ranges = mergeRanges(e.Ranges, lineRange{startLine, endLine})
	c.evictIfNeeded()
}

func (c *FileStateCache) Invalidate(path string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.remove(normalizePath(path))
}

func (c *FileStateCache) Merge(other *FileStateCache) {
	if other == nil {
		return
	}
	other.mu.RLock()
	c.mu.Lock()
	defer c.mu.Unlock()
	defer other.mu.RUnlock()

	for p, e := range other.entries {
		if existing, ok := c.entries[p]; !ok || e.Mtime > existing.Mtime {
			if ok {
				c.remove(p)
			}
			c.entries[p] = e
			c.order = append(c.order, p)
		}
	}
	c.evictIfNeeded()
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

// mergeRanges inserts r into a sorted, non-overlapping slice of ranges.
func mergeRanges(ranges []lineRange, r lineRange) []lineRange {
	ranges = append(ranges, r)
	sort.Slice(ranges, func(i, j int) bool { return ranges[i].Start < ranges[j].Start })

	merged := ranges[:0]
	for _, rg := range ranges {
		if len(merged) == 0 || merged[len(merged)-1].End < rg.Start-1 {
			merged = append(merged, rg)
		} else if rg.End > merged[len(merged)-1].End {
			merged[len(merged)-1].End = rg.End
		}
	}
	return merged
}

func normalizePath(p string) string {
	if resolved, err := filepath.EvalSymlinks(filepath.Clean(p)); err == nil {
		return resolved
	}
	return filepath.Clean(p)
}
