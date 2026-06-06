package cindex

import (
	"strings"
	"testing"
)

func TestAddNode(t *testing.T) {
	g := NewGraph()

	// Auto-assign ID
	n1 := &Node{Name: "foo", Kind: KindFunc, File: "a.go", Line: 1}
	id1 := g.AddNode(n1)
	if id1 != 1 {
		t.Errorf("AddNode auto ID = %d, want 1", id1)
	}
	if n1.ID != 1 {
		t.Errorf("node.ID = %d, want 1", n1.ID)
	}

	// Second node
	n2 := &Node{Name: "bar", Kind: KindFunc, File: "a.go", Line: 10}
	id2 := g.AddNode(n2)
	if id2 != 2 {
		t.Errorf("AddNode auto ID = %d, want 2", id2)
	}

	// Manual ID
	n3 := &Node{ID: 100, Name: "baz", Kind: KindStruct, File: "b.go", Line: 5}
	id3 := g.AddNode(n3)
	if id3 != 100 {
		t.Errorf("AddNode manual ID = %d, want 100", id3)
	}

	// Next auto ID should be > 100
	n4 := &Node{Name: "qux", Kind: KindClass, File: "c.go", Line: 1}
	id4 := g.AddNode(n4)
	if id4 != 101 {
		t.Errorf("AddNode after manual = %d, want 101", id4)
	}

	if len(g.Nodes) != 4 {
		t.Errorf("len(Nodes) = %d, want 4", len(g.Nodes))
	}
}

func TestAddEdge(t *testing.T) {
	g := NewGraph()
	n1 := &Node{Name: "a", Kind: KindFunc, File: "a.go", Line: 1}
	n2 := &Node{Name: "b", Kind: KindFunc, File: "a.go", Line: 5}
	g.AddNode(n1)
	g.AddNode(n2)

	e1 := &Edge{FromID: n1.ID, ToID: n2.ID, Kind: EdgeCalls}
	id1 := g.AddEdge(e1)
	if id1 != 1 {
		t.Errorf("AddEdge auto ID = %d, want 1", id1)
	}

	// Manual ID
	e2 := &Edge{ID: 50, FromID: n1.ID, ToID: n2.ID, Kind: EdgeImports}
	id2 := g.AddEdge(e2)
	if id2 != 50 {
		t.Errorf("AddEdge manual ID = %d, want 50", id2)
	}

	// Next auto ID should be > 50
	e3 := &Edge{FromID: n2.ID, ToID: n1.ID, Kind: EdgeCalls}
	id3 := g.AddEdge(e3)
	if id3 != 51 {
		t.Errorf("AddEdge after manual = %d, want 51", id3)
	}
}

func TestRemoveNode(t *testing.T) {
	g := NewGraph()
	n1 := &Node{Name: "a", Kind: KindFunc, File: "a.go", Line: 1}
	n2 := &Node{Name: "b", Kind: KindFunc, File: "a.go", Line: 5}
	n3 := &Node{Name: "c", Kind: KindFunc, File: "b.go", Line: 1}
	g.AddNode(n1)
	g.AddNode(n2)
	g.AddNode(n3)

	e1 := &Edge{FromID: n1.ID, ToID: n2.ID, Kind: EdgeCalls}
	e2 := &Edge{FromID: n2.ID, ToID: n3.ID, Kind: EdgeCalls}
	e3 := &Edge{FromID: n1.ID, ToID: n3.ID, Kind: EdgeImports}
	g.AddEdge(e1)
	g.AddEdge(e2)
	g.AddEdge(e3)

	g.RemoveNode(n2.ID)

	if _, ok := g.Nodes[n2.ID]; ok {
		t.Error("node should be removed from Nodes map")
	}
	if len(g.Edges) != 1 {
		t.Errorf("len(Edges) = %d, want 1 (only n1→n3 import should remain)", len(g.Edges))
	}
	if len(g.edgesByFrom[n1.ID]) != 1 {
		t.Errorf("edgesByFrom[n1] = %d, want 1", len(g.edgesByFrom[n1.ID]))
	}
	if len(g.edgesByTo[n3.ID]) != 1 {
		t.Errorf("edgesByTo[n3] = %d, want 1", len(g.edgesByTo[n3.ID]))
	}

	// Removing non-existent node should be a no-op
	g.RemoveNode(9999)
}

func TestRemoveFileNodes(t *testing.T) {
	g := NewGraph()
	n1 := &Node{Name: "a", Kind: KindFunc, File: "a.go", Line: 1}
	n2 := &Node{Name: "b", Kind: KindFunc, File: "a.go", Line: 5}
	n3 := &Node{Name: "c", Kind: KindFunc, File: "b.go", Line: 1}
	g.AddNode(n1)
	g.AddNode(n2)
	g.AddNode(n3)

	g.AddEdge(&Edge{FromID: n1.ID, ToID: n2.ID, Kind: EdgeCalls})
	g.AddEdge(&Edge{FromID: n1.ID, ToID: n3.ID, Kind: EdgeCalls})

	g.RemoveFileNodes("a.go")

	if len(g.Nodes) != 1 {
		t.Errorf("len(Nodes) = %d, want 1", len(g.Nodes))
	}
	if _, ok := g.Nodes[n3.ID]; !ok {
		t.Error("node from b.go should still exist")
	}
	// Both edges are removed: n1→n2 (both in a.go), n1→n3 (n1 in a.go)
	if len(g.Edges) != 0 {
		t.Errorf("len(Edges) = %d, want 0 (n1 was in a.go, so all its edges are removed)", len(g.Edges))
	}
	if _, ok := g.files["a.go"]; ok {
		t.Error("file entry should be removed")
	}
}

func TestFindNodesByName(t *testing.T) {
	g := NewGraph()
	g.AddNode(&Node{Name: "handleRequest", Kind: KindFunc, File: "a.go", Line: 1})
	g.AddNode(&Node{Name: "handleRequestAsync", Kind: KindFunc, File: "a.go", Line: 10})
	g.AddNode(&Node{Name: "processData", Kind: KindFunc, File: "b.go", Line: 1})

	// Partial match — "handleRequest" matches both handleRequest and handleRequestAsync
	nodes := g.FindNodesByName("handleRequest")
	if len(nodes) != 2 {
		t.Errorf("FindNodesByName('handleRequest'): got %d nodes, want 2", len(nodes))
	}

	// Partial match
	nodes = g.FindNodesByName("handle")
	if len(nodes) != 2 {
		t.Errorf("partial match 'handle': got %d nodes, want 2", len(nodes))
	}

	// Case-insensitive partial
	nodes = g.FindNodesByName("REQUEST")
	if len(nodes) != 2 {
		t.Errorf("partial match 'REQUEST': got %d nodes, want 2", len(nodes))
	}

	// No match
	nodes = g.FindNodesByName("nonexistent")
	if len(nodes) != 0 {
		t.Errorf("no match: got %d nodes, want 0", len(nodes))
	}
}

func TestFindNodesByFile(t *testing.T) {
	g := NewGraph()
	g.AddNode(&Node{Name: "a", Kind: KindFunc, File: "/home/user/proj/cmd/main.go", Line: 1})
	g.AddNode(&Node{Name: "b", Kind: KindFunc, File: "/home/user/proj/pkg/handler.go", Line: 1})
	g.AddNode(&Node{Name: "c", Kind: KindFunc, File: "/home/user/proj/pkg/util.go", Line: 1})

	nodes := g.FindNodesByFile("handler.go")
	if len(nodes) != 1 {
		t.Errorf("FindNodesByFile('handler.go') = %d, want 1", len(nodes))
	}

	nodes = g.FindNodesByFile("pkg/")
	if len(nodes) != 2 {
		t.Errorf("FindNodesByFile('pkg/') = %d, want 2", len(nodes))
	}
}

func TestGetCallersCallees(t *testing.T) {
	g := NewGraph()
	a := &Node{Name: "a", Kind: KindFunc, File: "x.go", Line: 1}
	b := &Node{Name: "b", Kind: KindFunc, File: "x.go", Line: 5}
	c := &Node{Name: "c", Kind: KindFunc, File: "x.go", Line: 10}
	g.AddNode(a)
	g.AddNode(b)
	g.AddNode(c)

	// a calls b, a calls c
	g.AddEdge(&Edge{FromID: a.ID, ToID: b.ID, Kind: EdgeCalls})
	g.AddEdge(&Edge{FromID: a.ID, ToID: c.ID, Kind: EdgeCalls})
	// b calls c
	g.AddEdge(&Edge{FromID: b.ID, ToID: c.ID, Kind: EdgeCalls})

	// c's callers: a, b
	callers := g.GetCallers(c.ID)
	if len(callers) != 2 {
		t.Errorf("GetCallers(c) = %d, want 2", len(callers))
	}

	// a's callees: b, c
	callees := g.GetCallees(a.ID)
	if len(callees) != 2 {
		t.Errorf("GetCallees(a) = %d, want 2", len(callees))
	}

	// a's callers: none
	callers = g.GetCallers(a.ID)
	if len(callers) != 0 {
		t.Errorf("GetCallers(a) = %d, want 0", len(callers))
	}
}

func TestGetChildrenParent(t *testing.T) {
	g := NewGraph()
	pkg := &Node{Name: "pkg", Kind: KindType, File: "pkg/", Line: 0}
	fn := &Node{Name: "fn", Kind: KindFunc, File: "pkg/fn.go", Line: 1}
	g.AddNode(pkg)
	g.AddNode(fn)
	g.AddEdge(&Edge{FromID: pkg.ID, ToID: fn.ID, Kind: EdgeContains})

	children := g.GetChildren(pkg.ID)
	if len(children) != 1 || children[0].ID != fn.ID {
		t.Errorf("GetChildren(pkg) should return fn")
	}

	parent := g.GetParent(fn.ID)
	if parent == nil || parent.ID != pkg.ID {
		t.Errorf("GetParent(fn) should return pkg")
	}

	if g.GetParent(pkg.ID) != nil {
		t.Error("GetParent(pkg) should return nil")
	}
}

func TestQueryDeps(t *testing.T) {
	g := NewGraph()
	n1 := &Node{Name: "main", Kind: KindFunc, File: "main.go", Line: 1, PkgPath: "main"}
	n2 := &Node{Name: "handler", Kind: KindFunc, File: "handler.go", Line: 1, PkgPath: "handler"}
	n3 := &Node{Name: "util", Kind: KindFunc, File: "util.go", Line: 1, PkgPath: "util"}
	g.AddNode(n1)
	g.AddNode(n2)
	g.AddNode(n3)

	g.AddEdge(&Edge{FromID: n1.ID, ToID: n2.ID, Kind: EdgeImports})
	g.AddEdge(&Edge{FromID: n1.ID, ToID: n3.ID, Kind: EdgeImports})
	g.AddEdge(&Edge{FromID: n2.ID, ToID: n3.ID, Kind: EdgeImports})

	deps := g.QueryDeps("main")
	if len(deps) != 2 {
		t.Errorf("QueryDeps('main') = %d deps, want 2", len(deps))
	}

	deps = g.QueryDeps("handler")
	if len(deps) != 1 {
		t.Errorf("QueryDeps('handler') = %d deps, want 1", len(deps))
	}

	deps = g.QueryDeps("nonexistent")
	if deps != nil {
		t.Errorf("QueryDeps('nonexistent') = %v, want nil", deps)
	}
}

func TestQueryFile(t *testing.T) {
	g := NewGraph()
	g.AddNode(&Node{Name: "a", Kind: KindFunc, File: "/proj/cmd/main.go", Line: 1})
	g.AddNode(&Node{Name: "b", Kind: KindFunc, File: "/proj/pkg/handler.go", Line: 1})

	files := g.QueryFile("handler.go")
	if len(files) != 1 || files[0].Path != "/proj/pkg/handler.go" {
		t.Errorf("QueryFile('handler.go') = %v", files)
	}

	files = g.QueryFile("nonexistent")
	if len(files) != 0 {
		t.Errorf("QueryFile('nonexistent') = %d, want 0", len(files))
	}
}

func TestFormatSkeleton(t *testing.T) {
	g := NewGraph()
	g.AddNode(&Node{Name: "main", Kind: KindFunc, File: "/proj/main.go", Line: 1, PkgPath: "main"})
	g.AddNode(&Node{Name: "handler", Kind: KindFunc, File: "/proj/handler.go", Line: 1, PkgPath: "main"})
	g.AddEdge(&Edge{FromID: 1, ToID: 2, Kind: EdgeImports})

	s := g.FormatSkeleton("/proj")
	if !strings.Contains(s, "<project>") {
		t.Error("skeleton should contain <project>")
	}
	if !strings.Contains(s, "<language>go</language>") {
		t.Error("skeleton should detect Go language")
	}
	if !strings.Contains(s, "2 files") {
		t.Errorf("skeleton should report 2 files, got: %s", s)
	}
	if !strings.Contains(s, "2 symbols") {
		t.Errorf("skeleton should report 2 symbols, got: %s", s)
	}

	// Empty graph
	empty := NewGraph()
	if empty.FormatSkeleton("") != "" {
		t.Error("empty graph should return empty skeleton")
	}
}

func TestFormatSkeletonDirTree(t *testing.T) {
	g := NewGraph()
	g.AddNode(&Node{Name: "a", Kind: KindFunc, File: "/proj/cmd/server/main.go", Line: 1})
	g.AddNode(&Node{Name: "b", Kind: KindFunc, File: "/proj/internal/handler/handler.go", Line: 1})
	g.AddNode(&Node{Name: "c", Kind: KindFunc, File: "/proj/internal/util/util.go", Line: 1})

	s := g.FormatSkeleton("/proj")
	if !strings.Contains(s, "cmd") {
		t.Errorf("skeleton should contain 'cmd' dir, got: %s", s)
	}
	if !strings.Contains(s, "internal") {
		t.Errorf("skeleton should contain 'internal' dir, got: %s", s)
	}
}
