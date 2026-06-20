package db

import (
	"path/filepath"
	"testing"

	graphpkg "nekocode/bot/index/graph"
)

func openTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Skipf("OpenDB failed (FTS5 may be unavailable): %v", err)
		return nil
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestSaveNodeAndLoadGraph(t *testing.T) {
	db := openTestDB(t)

	n := &graphpkg.Node{ID: 1, Name: "foo", Kind: graphpkg.KindFunc, File: "a.go", Line: 10, EndLine: 20, PkgPath: "main", Signature: "func foo()", Doc: "// foo does stuff", Visibility: "public"}
	if err := db.SaveNode(n); err != nil {
		t.Fatalf("SaveNode: %v", err)
	}

	g, err := db.LoadGraph()
	if err != nil {
		t.Fatalf("LoadGraph: %v", err)
	}
	if len(g.Nodes) != 1 {
		t.Fatalf("len(Nodes) = %d, want 1", len(g.Nodes))
	}
	loaded := g.Nodes[1]
	if loaded.Name != "foo" || loaded.Kind != graphpkg.KindFunc || loaded.Line != 10 || loaded.PkgPath != "main" {
		t.Errorf("loaded node = %+v", loaded)
	}
}

func TestSaveNodeUpsert(t *testing.T) {
	db := openTestDB(t)

	n := &graphpkg.Node{ID: 1, Name: "foo", Kind: graphpkg.KindFunc, File: "a.go", Line: 1}
	db.SaveNode(n)

	// Update same ID
	n2 := &graphpkg.Node{ID: 1, Name: "bar", Kind: graphpkg.KindStruct, File: "b.go", Line: 5}
	db.SaveNode(n2)

	g, _ := db.LoadGraph()
	if len(g.Nodes) != 1 {
		t.Fatalf("len(Nodes) = %d, want 1 after upsert", len(g.Nodes))
	}
	if g.Nodes[1].Name != "bar" {
		t.Errorf("name after upsert = %q, want bar", g.Nodes[1].Name)
	}
}

func TestSaveEdgeAndLoadGraph(t *testing.T) {
	db := openTestDB(t)

	db.SaveNode(&graphpkg.Node{ID: 1, Name: "a", Kind: graphpkg.KindFunc, File: "a.go", Line: 1})
	db.SaveNode(&graphpkg.Node{ID: 2, Name: "b", Kind: graphpkg.KindFunc, File: "a.go", Line: 5})
	db.SaveEdge(&graphpkg.Edge{ID: 1, FromID: 1, ToID: 2, Kind: graphpkg.EdgeCalls, File: "a.go", Line: 3})

	g, err := db.LoadGraph()
	if err != nil {
		t.Fatalf("LoadGraph: %v", err)
	}
	if len(g.Edges) != 1 {
		t.Fatalf("len(Edges) = %d, want 1", len(g.Edges))
	}
	e := g.Edges[1]
	if e.FromID != 1 || e.ToID != 2 || e.Kind != graphpkg.EdgeCalls {
		t.Errorf("loaded edge = %+v", e)
	}
}

func TestSaveFileAndGetFileHash(t *testing.T) {
	db := openTestDB(t)

	if err := db.SaveFile("a.go", "abc123", "go"); err != nil {
		t.Fatalf("SaveFile: %v", err)
	}
	hash := db.GetFileHash("a.go")
	if hash != "abc123" {
		t.Errorf("GetFileHash = %q, want abc123", hash)
	}

	// Non-existent file
	hash = db.GetFileHash("nonexistent.go")
	if hash != "" {
		t.Errorf("GetFileHash(nonexistent) = %q, want empty", hash)
	}

	// Upsert
	db.SaveFile("a.go", "def456", "go")
	hash = db.GetFileHash("a.go")
	if hash != "def456" {
		t.Errorf("GetFileHash after upsert = %q, want def456", hash)
	}
}

func TestDeleteFile(t *testing.T) {
	db := openTestDB(t)

	db.SaveNode(&graphpkg.Node{ID: 1, Name: "a", Kind: graphpkg.KindFunc, File: "a.go", Line: 1})
	db.SaveNode(&graphpkg.Node{ID: 2, Name: "b", Kind: graphpkg.KindFunc, File: "a.go", Line: 5})
	db.SaveNode(&graphpkg.Node{ID: 3, Name: "c", Kind: graphpkg.KindFunc, File: "b.go", Line: 1})
	db.SaveEdge(&graphpkg.Edge{ID: 1, FromID: 1, ToID: 2, Kind: graphpkg.EdgeCalls, File: "a.go", Line: 3})
	db.SaveEdge(&graphpkg.Edge{ID: 2, FromID: 1, ToID: 3, Kind: graphpkg.EdgeCalls, File: "a.go", Line: 4})
	db.SaveFile("a.go", "hash1", "go")
	db.SaveFile("b.go", "hash2", "go")

	if err := db.DeleteFile("a.go"); err != nil {
		t.Fatalf("DeleteFile: %v", err)
	}

	g, _ := db.LoadGraph()
	if len(g.Nodes) != 1 {
		t.Errorf("len(Nodes) = %d, want 1 (only b.go)", len(g.Nodes))
	}
	if len(g.Edges) != 0 {
		t.Errorf("len(Edges) = %d, want 0", len(g.Edges))
	}
	if db.GetFileHash("a.go") != "" {
		t.Error("file hash should be deleted")
	}
}

func TestClear(t *testing.T) {
	db := openTestDB(t)

	db.SaveNode(&graphpkg.Node{ID: 1, Name: "a", Kind: graphpkg.KindFunc, File: "a.go", Line: 1})
	db.SaveEdge(&graphpkg.Edge{ID: 1, FromID: 1, ToID: 1, Kind: graphpkg.EdgeCalls})
	db.SaveFile("a.go", "hash", "go")

	if err := db.Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}

	g, _ := db.LoadGraph()
	if len(g.Nodes) != 0 || len(g.Edges) != 0 {
		t.Error("graph should be empty after Clear")
	}
	if db.FileCount() != 0 {
		t.Error("FileCount should be 0 after Clear")
	}
}

func TestFileCountNodeCount(t *testing.T) {
	db := openTestDB(t)

	db.SaveNode(&graphpkg.Node{ID: 1, Name: "a", Kind: graphpkg.KindFunc, File: "a.go", Line: 1})
	db.SaveNode(&graphpkg.Node{ID: 2, Name: "b", Kind: graphpkg.KindFunc, File: "b.go", Line: 1})
	db.SaveFile("a.go", "h1", "go")
	db.SaveFile("b.go", "h2", "go")

	if db.NodeCount() != 2 {
		t.Errorf("NodeCount = %d, want 2", db.NodeCount())
	}
	if db.FileCount() != 2 {
		t.Errorf("FileCount = %d, want 2", db.FileCount())
	}
}

func TestSearchFTS(t *testing.T) {
	db := openTestDB(t)

	db.SaveNode(&graphpkg.Node{ID: 1, Name: "handleRequest", Kind: graphpkg.KindFunc, File: "a.go", Line: 1, Signature: "func HandleRequest(w http.ResponseWriter, r *http.Request)", Doc: "// HandleRequest processes incoming HTTP requests"})
	db.SaveNode(&graphpkg.Node{ID: 2, Name: "processData", Kind: graphpkg.KindFunc, File: "b.go", Line: 1, Signature: "func processData(data []byte)", Doc: "// processData transforms raw bytes"})
	db.SaveNode(&graphpkg.Node{ID: 3, Name: "MyStruct", Kind: graphpkg.KindStruct, File: "c.go", Line: 1})

	// Search by name
	nodes, err := db.SearchFTS("handleRequest", 10)
	if err != nil {
		t.Fatalf("SearchFTS: %v", err)
	}
	if len(nodes) != 1 || nodes[0].Name != "handleRequest" {
		t.Errorf("search 'handleRequest': got %d results", len(nodes))
	}

	// Search by signature
	nodes, err = db.SearchFTS("ResponseWriter", 10)
	if err != nil {
		t.Fatalf("SearchFTS: %v", err)
	}
	if len(nodes) != 1 {
		t.Errorf("search 'ResponseWriter': got %d results, want 1", len(nodes))
	}

	// Search by doc
	nodes, err = db.SearchFTS("processes", 10)
	if err != nil {
		t.Fatalf("SearchFTS: %v", err)
	}
	if len(nodes) != 1 {
		t.Errorf("search 'processes': got %d results, want 1", len(nodes))
	}

	// No results
	nodes, err = db.SearchFTS("nonexistent", 10)
	if err != nil {
		t.Fatalf("SearchFTS: %v", err)
	}
	if len(nodes) != 0 {
		t.Errorf("search 'nonexistent': got %d results, want 0", len(nodes))
	}
}

func TestFTSTriggers(t *testing.T) {
	db := openTestDB(t)

	// INSERT
	db.SaveNode(&graphpkg.Node{ID: 1, Name: "alpha", Kind: graphpkg.KindFunc, File: "a.go", Line: 1, Doc: "// alpha function"})
	nodes, _ := db.SearchFTS("alpha", 10)
	if len(nodes) != 1 {
		t.Fatalf("FTS after INSERT: got %d, want 1", len(nodes))
	}

	// UPDATE
	db.SaveNode(&graphpkg.Node{ID: 1, Name: "beta", Kind: graphpkg.KindFunc, File: "a.go", Line: 1, Doc: "// beta function"})
	nodes, _ = db.SearchFTS("alpha", 10)
	if len(nodes) != 0 {
		t.Error("FTS should not find old name after UPDATE")
	}
	nodes, _ = db.SearchFTS("beta", 10)
	if len(nodes) != 1 {
		t.Errorf("FTS after UPDATE: got %d, want 1", len(nodes))
	}

	// DELETE (via Clear)
	db.Clear()
	nodes, _ = db.SearchFTS("beta", 10)
	if len(nodes) != 0 {
		t.Error("FTS should be empty after Clear")
	}
}
