package projctx

import (
	"strings"
	"testing"
)

func sampleIndex() *ProjectIndex {
	return &ProjectIndex{
		Language: "go",
		Module:   "example.com/app",
		Packages: []PkgInfo{
			{Name: "main", Path: "example.com/app", Dir: "/app", Files: []string{"main.go"}},
			{Name: "agent", Path: "example.com/app/agent", Dir: "/app/agent", Files: []string{"agent.go", "run.go"}},
		},
		Files: []FileInfo{
			{Path: "/app/main.go", Package: "example.com/app", Lines: 50},
			{Path: "/app/agent/agent.go", Package: "example.com/app/agent", Lines: 200},
			{Path: "/app/agent/run.go", Package: "example.com/app/agent", Lines: 150},
		},
		Symbols: []SymbolInfo{
			{Name: "Agent", Kind: "struct", File: "/app/agent/agent.go", Line: 15, PkgPath: "example.com/app/agent"},
			{Name: "Run", Kind: "func", File: "/app/agent/run.go", Line: 30, PkgPath: "example.com/app/agent"},
			{Name: "NewAgent", Kind: "func", File: "/app/agent/agent.go", Line: 40, PkgPath: "example.com/app/agent"},
		},
		Deps: map[string][]string{
			"example.com/app":       {"example.com/app/agent"},
			"example.com/app/agent": {"fmt", "context"},
		},
	}
}

func TestQuerySymbol(t *testing.T) {
	idx := sampleIndex()
	r := idx.QuerySymbol("Agent")
	if len(r) != 1 || r[0].Name != "Agent" {
		t.Errorf("exact match failed: %+v", r)
	}

	r = idx.QuerySymbol("age") // partial match
	if len(r) == 0 {
		t.Error("partial match failed")
	}

	r = idx.QuerySymbol("Nope")
	if len(r) != 0 {
		t.Error("should be empty")
	}
}

func TestQueryDeps(t *testing.T) {
	idx := sampleIndex()
	deps := idx.QueryDeps("example.com/app")
	if len(deps) != 1 || deps[0] != "example.com/app/agent" {
		t.Errorf("deps = %v", deps)
	}
}

func TestQueryFile(t *testing.T) {
	idx := sampleIndex()
	r := idx.QueryFile("agent")
	if len(r) != 2 {
		t.Errorf("expected 2 files, got %d", len(r))
	}
}

func TestFormatSkeleton(t *testing.T) {
	idx := sampleIndex()
	s := idx.FormatSkeleton()
	if !strings.Contains(s, "<project>") {
		t.Error("missing project tag")
	}
	if !strings.Contains(s, "example.com/app") {
		t.Error("missing module")
	}
	if !strings.Contains(s, "<deps>") {
		t.Error("missing deps")
	}
}

func TestEstimateLines(t *testing.T) {
	if n := estimateLines(400); n != 10 {
		t.Errorf("estimateLines(400) = %d, want 10", n)
	}
	if n := estimateLines(10); n != 1 {
		t.Errorf("estimateLines(10) = %d, want 1", n)
	}
}

func TestIsStdlib(t *testing.T) {
	if !isStdlib("fmt") {
		t.Error("fmt is stdlib")
	}
	if !isStdlib("net/http") {
		t.Error("net/http is stdlib")
	}
	if isStdlib("github.com/foo/bar") {
		t.Error("github.com is not stdlib")
	}
}
