package syncer

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"

	graphpkg "nekocode/bot/extension/tool/builtin/index/core/internal/graph"
	indexerpkg "nekocode/bot/extension/tool/builtin/index/core/internal/indexer"
)

func TestSyncerIndexesAndRemovesSupportedFileEvents(t *testing.T) {
	cwd := t.TempDir()
	idx, err := indexerpkg.NewIndexer(filepath.Join(cwd, "index.db"))
	if err != nil {
		t.Fatalf("NewIndexer() error = %v", err)
	}
	t.Cleanup(func() {
		if err := idx.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	})

	var graphMu sync.RWMutex
	g := graphpkg.NewGraph()
	s, err := NewSyncer(idx, cwd, &graphMu)
	if err != nil {
		t.Fatalf("NewSyncer() error = %v", err)
	}
	s.SetGraph(g)
	s.Start()
	t.Cleanup(func() {
		if err := s.Stop(); err != nil {
			t.Errorf("stop syncer: %v", err)
		}
	})

	path := filepath.Join(cwd, "main.go")
	if err := os.WriteFile(path, []byte("package main\n\nfunc Hello() {}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	waitForGraph(t, &graphMu, g, func(g *graphpkg.Graph) bool {
		return len(g.FindNodesByFile("main.go")) > 0
	})

	if err := os.Remove(path); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}

	waitForGraph(t, &graphMu, g, func(g *graphpkg.Graph) bool {
		return len(g.FindNodesByFile("main.go")) == 0
	})
}

func TestSyncerRemovesRenamedPaths(t *testing.T) {
	for _, mode := range []string{"file", "directory", "directory_moved_out"} {
		t.Run(mode, func(t *testing.T) {
			cwd := t.TempDir()
			old := filepath.Join(cwd, "old")
			next := filepath.Join(cwd, "next")
			oldFile, nextFile := filepath.Join(old, "nested", "source.go"), filepath.Join(next, "nested", "source.go")
			if mode == "file" {
				old, next = old+".go", next+".go"
				oldFile, nextFile = old, next
			}
			if mode == "directory_moved_out" {
				next = filepath.Join(t.TempDir(), "moved")
			}
			if err := os.MkdirAll(filepath.Dir(oldFile), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(oldFile, []byte("package sample\nfunc Original() {}\n"), 0600); err != nil {
				t.Fatal(err)
			}
			idx, err := indexerpkg.NewIndexer(filepath.Join(t.TempDir(), "index.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer idx.Close()
			var mu sync.RWMutex
			g := graphpkg.NewGraph()
			s, err := NewSyncer(idx, cwd, &mu)
			if err != nil {
				t.Fatal(err)
			}
			s.SetGraph(g)
			s.handleFileChanges(map[string]fsnotify.Op{oldFile: fsnotify.Create})
			s.Start()
			defer s.Stop()
			if err := os.Rename(old, next); err != nil {
				t.Fatal(err)
			}
			waitForGraph(t, &mu, g, func(g *graphpkg.Graph) bool {
				return !g.HasFileInfo(oldFile) && (mode == "directory_moved_out" || g.HasFileInfo(nextFile))
			})
			if mode != "directory_moved_out" {
				if err := os.WriteFile(nextFile, []byte("package sample\nfunc Updated() {}\n"), 0600); err != nil {
					t.Fatal(err)
				}
				waitForGraph(t, &mu, g, func(g *graphpkg.Graph) bool {
					return len(g.FindNodesByName("Updated")) == 1 && len(g.FindNodesByName("Original")) == 0
				})
			}
		})
	}
}

func TestRemovedDirectoryPreservesSiblingIndex(t *testing.T) {
	for _, metadataOnly := range []bool{false, true} {
		cwd := t.TempDir()
		idx, err := indexerpkg.NewIndexer(filepath.Join(t.TempDir(), "index.db"))
		if err != nil {
			t.Fatal(err)
		}
		var g *graphpkg.Graph
		if !metadataOnly {
			g = graphpkg.NewGraph()
		}
		removed := filepath.Join(cwd, "old", "source.go")
		sibling := filepath.Join(cwd, "old-sibling", "source.go")
		for _, path := range []string{removed, sibling} {
			if err := idx.UpsertFile(g, cwd, path, []byte("package sample\nfunc Example() {}\n")); err != nil {
				t.Fatal(err)
			}
		}
		s := &Syncer{indexer: idx, graph: g, cwd: cwd}
		s.handleFileChanges(map[string]fsnotify.Op{filepath.Join(cwd, "old"): fsnotify.Rename})
		paths, err := idx.IndexedFilePaths()
		if err != nil || len(paths) != 1 || paths[0] != sibling {
			t.Fatalf("metadataOnly=%v paths=%v err=%v", metadataOnly, paths, err)
		}
		if err := idx.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestUnsupportedRemovalsDoNotDelayPendingIndexUpdate(t *testing.T) {
	cwd := t.TempDir()
	ignored := filepath.Join(cwd, ".ignored")
	if err := os.Mkdir(ignored, 0700); err != nil {
		t.Fatal(err)
	}
	idx, err := indexerpkg.NewIndexer(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	var mu sync.RWMutex
	g := graphpkg.NewGraph()
	s, err := NewSyncer(idx, cwd, &mu)
	if err != nil {
		t.Fatal(err)
	}
	s.SetGraph(g)
	s.Start()
	defer s.Stop()
	if err := os.WriteFile(filepath.Join(cwd, "source.go"), []byte("package sample\nfunc Ready() {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case <-tick.C:
			mu.RLock()
			ready := len(g.FindNodesByName("Ready")) == 1
			mu.RUnlock()
			if ready {
				return
			}
			s.watcher.Events <- fsnotify.Event{Name: filepath.Join(cwd, "ignored.tmp"), Op: fsnotify.Remove}
			s.watcher.Events <- fsnotify.Event{Name: ignored, Op: fsnotify.Create}
		case <-deadline:
			t.Fatal("unrelated removals starved the supported file update")
		}
	}
}

func TestSyncerIgnoresUnsupportedFiles(t *testing.T) {
	cwd := t.TempDir()
	idx, err := indexerpkg.NewIndexer(filepath.Join(cwd, "index.db"))
	if err != nil {
		t.Fatalf("NewIndexer() error = %v", err)
	}
	t.Cleanup(func() {
		if err := idx.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	})

	var graphMu sync.RWMutex
	g := graphpkg.NewGraph()
	s, err := NewSyncer(idx, cwd, &graphMu)
	if err != nil {
		t.Fatalf("NewSyncer() error = %v", err)
	}
	s.SetGraph(g)
	s.Start()
	t.Cleanup(func() {
		if err := s.Stop(); err != nil {
			t.Errorf("stop syncer: %v", err)
		}
	})

	if err := os.WriteFile(filepath.Join(cwd, "notes.txt"), []byte("ignore me"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	time.Sleep(700 * time.Millisecond)
	graphMu.RLock()
	defer graphMu.RUnlock()
	if got := len(g.FindNodesByFile("notes.txt")); got != 0 {
		t.Fatalf("unsupported file indexed %d nodes", got)
	}
}

func TestNewSyncerFailureRetainsDegradedStatus(t *testing.T) {
	originalNewWatcher := newWatcher
	t.Cleanup(func() { newWatcher = originalNewWatcher })
	injected := errors.New("injected watcher creation failure")
	newWatcher = func() (*fsnotify.Watcher, error) { return nil, injected }

	s, err := NewSyncer(nil, t.TempDir(), nil)
	if !errors.Is(err, injected) {
		t.Fatalf("NewSyncer() error = %v", err)
	}
	if s == nil {
		t.Fatal("NewSyncer() discarded failed watcher status")
	}
	state, lastErr := s.Status()
	if state != StateDegraded || !errors.Is(lastErr, injected) {
		t.Fatalf("Status() = %q, %v", state, lastErr)
	}
	if err := s.Stop(); err != nil {
		t.Fatal(err)
	}
}

func TestSyncerStatusRunningAndStopped(t *testing.T) {
	s, err := NewSyncer(nil, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	s.Start()
	if state, lastErr := s.Status(); state != StateRunning || lastErr != nil {
		t.Fatalf("running Status() = %q, %v", state, lastErr)
	}
	if err := s.Stop(); err != nil {
		t.Fatal(err)
	}
	if state, lastErr := s.Status(); state != StateStopped || lastErr != nil {
		t.Fatalf("stopped Status() = %q, %v", state, lastErr)
	}
}

func waitForGraph(t *testing.T, graphMu *sync.RWMutex, g *graphpkg.Graph, ok func(*graphpkg.Graph) bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		graphMu.RLock()
		matched := ok(g)
		graphMu.RUnlock()
		if matched {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	graphMu.RLock()
	defer graphMu.RUnlock()
	t.Fatalf("graph condition was not met before timeout; nodes=%d edges=%d", len(g.Nodes), len(g.Edges))
}

func TestBatchUsesFinalStateForRemoveAndCreate(t *testing.T) {
	cwd := t.TempDir()
	idx, err := indexerpkg.NewIndexer(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	g := graphpkg.NewGraph()
	s := &Syncer{indexer: idx, graph: g, cwd: cwd}
	path := filepath.Join(cwd, "a.go")
	if err := idx.UpsertFile(g, cwd, path, []byte("package sample\nfunc Original() {}")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("package sample\nfunc Replacement() {}"), 0600); err != nil {
		t.Fatal(err)
	}
	s.handleFileChanges(map[string]fsnotify.Op{path: fsnotify.Remove | fsnotify.Create | fsnotify.Write})
	if len(g.FindNodesByName("Replacement")) != 1 || len(g.FindNodesByName("Original")) != 0 {
		t.Fatal("recreated file was treated as deleted")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	s.handleFileChanges(map[string]fsnotify.Op{path: fsnotify.Remove})
	if g.FileCount() != 0 || len(g.Nodes) != 0 {
		t.Fatal("removed file retained in graph")
	}
}
