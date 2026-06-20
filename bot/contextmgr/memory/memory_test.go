package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoad_Nonexistent(t *testing.T) {
	f, err := Load("/tmp/nekocode_nonexistent_memory.md")
	if err != nil {
		t.Fatalf("Load should not error for missing file: %v", err)
	}
	if f == nil {
		t.Fatal("Load should return empty File")
	}
}

func TestSaveAndLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.md")
	f := &File{path: path, TechStack: "- Go\n- Python"}
	if err := f.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	f2, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !strings.Contains(f2.TechStack, "Go") {
		t.Errorf("TechStack not preserved: %s", f2.TechStack)
	}
}

func TestBuild_Empty(t *testing.T) {
	f := &File{path: "/tmp/test.md"}
	if b := f.Build(); b != "" {
		t.Errorf("empty memory should produce empty Build, got: %s", b)
	}
}

func TestBuild_WithContent(t *testing.T) {
	f := &File{path: "/tmp/test.md", TechStack: "- Go"}
	b := f.Build()
	if !strings.Contains(b, "Tech Stack") || !strings.Contains(b, "Go") {
		t.Errorf("Build missing content: %s", b)
	}
}

func TestAppend(t *testing.T) {
	f := &File{path: "/tmp/test.md"}
	if err := f.Append("goals", "finish project"); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if !strings.Contains(f.ActiveGoals, "finish project") {
		t.Errorf("goal not appended: %s", f.ActiveGoals)
	}
	// Append again.
	if err := f.Append("goals", "write tests"); err != nil {
		t.Fatalf("second Append: %v", err)
	}
	if !strings.Contains(f.ActiveGoals, "write tests") {
		t.Errorf("second goal not appended: %s", f.ActiveGoals)
	}
}

func TestAppend_InvalidSection(t *testing.T) {
	f := &File{path: "/tmp/test.md"}
	if err := f.Append("nonexistent", "content"); err == nil {
		t.Error("invalid section should return error")
	}
}

func TestMergeFromCompaction(t *testing.T) {
	f := &File{path: "/tmp/test.md"}
	f.MergeFromCompaction([]string{"pkg/auth → handles login"}, "implement oauth")
	if !strings.Contains(f.ActiveGoals, "implement oauth") {
		t.Errorf("goal not set: %s", f.ActiveGoals)
	}
	if !strings.Contains(f.ArchMap, "pkg/auth") {
		t.Errorf("fact not added: %s", f.ArchMap)
	}
	// Duplicate should not be added.
	f.MergeFromCompaction([]string{"pkg/auth → handles login"}, "")
	if strings.Count(f.ArchMap, "pkg/auth") > 1 {
		t.Error("duplicate fact should not be added")
	}
}

func TestSave_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mem2.md")
	f := &File{path: path, TechStack: "- Rust", ActiveGoals: "- ship it", Preferences: "- use tabs"}
	f.Save()
	f2, _ := Load(path)
	if !strings.Contains(f2.TechStack, "Rust") {
		t.Errorf("TechStack not preserved: %q", f2.TechStack)
	}
	if !strings.Contains(f2.ActiveGoals, "ship it") {
		t.Errorf("ActiveGoals not preserved: %q", f2.ActiveGoals)
	}
	if !strings.Contains(f2.Preferences, "use tabs") {
		t.Errorf("Preferences not preserved: %q", f2.Preferences)
	}
}

func TestDefaultPath(t *testing.T) {
	p := DefaultPath()
	if !strings.Contains(p, ".nekocode") {
		t.Errorf("unexpected default path: %s", p)
	}
}

func TestNewFile_ParseComplex(t *testing.T) {
	content := `## Tech Stack
- Go
- React

## Active Goals
- release v1

## Completed Tasks
- setup ci
- add tests

## Key Architecture Map
- pkg/auth → auth module

## User Preferences
- use two spaces
`
	// Simulate parsing by saving and loading.
	path := filepath.Join(t.TempDir(), "mem3.md")
	os.WriteFile(path, []byte(content), 0644)
	f, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !strings.Contains(f.TechStack, "Go") {
		t.Errorf("TechStack: %s", f.TechStack)
	}
	if !strings.Contains(f.CompletedTasks, "setup ci") {
		t.Errorf("CompletedTasks: %s", f.CompletedTasks)
	}
	if !strings.Contains(f.Preferences, "two spaces") {
		t.Errorf("Preferences: %s", f.Preferences)
	}
}
