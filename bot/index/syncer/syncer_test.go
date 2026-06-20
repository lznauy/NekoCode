package syncer

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	graphpkg "nekocode/bot/index/graph"
	indexerpkg "nekocode/bot/index/indexer"
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
	t.Cleanup(s.Stop)

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
	t.Cleanup(s.Stop)

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
