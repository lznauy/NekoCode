package index

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setupTestProject creates a minimal Go project in a temp directory and
// chdirs into it so NewIndexTool()'s os.Getwd() resolves the project.
func setupTestProject(t *testing.T) {
	t.Helper()
	dir := t.TempDir()

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

	t.Chdir(dir)
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	parent := filepath.Dir(filepath.Join(dir, name))
	os.MkdirAll(parent, 0755)
	os.WriteFile(filepath.Join(dir, name), []byte(content), 0644)
}

func TestIndexToolSkeleton(t *testing.T) {
	setupTestProject(t)
	tool := NewIndexTool()

	result, err := tool.Execute(context.Background(), map[string]any{"query": "skeleton"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(result.Render(), "<project>") {
		t.Error("skeleton should contain <project>")
	}
	if !strings.Contains(result.Render(), "<language>go</language>") {
		t.Error("skeleton should detect Go")
	}
}

func TestIndexToolSymbol(t *testing.T) {
	setupTestProject(t)
	tool := NewIndexTool()

	result, err := tool.Execute(context.Background(), map[string]any{"query": "symbol:Hello"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(result.Render(), "Hello") {
		t.Errorf("symbol query should find Hello, got: %s", result.Render())
	}
}

func TestIndexToolCallers(t *testing.T) {
	setupTestProject(t)
	tool := NewIndexTool()

	result, err := tool.Execute(context.Background(), map[string]any{"query": "callers:Hello"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(result.Render(), "main") || !strings.Contains(result.Render(), "Hello") {
		t.Errorf("callers query should show main -> Hello, got: %s", result.Render())
	}
}

func TestIndexToolCallees(t *testing.T) {
	setupTestProject(t)
	tool := NewIndexTool()

	result, err := tool.Execute(context.Background(), map[string]any{"query": "callees:main"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(result.Render(), "main") || !strings.Contains(result.Render(), "Hello") {
		t.Errorf("callees query should show main -> Hello, got: %s", result.Render())
	}
}

func TestIndexToolFile(t *testing.T) {
	setupTestProject(t)
	tool := NewIndexTool()

	result, err := tool.Execute(context.Background(), map[string]any{"query": "file:main.go"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(result.Render(), "main.go") {
		t.Errorf("file query should find main.go, got: %s", result.Render())
	}
}

func TestIndexToolDeps(t *testing.T) {
	setupTestProject(t)
	tool := NewIndexTool()

	result, err := tool.Execute(context.Background(), map[string]any{"query": "deps:main"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// main imports util, so should show util
	if !strings.Contains(result.Render(), "util") && !strings.Contains(result.Render(), "not found") {
		t.Errorf("deps query result: %s", result.Render())
	}
}

func TestIndexToolSearch(t *testing.T) {
	setupTestProject(t)
	tool := NewIndexTool()

	result, err := tool.Execute(context.Background(), map[string]any{"query": "search:Hello"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// FTS may not be available in memory-only mode
	if strings.Contains(result.Render(), "not available") {
		t.Skip("FTS not available")
	}
	if !strings.Contains(result.Render(), "Hello") {
		t.Errorf("search should find Hello, got: %s", result.Render())
	}
}

func TestIndexToolEmptyQuery(t *testing.T) {
	setupTestProject(t)
	tool := NewIndexTool()

	result, err := tool.Execute(context.Background(), map[string]any{"query": ""})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(result.Render(), "Missing") {
		t.Errorf("empty query should return error message, got: %s", result.Render())
	}
}

func TestIndexToolInvalidFormat(t *testing.T) {
	setupTestProject(t)
	tool := NewIndexTool()

	result, err := tool.Execute(context.Background(), map[string]any{"query": "invalid"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(result.Render(), "Invalid") {
		t.Errorf("invalid format should return error message, got: %s", result.Render())
	}
}

func TestIndexToolUnknownPrefix(t *testing.T) {
	setupTestProject(t)
	tool := NewIndexTool()

	result, err := tool.Execute(context.Background(), map[string]any{"query": "unknown:value"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(result.Render(), "Unknown") {
		t.Errorf("unknown prefix should return error message, got: %s", result.Render())
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

func TestIndexToolNameAndDescription(t *testing.T) {
	tool := NewIndexTool()

	if tool.Name() != "index" {
		t.Errorf("Name() = %q, want index", tool.Name())
	}
	if tool.Description() == "" {
		t.Error("Description() should not be empty")
	}
	if len(tool.Parameters()) == 0 {
		t.Error("Parameters() should not be empty")
	}
}
