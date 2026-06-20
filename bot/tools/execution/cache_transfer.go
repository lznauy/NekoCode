package execution

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
