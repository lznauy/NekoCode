package cindex

import (
	"os"
	"path/filepath"
	"testing"
)

func setupTestProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	// main.go — imports and calls handler
	os.WriteFile(filepath.Join(dir, "main.go"), []byte(`package main

import "myproject/handler"

func main() {
	handler.Handle("hello")
	handler.Process("world")
}
`), 0644)

	// handler/handler.go — defines Handle and Process, calls util.Format
	os.MkdirAll(filepath.Join(dir, "handler"), 0755)
	os.WriteFile(filepath.Join(dir, "handler", "handler.go"), []byte(`package handler

import "myproject/util"

// Handle processes a request.
func Handle(name string) {
	util.Format(name)
}

// Process transforms data.
func Process(data string) {
	util.Format(data)
}
`), 0644)

	// util/util.go — defines Format
	os.MkdirAll(filepath.Join(dir, "util"), 0755)
	os.WriteFile(filepath.Join(dir, "util", "util.go"), []byte(`package util

// Format formats a string.
func Format(s string) string {
	return s
}
`), 0644)

	return dir
}

func TestIndexAll(t *testing.T) {
	dir := setupTestProject(t)

	dbPath := filepath.Join(dir, ".nekocode", "cindex.db")
	os.MkdirAll(filepath.Dir(dbPath), 0755)

	indexer, err := NewIndexer(dbPath)
	if err != nil {
		t.Skipf("NewIndexer failed (FTS5 may be unavailable): %v", err)
	}
	defer indexer.Close()

	g, err := indexer.IndexAll(dir)
	if err != nil {
		t.Fatalf("IndexAll: %v", err)
	}

	// Should have nodes from all 3 files
	if len(g.Nodes) < 4 {
		t.Errorf("len(Nodes) = %d, want >= 4 (main, Handle, Process, Format)", len(g.Nodes))
	}

	// Check that files were recorded
	if len(g.files) != 3 {
		t.Errorf("len(files) = %d, want 3", len(g.files))
	}

	// Check that we have call edges
	callEdges := 0
	for _, e := range g.Edges {
		if e.Kind == EdgeCalls {
			callEdges++
		}
	}
	if callEdges < 3 {
		t.Errorf("call edges = %d, want >= 3", callEdges)
	}

	// Import edges should be created (associated with file nodes)
	importEdges := 0
	for _, e := range g.Edges {
		if e.Kind == EdgeImports {
			importEdges++
		}
	}
	if importEdges < 2 {
		t.Errorf("import edges = %d, want >= 2", importEdges)
	}

	// File nodes should be created for each file
	fileNodes := 0
	for _, n := range g.Nodes {
		if n.Kind == "file" {
			fileNodes++
		}
	}
	if fileNodes != 3 {
		t.Errorf("file nodes = %d, want 3", fileNodes)
	}
}

func TestIndexAllResolveReferences(t *testing.T) {
	dir := setupTestProject(t)

	dbPath := filepath.Join(dir, ".nekocode", "cindex.db")
	os.MkdirAll(filepath.Dir(dbPath), 0755)

	indexer, err := NewIndexer(dbPath)
	if err != nil {
		t.Skipf("NewIndexer failed (FTS5 may be unavailable): %v", err)
	}
	defer indexer.Close()

	g, err := indexer.IndexAll(dir)
	if err != nil {
		t.Fatalf("IndexAll: %v", err)
	}

	// Check that cross-file call edges are resolved (ToID != 0)
	unresolved := 0
	for _, e := range g.Edges {
		if e.Kind == EdgeCalls && e.ToID == 0 {
			unresolved++
		}
	}
	if unresolved > 0 {
		t.Errorf("unresolved call edges = %d, want 0", unresolved)
	}
}

func TestLoadOrBuild(t *testing.T) {
	dir := setupTestProject(t)

	dbPath := filepath.Join(dir, ".nekocode", "cindex.db")
	os.MkdirAll(filepath.Dir(dbPath), 0755)

	indexer, err := NewIndexer(dbPath)
	if err != nil {
		t.Skipf("NewIndexer failed (FTS5 may be unavailable): %v", err)
	}
	defer indexer.Close()

	// First call: should build fresh
	g1, err := indexer.LoadOrBuild(dir)
	if err != nil {
		t.Fatalf("LoadOrBuild (first): %v", err)
	}
	if len(g1.Nodes) == 0 {
		t.Error("first LoadOrBuild should produce nodes")
	}

	// Second call: should load from DB
	g2, err := indexer.LoadOrBuild(dir)
	if err != nil {
		t.Fatalf("LoadOrBuild (second): %v", err)
	}
	if len(g2.Nodes) != len(g1.Nodes) {
		t.Errorf("second LoadOrBuild: %d nodes, want %d", len(g2.Nodes), len(g1.Nodes))
	}
}

func TestQueryDepsEndToEnd(t *testing.T) {
	dir := setupTestProject(t)

	dbPath := filepath.Join(dir, ".nekocode", "cindex.db")
	os.MkdirAll(filepath.Dir(dbPath), 0755)

	indexer, err := NewIndexer(dbPath)
	if err != nil {
		t.Skipf("NewIndexer failed (FTS5 may be unavailable): %v", err)
	}
	defer indexer.Close()

	g, err := indexer.IndexAll(dir)
	if err != nil {
		t.Fatalf("IndexAll: %v", err)
	}

	// main imports handler, handler imports util
	deps := g.QueryDeps("main")
	if len(deps) != 1 {
		t.Errorf("QueryDeps('main') = %d deps, want 1 (handler)", len(deps))
	}

	deps = g.QueryDeps("handler")
	if len(deps) != 1 {
		t.Errorf("QueryDeps('handler') = %d deps, want 1 (util)", len(deps))
	}

	deps = g.QueryDeps("util")
	if deps != nil {
		t.Errorf("QueryDeps('util') = %v, want nil (no deps)", deps)
	}
}

func TestResolveReferences(t *testing.T) {
	g := NewGraph()
	// Simulate two files: main calls handler.Handle
	mainFn := &Node{Name: "main", Kind: KindFunc, File: "main.go", Line: 1, PkgPath: "main"}
	handleFn := &Node{Name: "Handle", Kind: KindFunc, File: "handler.go", Line: 1, PkgPath: "handler"}
	g.AddNode(mainFn)
	g.AddNode(handleFn)

	// Unresolved call edge
	e := &Edge{FromID: mainFn.ID, ToID: 0, Kind: EdgeCalls, CalleeName: "Handle"}
	g.AddEdge(e)

	indexer := &Indexer{}
	indexer.ResolveReferences(g)

	if e.ToID != handleFn.ID {
		t.Errorf("ResolveReferences: ToID = %d, want %d", e.ToID, handleFn.ID)
	}
}

func TestResolveReferencesSamePackagePriority(t *testing.T) {
	g := NewGraph()
	// Two packages both have a function named "Process"
	mainFn := &Node{Name: "main", Kind: KindFunc, File: "main.go", Line: 1, PkgPath: "main"}
	processMain := &Node{Name: "Process", Kind: KindFunc, File: "main.go", Line: 10, PkgPath: "main"}
	processOther := &Node{Name: "Process", Kind: KindFunc, File: "other.go", Line: 1, PkgPath: "other"}
	g.AddNode(mainFn)
	g.AddNode(processMain)
	g.AddNode(processOther)

	e := &Edge{FromID: mainFn.ID, ToID: 0, Kind: EdgeCalls, CalleeName: "Process"}
	g.AddEdge(e)

	indexer := &Indexer{}
	indexer.ResolveReferences(g)

	// Should prefer same-package candidate
	if e.ToID != processMain.ID {
		t.Errorf("ResolveReferences: ToID = %d, want %d (same package)", e.ToID, processMain.ID)
	}
}

func TestIndexAllSkipsIgnoredDirs(t *testing.T) {
	dir := t.TempDir()

	// Normal file
	os.WriteFile(filepath.Join(dir, "main.go"), []byte(`package main
func main() {}
`), 0644)

	// File in ignored directory
	os.MkdirAll(filepath.Join(dir, "vendor", "lib"), 0755)
	os.WriteFile(filepath.Join(dir, "vendor", "lib", "lib.go"), []byte(`package lib
func Lib() {}
`), 0644)

	// File in dot directory
	os.MkdirAll(filepath.Join(dir, ".hidden"), 0755)
	os.WriteFile(filepath.Join(dir, ".hidden", "secret.go"), []byte(`package secret
func Secret() {}
`), 0644)

	dbPath := filepath.Join(dir, ".nekocode", "cindex.db")
	os.MkdirAll(filepath.Dir(dbPath), 0755)

	indexer, err := NewIndexer(dbPath)
	if err != nil {
		t.Skipf("NewIndexer failed (FTS5 may be unavailable): %v", err)
	}
	defer indexer.Close()

	g, err := indexer.IndexAll(dir)
	if err != nil {
		t.Fatalf("IndexAll: %v", err)
	}

	// Should only have nodes from main.go
	for _, n := range g.Nodes {
		if n.Name == "Lib" {
			t.Error("should not index vendor/ files")
		}
		if n.Name == "Secret" {
			t.Error("should not index .hidden/ files")
		}
	}
}

func TestIndexAllSkipsGeneratedGoFiles(t *testing.T) {
	dir := t.TempDir()

	os.WriteFile(filepath.Join(dir, "main.go"), []byte(`package main
func main() {}
`), 0644)
	os.WriteFile(filepath.Join(dir, "api.pb.go"), []byte(`package main
func Generated() {}
`), 0644)
	os.WriteFile(filepath.Join(dir, "service.mock.go"), []byte(`package main
func Mock() {}
`), 0644)

	dbPath := filepath.Join(dir, ".nekocode", "cindex.db")
	os.MkdirAll(filepath.Dir(dbPath), 0755)

	indexer, err := NewIndexer(dbPath)
	if err != nil {
		t.Skipf("NewIndexer failed (FTS5 may be unavailable): %v", err)
	}
	defer indexer.Close()

	g, err := indexer.IndexAll(dir)
	if err != nil {
		t.Fatalf("IndexAll: %v", err)
	}

	for _, n := range g.Nodes {
		if n.Name == "Generated" {
			t.Error("should not index .pb.go files")
		}
		if n.Name == "Mock" {
			t.Error("should not index .gen.go files")
		}
	}
}
