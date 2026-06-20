package syncer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	graphpkg "nekocode/bot/index/graph"
	indexerpkg "nekocode/bot/index/indexer"
)

// Syncer watches for file changes and updates the graph incrementally.
type Syncer struct {
	indexer *indexerpkg.Indexer
	graph   *graphpkg.Graph
	graphMu *sync.RWMutex
	watcher *fsnotify.Watcher
	cwd     string
	mu      sync.Mutex
	stopCh  chan struct{}
	doneCh  chan struct{}
}

// NewSyncer creates a new file syncer.
func NewSyncer(indexer *indexerpkg.Indexer, cwd string, graphMu *sync.RWMutex) (*Syncer, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("create watcher: %w", err)
	}

	s := &Syncer{
		indexer: indexer,
		graphMu: graphMu,
		watcher: watcher,
		cwd:     cwd,
		stopCh:  make(chan struct{}),
		doneCh:  make(chan struct{}),
	}

	// Add directories to watch
	if err := s.addWatchDirs(cwd); err != nil {
		watcher.Close()
		return nil, err
	}

	return s, nil
}

// addWatchDirs recursively adds directories to the watcher.
func (s *Syncer) addWatchDirs(dir string) error {
	const maxDepth = 10
	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			return nil
		}
		name := info.Name()
		if indexerpkg.ShouldSkipDir(name) {
			return filepath.SkipDir
		}
		// Depth limit
		rel, _ := filepath.Rel(dir, path)
		if rel != "." && strings.Count(rel, string(filepath.Separator)) >= maxDepth {
			return filepath.SkipDir
		}
		return s.watcher.Add(path)
	})
}

// Start begins watching for file changes.
func (s *Syncer) Start() {
	go func() {
		defer close(s.doneCh)
		var debounceTimer *time.Timer
		pendingChanges := make(map[string]fsnotify.Op) // path → accumulated ops

		for {
			select {
			case <-s.stopCh:
				return
			case event, ok := <-s.watcher.Events:
				if !ok {
					return
				}
				// Only care about write/create/remove events
				if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Remove) == 0 {
					continue
				}

				// If a new directory was created, watch it (if not ignored)
				if event.Op&fsnotify.Create != 0 {
					if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
						name := info.Name()
						if !indexerpkg.ShouldSkipDir(name) {
							s.watcher.Add(event.Name)
						}
						continue // directory events don't need file-level processing
					}
				}

				if !indexerpkg.SupportsFile(event.Name) {
					continue
				}

				// Accumulate the change (under lock to avoid data race with timer callback)
				s.mu.Lock()
				pendingChanges[event.Name] |= event.Op
				s.mu.Unlock()

				// (Re)set the debounce timer
				if debounceTimer != nil {
					debounceTimer.Stop()
				}
				debounceTimer = time.AfterFunc(500*time.Millisecond, func() {
					s.mu.Lock()
					changes := pendingChanges
					pendingChanges = make(map[string]fsnotify.Op)
					s.mu.Unlock()

					for path, op := range changes {
						s.handleFileChange(path, op)
					}
				})

			case err, ok := <-s.watcher.Errors:
				if !ok {
					return
				}
				_ = err
			}
		}
	}()
}

// handleFileChange processes a file change event.
// File reading and parsing happen outside the lock; only graph/DB mutations hold it.
func (s *Syncer) handleFileChange(path string, op fsnotify.Op) {
	if op&fsnotify.Remove != 0 {
		if s.graphMu != nil {
			s.graphMu.Lock()
			defer s.graphMu.Unlock()
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		_ = s.indexer.DeleteFile(s.graph, path)
		return
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return
	}

	if s.graphMu != nil {
		s.graphMu.Lock()
		defer s.graphMu.Unlock()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.indexer.UpsertFile(s.graph, s.cwd, path, content)
}

// Stop stops the syncer and waits for the background goroutine to exit.
func (s *Syncer) Stop() {
	close(s.stopCh)
	s.watcher.Close()
	<-s.doneCh
}

// SetGraph updates the graph reference.
func (s *Syncer) SetGraph(g *graphpkg.Graph) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.graph = g
}
