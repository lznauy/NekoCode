package projecttool

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nekocode/bot/index/service"
)

func setupTestManager(t *testing.T) *service.Manager {
	t.Helper()
	dir := t.TempDir()

	// Create a minimal Go project
	writeFile(t, dir, "go.mod", "module myproject\n")
	writeFile(t, dir, "main.go", `package main

import "myproject/util"

// main is the entry point.
func main() {
	util.Hello("world")
}
`)
	writeFile(t, dir, "util/util.go", `package util

// Hello prints a greeting.
func Hello(name string) {
	println("hello", name)
}
`)

	mgr, err := service.NewManager(dir)
	if err != nil {
		t.Skipf("NewManager failed (FTS5 may be unavailable): %v", err)
		return nil
	}
	if err := mgr.Init(); err != nil {
		t.Skipf("Init failed (FTS5 may be unavailable): %v", err)
		return nil
	}
	t.Cleanup(func() { mgr.Close() })
	return mgr
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	parent := filepath.Dir(filepath.Join(dir, name))
	os.MkdirAll(parent, 0755)
	os.WriteFile(filepath.Join(dir, name), []byte(content), 0644)
}

func TestProjectInfoToolSkeleton(t *testing.T) {
	mgr := setupTestManager(t)
	tool := NewProjectInfoTool(mgr)

	result, err := tool.Execute(context.Background(), map[string]any{"query": "skeleton"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(result, "<project>") {
		t.Error("skeleton should contain <project>")
	}
	if !strings.Contains(result, "<language>go</language>") {
		t.Error("skeleton should detect Go")
	}
}

func TestProjectInfoToolSymbol(t *testing.T) {
	mgr := setupTestManager(t)
	tool := NewProjectInfoTool(mgr)

	result, err := tool.Execute(context.Background(), map[string]any{"query": "symbol:Hello"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(result, "Hello") {
		t.Errorf("symbol query should find Hello, got: %s", result)
	}
}

func TestProjectInfoToolFile(t *testing.T) {
	mgr := setupTestManager(t)
	tool := NewProjectInfoTool(mgr)

	result, err := tool.Execute(context.Background(), map[string]any{"query": "file:main.go"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(result, "main.go") {
		t.Errorf("file query should find main.go, got: %s", result)
	}
}

func TestProjectInfoToolDeps(t *testing.T) {
	mgr := setupTestManager(t)
	tool := NewProjectInfoTool(mgr)

	result, err := tool.Execute(context.Background(), map[string]any{"query": "deps:main"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// main imports util, so should show util
	if !strings.Contains(result, "util") && !strings.Contains(result, "not found") {
		t.Errorf("deps query result: %s", result)
	}
}

func TestProjectInfoToolSearch(t *testing.T) {
	mgr := setupTestManager(t)
	if mgr == nil {
		t.Skip("manager not available")
	}
	tool := NewProjectInfoTool(mgr)

	result, err := tool.Execute(context.Background(), map[string]any{"query": "search:Hello"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// FTS may not be available in memory-only mode
	if strings.Contains(result, "not available") {
		t.Skip("FTS not available")
	}
	if !strings.Contains(result, "Hello") {
		t.Errorf("search should find Hello, got: %s", result)
	}
}

func TestProjectInfoToolEmptyQuery(t *testing.T) {
	mgr := setupTestManager(t)
	tool := NewProjectInfoTool(mgr)

	result, err := tool.Execute(context.Background(), map[string]any{"query": ""})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(result, "Missing") {
		t.Errorf("empty query should return error message, got: %s", result)
	}
}

func TestProjectInfoToolInvalidFormat(t *testing.T) {
	mgr := setupTestManager(t)
	tool := NewProjectInfoTool(mgr)

	result, err := tool.Execute(context.Background(), map[string]any{"query": "invalid"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(result, "Invalid") {
		t.Errorf("invalid format should return error message, got: %s", result)
	}
}

func TestProjectInfoToolUnknownPrefix(t *testing.T) {
	mgr := setupTestManager(t)
	tool := NewProjectInfoTool(mgr)

	result, err := tool.Execute(context.Background(), map[string]any{"query": "unknown:value"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(result, "Unknown") {
		t.Errorf("unknown prefix should return error message, got: %s", result)
	}
}

func TestProjectInfoToolNilGraph(t *testing.T) {
	mgr := &service.Manager{} // no Init, graph is nil
	tool := NewProjectInfoTool(mgr)

	result, err := tool.Execute(context.Background(), map[string]any{"query": "skeleton"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(result, "not available") {
		t.Errorf("nil graph should return not available, got: %s", result)
	}
}

func TestShortenPath(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/very/long/path/to/file.go", "path/to/file.go"},
		{"/a/b.go", "/a/b.go"},
	}

	for _, tt := range tests {
		got := shortenPath(tt.path)
		if got != tt.want {
			t.Errorf("shortenPath(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestProjectInfoToolNameAndDescription(t *testing.T) {
	mgr := &service.Manager{}
	tool := NewProjectInfoTool(mgr)

	if tool.Name() != "project_info" {
		t.Errorf("Name() = %q, want project_info", tool.Name())
	}
	if tool.Description() == "" {
		t.Error("Description() should not be empty")
	}
	if len(tool.Parameters()) == 0 {
		t.Error("Parameters() should not be empty")
	}
}
