package diff

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestToolReadsPathSources(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "old.txt")
	newPath := filepath.Join(dir, "new.txt")
	if err := os.WriteFile(oldPath, []byte("one\ntwo\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newPath, []byte("one\nTWO\n"), 0644); err != nil {
		t.Fatal(err)
	}

	out, err := NewTool().Execute(context.Background(), map[string]any{
		"old":     "path:" + oldPath,
		"new":     "path:" + newPath,
		"path":    "sample.txt",
		"context": -3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "[sample.txt#diff]") {
		t.Fatalf("missing header in %q", out)
	}
	if !strings.Contains(out, "-2:two") || !strings.Contains(out, "+2:TWO") {
		t.Fatalf("missing changed lines in %q", out)
	}
}

func TestComputeDiffUsesSharedContext(t *testing.T) {
	out := RenderHunks(ComputeDiff("one\ntwo\nthree\n", "one\nTWO\nthree\n", 1))
	for _, want := range []string{" 1:one", "-2:two", "+2:TWO", " 3:three"} {
		if !strings.Contains(out, want) {
			t.Fatalf("diff missing %q:\n%s", want, out)
		}
	}
}

func TestComputeDiffFindsCommonTail(t *testing.T) {
	out := RenderHunks(ComputeDiff("a\nb\nc\n", "a\nx\nb\nc\n", 1))
	if !strings.Contains(out, "+2:x") {
		t.Fatalf("diff missing insertion:\n%s", out)
	}
	if strings.Contains(out, "-2:b") || strings.Contains(out, "-3:c") {
		t.Fatalf("diff should not delete common tail:\n%s", out)
	}
}

func TestRenderTextChangeHandlesHeaderAndNoChange(t *testing.T) {
	out := RenderTextChange("old\n", "new\n", TextChangeOptions{
		Context:      DefaultContext,
		Header:       ToolHeader("write", "file.txt"),
		NoChangeText: NoChanges,
	})
	if !strings.HasPrefix(out, "[write file.txt]\n") {
		t.Fatalf("missing header:\n%s", out)
	}
	if !strings.Contains(out, "-1:old") || !strings.Contains(out, "+1:new") {
		t.Fatalf("missing diff body:\n%s", out)
	}

	out = RenderTextChange("same\n", "same\n", TextChangeOptions{NoChangeText: NoChanges})
	if out != NoChanges {
		t.Fatalf("no-change output = %q", out)
	}
}

func TestPreviewHeaders(t *testing.T) {
	if got := TagHeader("file.txt", "abc"); got != "[file.txt#abc]" {
		t.Fatalf("TagHeader() = %q", got)
	}
	if got := TagHeader("file.txt", ""); got != "[file.txt#]" {
		t.Fatalf("TagHeader() with empty tag = %q", got)
	}
	if got := ToolHeader("write", "file.txt"); got != "[write file.txt]" {
		t.Fatalf("ToolHeader() = %q", got)
	}
}
