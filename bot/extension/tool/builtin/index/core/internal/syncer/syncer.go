package syncer

import (
	"fmt"
	"nekocode/logger"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	graphpkg "nekocode/bot/extension/tool/builtin/index/core/internal/graph"
	indexerpkg "nekocode/bot/extension/tool/builtin/index/core/internal/indexer"
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
	start   sync.Once
	stop    sync.Once
	stopErr error
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
		if closeErr := watcher.Close(); closeErr != nil {
			return nil, fmt.Errorf("%w (close watcher: %v)", err, closeErr)
		}
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
	s.start.Do(func() { go s.run() })
}

func (s *Syncer) run() {
	defer close(s.doneCh)
	var debounceTimer *time.Timer
	var debounce <-chan time.Time
	pendingChanges := make(map[string]fsnotify.Op)
	defer func() {
		if debounceTimer != nil {
			debounceTimer.Stop()
		}
	}()

	for {
		select {
		case <-s.stopCh:
			return
		case event, ok := <-s.watcher.Events:
			if !ok {
				return
			}
			if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Remove) == 0 {
				continue
			}

			if event.Op&fsnotify.Create != 0 {
				if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
					if !indexerpkg.ShouldSkipDir(info.Name()) {
						_ = s.watcher.Add(event.Name)
					}
					continue
				}
			}

			if !indexerpkg.SupportsFile(event.Name) {
				continue
			}
			pendingChanges[event.Name] |= event.Op
			if debounceTimer == nil {
				debounceTimer = time.NewTimer(500 * time.Millisecond)
			} else {
				if !debounceTimer.Stop() {
					select {
					case <-debounceTimer.C:
					default:
					}
				}
				debounceTimer.Reset(500 * time.Millisecond)
			}
			debounce = debounceTimer.C

		case <-debounce:
			changes := pendingChanges
			pendingChanges = make(map[string]fsnotify.Op)
			debounce = nil
			s.handleFileChanges(changes)

		case _, ok := <-s.watcher.Errors:
			if !ok {
				return
			}
		}
	}
}

// handleFileChanges reads the final file states before taking the graph lock.
// One batch performs reference resolution only after all updates are published.
func (s *Syncer) handleFileChanges(events map[string]fsnotify.Op) {
	paths := make([]string, 0, len(events))
	for path := range events {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	changes := make([]indexerpkg.FileChange, 0, len(paths))
	for _, path := range paths {
		content, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			changes = append(changes, indexerpkg.FileChange{Path: path, Deleted: true})
			continue
		}
		if err != nil {
			continue
		}
		changes = append(changes, indexerpkg.FileChange{Path: path, Content: content})
	}
	if len(changes) == 0 {
		return
	}
	if s.graphMu != nil {
		s.graphMu.Lock()
		defer s.graphMu.Unlock()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.indexer.ApplyChanges(s.graph, s.cwd, changes); err != nil {
		logger.Log("index: update files: %v", err)
	}
}

// Stop stops the syncer and waits for the background goroutine to exit.
func (s *Syncer) Stop() error {
	s.Start()
	s.stop.Do(func() {
		close(s.stopCh)
		s.stopErr = s.watcher.Close()
		<-s.doneCh
	})
	return s.stopErr
}

// SetGraph updates the graph reference.
func (s *Syncer) SetGraph(g *graphpkg.Graph) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.graph = g
}
