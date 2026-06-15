package cindex

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Syncer watches for file changes and updates the graph incrementally.
type Syncer struct {
	indexer *Indexer
	graph   *Graph
	watcher *fsnotify.Watcher
	cwd     string
	mu      sync.Mutex
	stopCh  chan struct{}
	doneCh  chan struct{}
}

// NewSyncer creates a new file syncer.
func NewSyncer(indexer *Indexer, cwd string) (*Syncer, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("create watcher: %w", err)
	}

	s := &Syncer{
		indexer: indexer,
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
		if ShouldSkipDir(name) {
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
						if !ShouldSkipDir(name) {
							s.watcher.Add(event.Name)
						}
						continue // directory events don't need file-level processing
					}
				}

				// Check if file is supported
				ext := filepath.Ext(event.Name)
				if !supportedExts[ext] {
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
		s.mu.Lock()
		defer s.mu.Unlock()

		if err := s.indexer.db.DeleteFile(path); err != nil {
			return
		}
		if s.graph != nil {
			s.graph.RemoveFileNodes(path)
		}
		return
	}

	// File created or modified — read and parse outside the lock
	content, err := os.ReadFile(path)
	if err != nil {
		return
	}

	hash := fmt.Sprintf("%x", sha256.Sum256(content))
	oldHash := s.indexer.db.GetFileHash(path)
	if oldHash == hash {
		return // No change
	}

	nodes, edges := s.indexer.parser.ParseFile(path, content)

	// Acquire lock for graph and DB mutations
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.indexer.db.DeleteFile(path); err != nil {
		return
	}

	if s.graph != nil {
		s.graph.RemoveFileNodes(path)
		insertFileIntoGraph(s.graph, s.indexer.db, path, s.cwd, nodes, edges)
		s.indexer.ResolveReferences(s.graph)
	}

	lang := detectLanguageForFile(filepath.Ext(path))
	s.indexer.db.SaveFile(path, hash, lang)
}

// Stop stops the syncer and waits for the background goroutine to exit.
func (s *Syncer) Stop() {
	close(s.stopCh)
	s.watcher.Close()
	<-s.doneCh
}

// SetGraph updates the graph reference.
func (s *Syncer) SetGraph(g *Graph) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.graph = g
}
