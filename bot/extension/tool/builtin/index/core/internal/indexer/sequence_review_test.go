package indexer

import (
	"path/filepath"
	"testing"

	graphpkg "nekocode/bot/extension/tool/builtin/index/core/internal/graph"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

func TestPartialBatchFailureAndRepeatedTargetRecreation(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "index.db")
	idx, err := NewIndexer(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	g := graphpkg.NewGraph()
	caller, target := filepath.Join(dir, "a.go"), filepath.Join(dir, "b.go")
	if err := idx.ApplyChanges(g, dir, []FileChange{
		{Path: caller, Content: []byte("package sample\nfunc Caller() { Target() }")},
		{Path: target, Content: []byte("package sample\nfunc Target() {}")},
	}); err != nil {
		t.Fatal(err)
	}
	conn, err := sqlite.OpenConn(dbPath, sqlite.OpenReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := sqlitex.ExecScript(conn, `CREATE TRIGGER reject_blocked_file BEFORE INSERT ON files
WHEN NEW.path LIKE '%blocked.go' BEGIN SELECT RAISE(ABORT, 'injected file failure'); END;`); err != nil {
		t.Fatal(err)
	}
	err = idx.ApplyChanges(g, dir, []FileChange{
		{Path: target, Content: []byte("package sample\nfunc Target() {}\nfunc Added() {}")},
		{Path: filepath.Join(dir, "blocked.go"), Content: []byte("package sample\nfunc Blocked() {}")},
	})
	if err == nil {
		t.Fatal("batch did not report injected persistence failure")
	}
	if len(g.FindNodesByName("Added")) != 1 || len(g.FindNodesByName("Blocked")) != 0 {
		t.Fatal("partial failure did not retain exactly the successful file update")
	}
	check := func() {
		t.Helper()
		assertBatchCall(t, g)
		stored, err := idx.db.LoadGraph()
		if err != nil {
			t.Fatal(err)
		}
		resolveReferences(stored)
		assertBatchCall(t, stored)
		if len(stored.Nodes) != len(g.Nodes) || len(stored.Edges) != len(g.Edges) {
			t.Fatal("persistent graph and live graph diverged")
		}
	}
	check()
	for iteration := 0; iteration < 5; iteration++ {
		if err := idx.ApplyChanges(g, dir, []FileChange{
			{Path: target, Deleted: true},
			{Path: target, Content: []byte("package sample\nfunc Target() {}")},
			{Path: target, Deleted: true},
			{Path: target, Content: []byte("package sample\nfunc Target() {}\nfunc Added() {}")},
		}); err != nil {
			t.Fatal(err)
		}
		check()
	}
}
