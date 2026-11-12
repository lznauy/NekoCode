package session

import (
	"bytes"
	"context"
	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/provider/types"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeleteSubagentRejectsActiveAndKeepsSiblingOutputs(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	child, err := OpenSubagent("parent", "child", "/project", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := child.Begin("executor", "task", nil); err != nil {
		t.Fatal(err)
	}
	if err := child.Checkpoint(ctxmgr.ManagerSnapshot{}, "running", ""); err != nil {
		t.Fatal(err)
	}
	if err := DeleteSubagent("parent", "child"); err == nil {
		t.Fatal("deleted a live task")
	}
	child.Close()
	outputs, _ := ToolOutputDir("parent")
	own, sibling := filepath.Join(outputs, "subagents", "child", "a"), filepath.Join(outputs, "subagents", "sibling", "a")
	for _, path := range []string{own, sibling} {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("keep"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := DeleteSubagent("parent", "child"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(own); !os.IsNotExist(err) {
		t.Fatal("task outputs not removed", err)
	}
	if _, err := os.Stat(sibling); err != nil {
		t.Fatal("sibling output removed", err)
	}
	if _, err := OpenSubagent("parent", "child", "/project", false); err == nil {
		t.Fatal("deleted identity was reused")
	}
	if err := DeleteSubagent("parent", "child"); err != nil {
		t.Fatal("cleanup not idempotent", err)
	}
	if !SubagentDeleted("parent", "child") {
		t.Fatal("deletion marker missing")
	}
}
func TestSearchAndExportTaskOutputsAcrossPageBoundary(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	outputs, _ := ToolOutputDir("parent")
	path := filepath.Join(outputs, "subagents", "child", "output.txt")
	content := strings.Repeat("x", SubagentOutputBytes-3) + "needle" + strings.Repeat(" tail", 20000)
	seedToolArtifact(t, path, content)
	page, err := SearchSubagentOutputs(t.Context(), "parent", "child", "needle", 0, 20)
	if err != nil || len(page.Hits) != 1 || page.Hits[0].Sequence != 2 || !strings.Contains(page.Hits[0].Preview, "needle") {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	var out bytes.Buffer
	if err := ExportSubagentOutputs(t.Context(), "parent", "child", &out); err != nil {
		t.Fatal(err)
	}
	if out.String() != "\n[Tool output 2]\n"+content {
		t.Fatal("export lost content")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := SearchSubagentOutputs(t.Context(), "parent", "child", "needle", 0, 20); err == nil {
		t.Fatal("missing artifact returned empty successful search")
	}
}
func TestSearchTaskOutputsPaginationAndCancellation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	seedObservedSubagent(t, []types.Message{{Role: "user", Content: "task"}, {Role: "tool", Content: "hit one"}, {Role: "tool", Content: "hit two"}})
	first, err := SearchSubagentOutputs(t.Context(), "parent", "child", "hit", 0, 1)
	if err != nil || len(first.Hits) != 1 || !first.HasMore {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	second, err := SearchSubagentOutputs(t.Context(), "parent", "child", "hit", first.Next, 1)
	if err != nil || len(second.Hits) != 1 || second.Hits[0].Sequence <= first.Next || second.HasMore {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := SearchSubagentOutputs(ctx, "parent", "child", "hit", 0, 20); err == nil {
		t.Fatal("cancelled search succeeded")
	}
}

func TestSearchErrorDoesNotReturnPartialResultsOrCursor(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	seedObservedSubagent(t, []types.Message{{Role: "user", Content: "task"}, {Role: "tool", Content: "hit one"}, {Role: "tool", Content: "hit two"}})
	root, err := subagentRoot("parent")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "child", transcriptFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(data, []byte{'\n'})
	if len(lines) < 3 {
		t.Fatal("missing test transcript")
	}
	lines[2] = []byte("invalid record")
	if err = os.WriteFile(path, bytes.Join(lines, []byte{'\n'}), 0600); err != nil {
		t.Fatal(err)
	}
	page, err := SearchSubagentOutputs(t.Context(), "parent", "child", "hit", 0, 20)
	if err == nil {
		t.Fatal("corrupted committed record accepted")
	}
	if len(page.Hits) != 0 || page.Next != 0 || page.Total != 0 || page.HasMore {
		t.Fatalf("failed search returned usable half-page: %+v", page)
	}
}
