package db

import (
	"path/filepath"
	"strings"
	"testing"

	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"

	graphpkg "nekocode/bot/extension/tool/builtin/index/core/internal/graph"
)

func TestReplaceFileRollsBackSymbolsHashAndFTS(t *testing.T) {
	d := openTestDB(t)
	old := &graphpkg.Node{ID: 1, Name: "Original", Kind: graphpkg.KindFunc, File: "a.go"}
	if err := d.ReplaceFile("a.go", "old", "go", []*graphpkg.Node{old}, nil); err != nil {
		t.Fatal(err)
	}
	updated := &graphpkg.Node{ID: 2, Name: "Replacement", Kind: graphpkg.KindFunc, File: "a.go"}
	invalid := &graphpkg.Edge{ID: 1, FromID: 2, ToID: 999, Kind: graphpkg.EdgeCalls, File: "a.go"}
	if err := d.ReplaceFile("a.go", "new", "go", []*graphpkg.Node{updated}, []*graphpkg.Edge{invalid}); err == nil {
		t.Fatal("invalid foreign key was accepted")
	}
	g, err := d.LoadGraph()
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Nodes) != 1 || g.Nodes[1] == nil || g.Nodes[1].Name != "Original" || len(g.Edges) != 0 || d.GetFileHash("a.go") != "old" {
		t.Fatalf("failed transaction changed existing file: %+v", g)
	}
	results, err := d.SearchFTS("Original", 10)
	if err != nil || len(results) != 1 {
		t.Fatalf("original FTS entry lost: %v, %v", results, err)
	}
	results, err = d.SearchFTS("Replacement", 10)
	if err != nil || len(results) != 0 {
		t.Fatalf("rolled back FTS entry retained: %v, %v", results, err)
	}
}

func TestReplaceFilePersistsUnresolvedEdges(t *testing.T) {
	d := openTestDB(t)
	node := &graphpkg.Node{ID: 1, Name: "Caller", Kind: graphpkg.KindFunc, File: "a.go"}
	edge := &graphpkg.Edge{ID: 1, FromID: 1, Kind: graphpkg.EdgeCalls, File: "a.go", CalleeName: "External"}
	if err := d.ReplaceFile("a.go", "hash", "go", []*graphpkg.Node{node}, []*graphpkg.Edge{edge}); err != nil {
		t.Fatal(err)
	}
	g, err := d.LoadGraph()
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Edges) != 1 || g.Edges[1].ToID != 0 || g.Edges[1].CalleeName != "External" {
		t.Fatalf("unresolved reference did not round-trip: %+v", g.Edges)
	}
}

func TestLegacyIndexMigrationPreservesRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	conn, err := sqlite.OpenConn(path, sqlite.OpenReadWrite|sqlite.OpenCreate)
	if err != nil {
		t.Fatal(err)
	}
	legacy := strings.Replace(schemaSQL, "to_id INTEGER REFERENCES", "to_id INTEGER NOT NULL REFERENCES", 1)
	err = sqlitex.ExecScript(conn, legacy+`
 INSERT INTO nodes (id,name,kind,file,line) VALUES (1,'Caller','func','a.go',1),(2,'Target','func','b.go',1);
 INSERT INTO edges (id,from_id,to_id,kind,file,callee_name) VALUES (1,1,2,'calls','a.go','Target');
 INSERT INTO files (path,content_hash,language) VALUES ('a.go','old','go');
 `)
	closeErr := conn.Close()
	if err != nil {
		t.Fatal(err)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	d, err := OpenDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := d.migrate(); err != nil {
		t.Fatalf("repeat migration: %v", err)
	}
	g, err := d.LoadGraph()
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Nodes) != 2 || len(g.Edges) != 1 || g.Edges[1].ToID != 2 || d.GetFileHash("a.go") != "old" {
		t.Fatalf("migration lost index data: %+v", g)
	}
	if err := d.SaveEdge(&graphpkg.Edge{ID: 2, FromID: 1, Kind: graphpkg.EdgeCalls, CalleeName: "Unknown"}); err != nil {
		t.Fatalf("migrated schema rejects unresolved target: %v", err)
	}
	matches, err := d.SearchFTS("Caller", 10)
	if err != nil || len(matches) != 1 {
		t.Fatalf("migration damaged FTS: %v, %v", matches, err)
	}
}
