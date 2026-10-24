package indexer

import (
	"path/filepath"
	"testing"

	graphpkg "nekocode/bot/extension/tool/builtin/index/core/internal/graph"
)

func TestUpsertWithoutGraphClearsPreviousSymbols(t *testing.T) {
	dir := t.TempDir()
	idx, err := NewIndexer(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	path := filepath.Join(dir, "a.go")
	g := graphpkg.NewGraph()
	if err := idx.UpsertFile(g, dir, path, []byte("package sample\nfunc Original() { External() }")); err != nil {
		t.Fatal(err)
	}
	if err := idx.UpsertFile(nil, dir, path, []byte("package sample\nfunc Replacement() {}")); err != nil {
		t.Fatal(err)
	}
	stored, err := idx.db.LoadGraph()
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.Nodes) != 0 || len(stored.Edges) != 0 {
		t.Fatalf("upsert without graph retained stale symbols: nodes=%d edges=%d", len(stored.Nodes), len(stored.Edges))
	}
	if idx.db.GetFileHash(path) == "" || stored.FileCount() != 1 {
		t.Fatal("upsert without graph did not preserve updated file metadata")
	}
}
