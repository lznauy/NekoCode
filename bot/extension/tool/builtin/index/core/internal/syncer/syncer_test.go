package syncer

import (
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
