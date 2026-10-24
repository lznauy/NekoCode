package indexer

import (
	"os"
	"path/filepath"
	"testing"

	graphpkg "nekocode/bot/extension/tool/builtin/index/core/internal/graph"
)

func TestBatchUpdateResolvesCrossFileCallsAndReload(t *testing.T) {
	dir := t.TempDir()
	idx, err := NewIndexer(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	caller := filepath.Join(dir, "a.go")
	callee := filepath.Join(dir, "b.go")
	changes := []FileChange{
		{Path: caller, Content: []byte("package sample\nfunc Caller() { Target() }")},
		{Path: callee, Content: []byte("package sample\nfunc Target() {}")},
	}
	g := graphpkg.NewGraph()
	for iteration := 0; iteration < 2; iteration++ {
		if iteration > 0 {
			for j := range changes {
				changes[j].Content = append(changes[j].Content, []byte("\n// updated")...)
			}
		}
		if err := idx.ApplyChanges(g, dir, changes); err != nil {
			t.Fatal(err)
		}
		assertBatchCall(t, g)
	}
	nodeID, edgeID := g.NextIDs()
	if err := idx.ApplyChanges(g, dir, changes); err != nil {
		t.Fatal(err)
	}
	nextNode, nextEdge := g.NextIDs()
	if nodeID != nextNode || edgeID != nextEdge {
		t.Fatal("unchanged files were reindexed")
	}
	for _, c := range changes {
		if err := os.WriteFile(c.Path, c.Content, 0600); err != nil {
			t.Fatal(err)
		}
	}
	loaded, err := idx.LoadOrBuild(dir)
	if err != nil {
		t.Fatal(err)
	}
	assertBatchCall(t, loaded)
}

func assertBatchCall(t *testing.T, g *graphpkg.Graph) {
	t.Helper()
	callers := g.FindNodesByName("Caller")
	targets := g.FindNodesByName("Target")
	if len(callers) != 1 || len(targets) != 1 {
		t.Fatalf("missing symbols: caller=%d target=%d", len(callers), len(targets))
	}
	callees := g.GetCallees(callers[0].ID)
	if len(callees) != 1 || callees[0].ID != targets[0].ID {
		t.Fatalf("cross-file call was lost: %+v", callees)
	}
}

func TestFailedBatchLeavesLiveFileUnchanged(t *testing.T) {
	dir := t.TempDir()
	idx, err := NewIndexer(filepath.Join(dir, "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	g := graphpkg.NewGraph()
	path := filepath.Join(dir, "a.go")
	if err := idx.UpsertFile(g, dir, path, []byte("package sample\nfunc Original() {}")); err != nil {
		t.Fatal(err)
	}
	if err := idx.Close(); err != nil {
		t.Fatal(err)
	}
	if err := idx.UpsertFile(g, dir, path, []byte("package sample\nfunc Replacement() {}")); err == nil {
		t.Fatal("closed database accepted update")
	}
	if len(g.FindNodesByName("Original")) != 1 || len(g.FindNodesByName("Replacement")) != 0 {
		t.Fatal("failed persistence mutated live graph")
	}
}

func TestIndexAllReloadKeepsCalls(t *testing.T) {
	dir := setupTestProject(t)
	idx, err := NewIndexer(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	initial, err := idx.IndexAll(dir)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := idx.LoadOrBuild(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range []*graphpkg.Graph{initial, loaded} {
		handles := g.FindNodesByName("Handle")
		var found bool
		for _, n := range handles {
			if n.Kind == graphpkg.KindFunc && n.Name == "Handle" {
				found = len(g.GetCallers(n.ID)) > 0
			}
		}
		if !found {
			t.Fatal("index reload lost cross-file callers")
		}
	}
}
