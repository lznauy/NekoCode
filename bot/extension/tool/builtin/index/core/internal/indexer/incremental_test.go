package indexer

import (
	"os"
	"path/filepath"
	"testing"

	graphpkg "nekocode/bot/extension/tool/builtin/index/core/internal/graph"
)

func TestImportResolutionPrefersExactAndLongestPackagePath(t *testing.T) {
	g := graphpkg.NewGraph()
	g.AddNode(&graphpkg.Node{ID: 1, Name: "Short", PkgPath: "util"})
	g.AddNode(&graphpkg.Node{ID: 2, Name: "Long", PkgPath: "team/util"})
	g.AddNode(&graphpkg.Node{ID: 3, Name: "Other", PkgPath: "team/util"})
	for j, path := range []string{"team/util", "module/team/util", "module/util", "module/notutil", "external/missing"} {
		g.AddEdge(&graphpkg.Edge{ID: int64(j + 1), FromID: 1, Kind: graphpkg.EdgeImports, ImportPath: path})
	}
	resolveReferences(g)
	for j, want := range []int64{2, 2, 1, 0, 0} {
		if got := g.Edges[int64(j+1)].ToID; got != want {
			t.Fatalf("import %q target = %d, want %d", g.Edges[int64(j+1)].ImportPath, got, want)
		}
	}
	if g.EdgeToCount(2) != 2 || g.EdgeToCount(1) != 1 || g.EdgeToCount(0) != 2 {
		t.Fatal("incoming edge index was not updated")
	}
}

func TestStartupUpdatesOnlyChangedFilesAndPreservesCallers(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "index.db")
	write := func(name, code string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(code), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("a.go", "package sample\nfunc Caller() { Target() }")
	write("b.go", "package sample\nfunc Target() {}")
	idx, err := NewIndexer(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	g, err := idx.IndexAll(dir)
	if err != nil {
		t.Fatal(err)
	}
	callerID := g.FindNodesByName("Caller")[0].ID
	targetID := g.FindNodesByName("Target")[0].ID
	if err := idx.Close(); err != nil {
		t.Fatal(err)
	}
	write("b.go", "package sample\nfunc Target() {}\nfunc Added() {}")
	idx, err = NewIndexer(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	g, err = idx.LoadOrBuild(dir)
	if err != nil {
		t.Fatal(err)
	}
	if g.FindNodesByName("Caller")[0].ID != callerID {
		t.Fatal("unchanged caller was reindexed")
	}
	if g.FindNodesByName("Target")[0].ID == targetID {
		t.Fatal("modified target was not reindexed")
	}
	assertBatchCall(t, g)
	if g.FileCount() != 2 {
		t.Fatalf("loaded file metadata count = %d", g.FileCount())
	}
	// Deletion and recreation must keep the unchanged caller's named reference.
	if err := os.Remove(filepath.Join(dir, "b.go")); err != nil {
		t.Fatal(err)
	}
	g, err = idx.LoadOrBuild(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.FindNodesByName("Target")) != 0 || g.FileCount() != 1 {
		t.Fatal("deleted target was retained")
	}
	write("c.go", "package sample\nfunc Target() {}")
	g, err = idx.LoadOrBuild(dir)
	if err != nil {
		t.Fatal(err)
	}
	assertBatchCall(t, g)
	if g.FindNodesByName("Caller")[0].ID != callerID {
		t.Fatal("rename reindexed unchanged caller")
	}
	// The same contract applies to an already resolved live graph.
	if err := idx.UpsertFile(g, dir, filepath.Join(dir, "c.go"), []byte("package sample\nfunc Target() {}\n// live edit")); err != nil {
		t.Fatal(err)
	}
	assertBatchCall(t, g)
}

func TestIncompleteStartupScanDoesNotDeleteCachedFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.go")
	if err := os.WriteFile(path, []byte("package sample\nfunc Kept() {}"), 0600); err != nil {
		t.Fatal(err)
	}
	idx, err := NewIndexer(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	if _, err := idx.IndexAll(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "missing"), path); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := idx.LoadOrBuild(dir); err == nil {
		t.Fatal("incomplete scan was accepted")
	}
	stored, err := idx.db.LoadGraph()
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.FindNodesByName("Kept")) != 1 || stored.FileCount() != 1 {
		t.Fatal("scan error removed cached file")
	}
}
