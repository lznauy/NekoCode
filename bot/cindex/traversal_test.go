package cindex

import (
	"testing"
)

// buildTestGraph creates a graph: a → b → c, a → d
func buildTestGraph(t *testing.T) *Graph {
	t.Helper()
	g := NewGraph()
	a := &Node{Name: "a", Kind: KindFunc, File: "x.go", Line: 1}
	b := &Node{Name: "b", Kind: KindFunc, File: "x.go", Line: 5}
	c := &Node{Name: "c", Kind: KindFunc, File: "x.go", Line: 10}
	d := &Node{Name: "d", Kind: KindFunc, File: "x.go", Line: 15}
	g.AddNode(a)
	g.AddNode(b)
	g.AddNode(c)
	g.AddNode(d)
	g.AddEdge(&Edge{FromID: a.ID, ToID: b.ID, Kind: EdgeCalls})
	g.AddEdge(&Edge{FromID: b.ID, ToID: c.ID, Kind: EdgeCalls})
	g.AddEdge(&Edge{FromID: a.ID, ToID: d.ID, Kind: EdgeCalls})
	return g
}

func TestTraverseBFS(t *testing.T) {
	g := buildTestGraph(t)

	// From a: should visit a, b, d, c (BFS order)
	result := g.TraverseBFS(1, nil, 10, 100)
	names := make(map[string]bool)
	for _, n := range result {
		names[n.Name] = true
	}
	if !names["a"] || !names["b"] || !names["c"] || !names["d"] {
		t.Errorf("BFS from a should visit all nodes, got %d nodes", len(result))
	}

	// With edge kind filter — only "calls"
	result = g.TraverseBFS(1, []string{"calls"}, 10, 100)
	if len(result) != 4 {
		t.Errorf("BFS with 'calls' filter: got %d, want 4", len(result))
	}

	// With non-matching edge kind
	result = g.TraverseBFS(1, []string{"imports"}, 10, 100)
	if len(result) != 1 {
		t.Errorf("BFS with 'imports' filter: got %d, want 1 (only start node)", len(result))
	}

	// Max depth 1: only a and its direct neighbors (b, d)
	result = g.TraverseBFS(1, nil, 1, 100)
	if len(result) != 3 {
		t.Errorf("BFS depth=1: got %d, want 3", len(result))
	}

	// Max nodes limit
	result = g.TraverseBFS(1, nil, 10, 2)
	if len(result) != 2 {
		t.Errorf("BFS maxNodes=2: got %d, want 2", len(result))
	}
}

func TestTraverseDFS(t *testing.T) {
	g := buildTestGraph(t)

	result := g.TraverseDFS(1, nil, 10, 100)
	names := make(map[string]bool)
	for _, n := range result {
		names[n.Name] = true
	}
	if !names["a"] || !names["b"] || !names["c"] || !names["d"] {
		t.Errorf("DFS from a should visit all nodes, got %d nodes", len(result))
	}

	// Max depth 1
	result = g.TraverseDFS(1, nil, 1, 100)
	if len(result) != 3 {
		t.Errorf("DFS depth=1: got %d, want 3", len(result))
	}

	// Max nodes
	result = g.TraverseDFS(1, nil, 10, 2)
	if len(result) != 2 {
		t.Errorf("DFS maxNodes=2: got %d, want 2", len(result))
	}
}

func TestTraverseBFSReverse(t *testing.T) {
	g := buildTestGraph(t)

	// From c: should find c, b, a (reverse traversal via incoming edges)
	result := g.TraverseBFS(3, nil, 10, 100)
	names := make(map[string]bool)
	for _, n := range result {
		names[n.Name] = true
	}
	if !names["c"] || !names["b"] || !names["a"] {
		t.Errorf("BFS from c should find a,b,c via reverse edges, got %v", names)
	}
}

func TestGetImpactRadius(t *testing.T) {
	g := buildTestGraph(t)

	// Impact of b: callers (a) + callees (c) + b itself
	impact := g.GetImpactRadius(2)
	names := make(map[string]bool)
	for _, n := range impact {
		names[n.Name] = true
	}
	if !names["a"] || !names["b"] || !names["c"] {
		t.Errorf("impact of b should include a, b, c; got %v", names)
	}
	if names["d"] {
		t.Error("impact of b should not include d")
	}
}

func TestFindPaths(t *testing.T) {
	g := NewGraph()
	a := &Node{Name: "a", Kind: KindFunc, File: "x.go", Line: 1}
	b := &Node{Name: "b", Kind: KindFunc, File: "x.go", Line: 5}
	c := &Node{Name: "c", Kind: KindFunc, File: "x.go", Line: 10}
	d := &Node{Name: "d", Kind: KindFunc, File: "x.go", Line: 15}
	g.AddNode(a)
	g.AddNode(b)
	g.AddNode(c)
	g.AddNode(d)
	// a → b → d, a → c → d
	g.AddEdge(&Edge{FromID: a.ID, ToID: b.ID, Kind: EdgeCalls})
	g.AddEdge(&Edge{FromID: b.ID, ToID: d.ID, Kind: EdgeCalls})
	g.AddEdge(&Edge{FromID: a.ID, ToID: c.ID, Kind: EdgeCalls})
	g.AddEdge(&Edge{FromID: c.ID, ToID: d.ID, Kind: EdgeCalls})

	paths := g.FindPaths(a.ID, d.ID, 10)
	if len(paths) != 2 {
		t.Errorf("FindPaths(a→d) = %d paths, want 2", len(paths))
	}

	// With maxPaths=1
	paths = g.FindPaths(a.ID, d.ID, 1)
	if len(paths) != 1 {
		t.Errorf("FindPaths(a→d, max=1) = %d paths, want 1", len(paths))
	}

	// No path
	paths = g.FindPaths(d.ID, a.ID, 10)
	if len(paths) != 0 {
		t.Errorf("FindPaths(d→a) = %d paths, want 0", len(paths))
	}
}

func TestGetAncestorsDescendants(t *testing.T) {
	g := NewGraph()
	root := &Node{Name: "root", Kind: KindType, File: "/", Line: 0}
	pkg := &Node{Name: "pkg", Kind: KindType, File: "/pkg/", Line: 0}
	fn := &Node{Name: "fn", Kind: KindFunc, File: "/pkg/fn.go", Line: 1}
	g.AddNode(root)
	g.AddNode(pkg)
	g.AddNode(fn)
	g.AddEdge(&Edge{FromID: root.ID, ToID: pkg.ID, Kind: EdgeContains})
	g.AddEdge(&Edge{FromID: pkg.ID, ToID: fn.ID, Kind: EdgeContains})

	// Ancestors of fn: pkg, root
	ancestors := g.GetAncestors(fn.ID)
	if len(ancestors) != 2 {
		t.Errorf("GetAncestors(fn) = %d, want 2", len(ancestors))
	}

	// Descendants of root: pkg, fn
	descendants := g.GetDescendants(root.ID)
	if len(descendants) != 2 {
		t.Errorf("GetDescendants(root) = %d, want 2", len(descendants))
	}

	// Ancestors of root: none
	ancestors = g.GetAncestors(root.ID)
	if len(ancestors) != 0 {
		t.Errorf("GetAncestors(root) = %d, want 0", len(ancestors))
	}
}

func TestTraverseBFSStartNotInGraph(t *testing.T) {
	g := NewGraph()
	g.AddNode(&Node{ID: 1, Name: "a", Kind: KindFunc, File: "x.go", Line: 1})

	result := g.TraverseBFS(999, nil, 10, 100)
	if len(result) != 0 {
		t.Errorf("BFS from non-existent node: got %d, want 0", len(result))
	}
}
