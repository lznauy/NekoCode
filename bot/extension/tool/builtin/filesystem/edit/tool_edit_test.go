package edit

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nekocode/bot/extension/tool/runtime/workspace"
)

func TestEditV2ExactUniqueReplacement(t *testing.T) {
	td := t.TempDir()
	p := filepath.Join(td, "file.txt")
	writeFile(t, p, "one\ntwo\nthree\n")

	result, err := (&EditTool{}).Execute(editTestContext(td), map[string]any{
		"path":      p,
		"oldString": "two",
		"newString": "TWO",
	})
	if err != nil {
		t.Fatalf("edit failed: %v\n%s", err, result.Render())
	}
	if got, want := readFile(t, p), "one\nTWO\nthree\n"; got != want {
		t.Fatalf("unexpected content:\n%s", got)
	}
	if !strings.Contains(result.Render(), "-2:two") || !strings.Contains(result.Render(), "+2:TWO") {
		t.Fatalf("expected diff in result, got:\n%s", result.Render())
	}
}

func TestEditV2RejectsAmbiguousOldString(t *testing.T) {
	td := t.TempDir()
	p := filepath.Join(td, "file.txt")
	writeFile(t, p, "x\ntarget\ny\ntarget\n")

	_, err := (&EditTool{}).Execute(editTestContext(td), map[string]any{
		"path":      p,
		"oldString": "target",
		"newString": "changed",
	})
	if err == nil || !strings.Contains(err.Error(), "matched 2 times") || !strings.Contains(err.Error(), "line 2") || !strings.Contains(err.Error(), "line 4") {
		t.Fatalf("expected ambiguous match error, got %v", err)
	}
}

func TestEditV2ReplaceAllExact(t *testing.T) {
	td := t.TempDir()
	p := filepath.Join(td, "file.txt")
	writeFile(t, p, "a foo\nb foo\n")

	if _, err := (&EditTool{}).Execute(editTestContext(td), map[string]any{
		"path":       p,
		"oldString":  "foo",
		"newString":  "bar",
		"replaceAll": true,
	}); err != nil {
		t.Fatalf("edit failed: %v", err)
	}
	if got, want := readFile(t, p), "a bar\nb bar\n"; got != want {
		t.Fatalf("unexpected content:\n%s", got)
	}
	preview := (&EditTool{}).PreviewContext(editTestContext(td), map[string]any{
		"path":       p,
		"oldString":  "bar",
		"newString":  "baz",
		"replaceAll": true,
	})
	if !strings.Contains(preview, "(2 replacements)") {
		t.Fatalf("expected replacement count in preview, got:\n%s", preview)
	}
}

func TestEditV2LineTrimFallback(t *testing.T) {
	td := t.TempDir()
	p := filepath.Join(td, "file.txt")
	writeFile(t, p, "func main() {\n    call()\n}\n")

	result, err := (&EditTool{}).Execute(editTestContext(td), map[string]any{
		"path":      p,
		"oldString": "func main() {\ncall()\n}",
		"newString": "func main() {\n    other()\n}\n",
	})
	if err != nil {
		t.Fatalf("edit failed: %v", err)
	}
	if got, want := readFile(t, p), "func main() {\n    other()\n}\n"; got != want {
		t.Fatalf("unexpected content:\n%s", got)
	}
	if !strings.Contains(result.Render(), "matched via line-trim") {
		t.Fatalf("expected fallback note, got:\n%s", result.Render())
	}
}

func TestEditV2LineTrimFallbackPreservesTrailingLineBreak(t *testing.T) {
	td := t.TempDir()
	p := filepath.Join(td, "file.txt")
	writeFile(t, p, "before\n    call()\nafter\n")

	if _, err := (&EditTool{}).Execute(editTestContext(td), map[string]any{
		"path":      p,
		"oldString": "before\ncall()",
		"newString": "before\nother()",
	}); err != nil {
		t.Fatalf("edit failed: %v", err)
	}
	if got, want := readFile(t, p), "before\nother()\nafter\n"; got != want {
		t.Fatalf("unexpected content:\n%s", got)
	}
}

func TestEditV2PreviewIncludesStructuredPayload(t *testing.T) {
	td := t.TempDir()
	p := filepath.Join(td, "file.txt")
	writeFile(t, p, "one\ntwo\n")

	preview := (&EditTool{}).PreviewContext(editTestContext(td), map[string]any{
		"path":      p,
		"oldString": "two",
		"newString": "TWO",
	})
	if !strings.Contains(preview, structuredDiffMarker) {
		t.Fatalf("expected structured diff marker in preview, got:\n%s", preview)
	}
	if !strings.Contains(preview, "-2:two") || !strings.Contains(preview, "+2:TWO") {
		t.Fatalf("expected text diff to remain present, got:\n%s", preview)
	}
}

func TestEditV2CommitsAtomicallyWithoutBackup(t *testing.T) {
	td := t.TempDir()
	p := filepath.Join(td, "file.txt")
	writeFile(t, p, "one\ntwo\n")

	if _, err := (&EditTool{}).Execute(editTestContext(td), map[string]any{
		"path":      p,
		"oldString": "two",
		"newString": "TWO",
	}); err != nil {
		t.Fatalf("edit failed: %v", err)
	}
	if got, want := readFile(t, p), "one\nTWO\n"; got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
	entries, err := os.ReadDir(td)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".nekocode-edit-") {
			t.Fatalf("staged file was not cleaned up: %s", entry.Name())
		}
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func editTestContext(root string) context.Context {
	return workspace.WithManager(context.Background(), workspace.New(root, nil))
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
