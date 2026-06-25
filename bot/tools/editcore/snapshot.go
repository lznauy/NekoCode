package editcore

import (
	"sync"
	"time"
)

const (
	defaultMaxPaths   = 30
	defaultMaxPerPath = 4
	defaultMaxBytes   = 64 * 1024 * 1024 // 64 MiB
)

// Snapshot stores a versioned copy of a file's content.
type Snapshot struct {
	Path       string
	Text       string
	Hash       string
	RecordedAt time.Time
}

// SnapshotStore is an LRU cache of file content snapshots keyed by path.
// Each path retains up to maxPerPath versions for 3-way merge recovery.
type SnapshotStore struct {
	mu         sync.RWMutex
	versions   map[string][]Snapshot // path → versions (newest first)
	order      []string              // LRU order, most recent at end
	maxPaths   int
	maxPerPath int
	maxBytes   int64
	totalBytes int64
}

// NewSnapshotStore creates a snapshot store with default limits.
func NewSnapshotStore() *SnapshotStore {
	return &SnapshotStore{
		versions:   make(map[string][]Snapshot),
		maxPaths:   defaultMaxPaths,
		maxPerPath: defaultMaxPerPath,
		maxBytes:   defaultMaxBytes,
	}
}

// Record stores text for path and returns its content hash.
// If the content hash already exists for this path, the timestamp is refreshed
// (read fusion — identical content reuses the same tag).
// The hash is computed from LF-normalized, trailing-whitespace-stripped text,
// so the caller does not need to pre-normalize (RecordSnapshot normalizes).
func (s *SnapshotStore) Record(path, text string) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	// text is already LF-normalized by caller. Hash is computed from
	// stripped text (trailing whitespace removed) for tag stability.
	hash := ComputeFileHash(text)

	versions := s.versions[path]
	// Check for existing snapshot with same hash (read fusion).
	for i, v := range versions {
		if v.Hash == hash {
			versions[i].RecordedAt = time.Now()
			s.touchOrder(path)
			return hash
		}
	}

	// New snapshot.
	snap := Snapshot{
		Path:       path,
		Text:       text,
		Hash:       hash,
		RecordedAt: time.Now(),
	}

	// Prepend to version list.
	versions = append([]Snapshot{snap}, versions...)
	if len(versions) > s.maxPerPath {
		removed := versions[s.maxPerPath]
		s.totalBytes -= int64(len(removed.Text))
		versions = versions[:s.maxPerPath]
	}
	s.totalBytes += int64(len(text))

	// If this is a new path, add to LRU order; otherwise touch it.
	if _, exists := s.versions[path]; !exists {
		s.order = append(s.order, path)
	} else {
		s.touchOrder(path)
	}
	s.versions[path] = versions

	s.evictIfNeeded()
	return hash
}

// ByHash returns a specific snapshot by content hash, or nil.
func (s *SnapshotStore) ByHash(path, hash string) *Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, v := range s.versions[path] {
		if v.Hash == hash {
			v := v
			return &v
		}
	}
	return nil
}

func (s *SnapshotStore) touchOrder(path string) {
	s.removeFromOrder(path)
	s.order = append(s.order, path)
}

func (s *SnapshotStore) removeFromOrder(path string) {
	for i, p := range s.order {
		if p == path {
			s.order = append(s.order[:i], s.order[i+1:]...)
			return
		}
	}
}

func (s *SnapshotStore) evictIfNeeded() {
	for len(s.versions) > s.maxPaths && len(s.order) > 0 {
		s.evictOldest()
	}
	for s.totalBytes > s.maxBytes && len(s.order) > 0 {
		s.evictOldest()
	}
}

func (s *SnapshotStore) evictOldest() {
	oldest := s.order[0]
	s.order = s.order[1:]
	for _, v := range s.versions[oldest] {
		s.totalBytes -= int64(len(v.Text))
	}
	delete(s.versions, oldest)
}
