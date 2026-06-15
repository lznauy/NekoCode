package tools

import (
	"os"
	"sort"
	"sync"

	"nekocode/bot/tools/hashline"
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

var globalFileCache *FileStateCache
var globalSnapshotStore *hashline.SnapshotStore
var globalCacheMu sync.Mutex // protects save/swap/restore sequences in subagent engine

// SetGlobalFileCache sets the global file state cache.
func SetGlobalFileCache(c *FileStateCache) { globalFileCache = c }

// GetGlobalFileCache returns the global file state cache.
func GetGlobalFileCache() *FileStateCache { return globalFileCache }

// GlobalCacheMu returns the mutex that protects the global file cache
// save/swap/restore sequence in the subagent engine.
func GlobalCacheMu() *sync.Mutex { return &globalCacheMu }

// SetGlobalSnapshotStore sets the global snapshot store.
func SetGlobalSnapshotStore(s *hashline.SnapshotStore) { globalSnapshotStore = s }

// GetGlobalSnapshotStore returns the global snapshot store.
func GetGlobalSnapshotStore() *hashline.SnapshotStore { return globalSnapshotStore }

func NewFileStateCache() *FileStateCache {
	return &FileStateCache{entries: make(map[string]*fileState)}
}

// Lines returns the cached full-file content if the file hasn't changed.
func (c *FileStateCache) Lines(path string) ([]string, bool) {
	key := normalizePath(path)

	// Fast path: lookup under read lock only (no I/O).
	c.mu.RLock()
	e, ok := c.entries[key]
	if !ok || e.Lines == nil {
		c.mu.RUnlock()
		return nil, false
	}
	// Copy fields needed for staleness check.
	mtime, size := e.Mtime, e.Size
	lines := e.Lines
	c.mu.RUnlock()

	// Staleness check outside lock (os.Stat may block on slow filesystems).
	info, err := os.Stat(path)
	if err != nil || info.ModTime().UnixNano() != mtime || info.Size() != size {
		// Evict stale entry only if it hasn't already been refreshed by
		// another goroutine. Compare against the fresh stat values, not
		// the old cached values, to avoid evicting a just-updated entry.
		// Guard against nil info when os.Stat fails (file deleted).
		c.mu.Lock()
		if err == nil {
			newMtime := info.ModTime().UnixNano()
			newSize := info.Size()
			if cur, exists := c.entries[key]; exists && (cur.Mtime != newMtime || cur.Size != newSize) {
				c.remove(key)
			}
		} else {
			// File no longer exists — remove from cache.
			c.remove(key)
		}
		c.mu.Unlock()
		return nil, false
	}
	return lines, true
}

// Put stores the full file content and marks [startLine, endLine] as read.
func (c *FileStateCache) Put(path string, lines []string, startLine, endLine int) {
	// Stat outside lock to avoid blocking readers on slow filesystems.
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
		// Fresh or changed: replace.
		e = &fileState{
			Lines: lines,
			Mtime: mtime,
			Size:  size,
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

// Seed copies entries from src into this cache. Used to warm a subagent cache
// with the main agent's previously read files, avoiding cold-start disk reads.
// Only copies entries that don't already exist in this cache.
func (c *FileStateCache) Seed(src *FileStateCache) {
	c.transferFrom(src, false)
}

// Merge copies entries from other into this cache, replacing entries with
// older Mtime values. Used to merge a subagent's cache back after completion.
func (c *FileStateCache) Merge(other *FileStateCache) {
	c.transferFrom(other, true)
}

// transferFrom copies entries from src. If overwrite is true, existing entries
// are replaced when the source has a newer Mtime.
func (c *FileStateCache) transferFrom(src *FileStateCache, overwrite bool) {
	if src == nil {
		return
	}
	src.mu.RLock()
	c.mu.Lock()
	defer c.mu.Unlock()
	defer src.mu.RUnlock()

	for p, e := range src.entries {
		if existing, ok := c.entries[p]; ok {
			if overwrite && e.Mtime > existing.Mtime {
				c.remove(p)
			} else {
				continue
			}
		}
		// Deep copy the struct and its slices to avoid shared mutable
		// state between caches. Shallow copy would share the backing
		// arrays of Lines and Ranges, causing cross-cache corruption
		// when mergeRanges appends to Ranges.
		cp := *e
		if e.Lines != nil {
			cp.Lines = make([]string, len(e.Lines))
			copy(cp.Lines, e.Lines)
		}
		if e.Ranges != nil {
			cp.Ranges = make([]lineRange, len(e.Ranges))
			copy(cp.Ranges, e.Ranges)
		}
		c.entries[p] = &cp
		c.order = append(c.order, p)
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
	return normalizePathKey(p)
}
