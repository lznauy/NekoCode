package edit

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nekocode/bot/tools"
	readtool "nekocode/bot/tools/filesystem/read"
)

func TestEditRejectsLegacyDSL(t *testing.T) {
	_, err := (&EditTool{}).Execute(context.Background(), map[string]any{
		"patch": "[/tmp/a.go#12345678]\nreplace 1..1:\n+x",
	})
	if err == nil || !strings.Contains(err.Error(), "JSON intent") {
		t.Fatalf("expected JSON intent rejection, got %v", err)
	}
}

func TestEditJSONIntentMultipleOps(t *testing.T) {
	td := t.TempDir()
	p := filepath.Join(td, "intent.txt")
	if err := os.WriteFile(p, []byte("one\ntwo\nthree\nfour\n"), 0644); err != nil {
		t.Fatal(err)
	}
	ctx := tools.WithExecutionState(context.Background(), tools.NewExecutionState())
	out := readFileForEdit(t, ctx, p, 1, 4)

	patch := mustIntent(t, map[string]any{
		"path":          p,
		"base_revision": extractReadHeaderTag(out),
		"ops": []map[string]any{
			makeIntentOp("replace", extractViewField(out, "window"), 2, 2, "TWO"),
			makeIntentOp("insert_after", extractViewField(out, "window"), 4, 4, "five"),
		},
	})
	result, err := (&EditTool{}).Execute(ctx, map[string]any{"patch": patch})
	if err != nil {
		t.Fatalf("edit failed: %v\n%s", err, result)
	}
	if got, want := readFile(t, p), "one\nTWO\nthree\nfour\nfive\n"; got != want {
		t.Fatalf("unexpected content:\n%s", got)
	}
	if !strings.Contains(result, "VIEW rev=") {
		t.Fatalf("expected edit result to include new VIEW metadata:\n%s", result)
	}
}

func TestEditJSONIntentDelete(t *testing.T) {
	td := t.TempDir()
	p := filepath.Join(td, "intent.txt")
	if err := os.WriteFile(p, []byte("one\ntwo\nthree\n"), 0644); err != nil {
		t.Fatal(err)
	}
	ctx := tools.WithExecutionState(context.Background(), tools.NewExecutionState())
	out := readFileForEdit(t, ctx, p, 1, 3)

	patch := mustIntent(t, map[string]any{
		"path":          p,
		"base_revision": extractReadHeaderTag(out),
		"ops": []map[string]any{{
			"op": "delete",
			"target": map[string]any{
				"window_id":  extractViewField(out, "window"),
				"start_line": 2,
				"end_line":   2,
			},
		}},
	})
	if _, err := (&EditTool{}).Execute(ctx, map[string]any{"patch": patch}); err != nil {
		t.Fatalf("edit failed: %v", err)
	}
	if got, want := readFile(t, p), "one\nthree\n"; got != want {
		t.Fatalf("unexpected content:\n%s", got)
	}
}

func TestEditJSONIntentRejectsChangedTarget(t *testing.T) {
	td := t.TempDir()
	p := filepath.Join(td, "intent.txt")
	if err := os.WriteFile(p, []byte("one\ntwo\n"), 0644); err != nil {
		t.Fatal(err)
	}
	ctx := tools.WithExecutionState(context.Background(), tools.NewExecutionState())
	out := readFileForEdit(t, ctx, p, 1, 2)
	if err := os.WriteFile(p, []byte("one\nchanged\n"), 0644); err != nil {
		t.Fatal(err)
	}
	patch := mustIntent(t, map[string]any{
		"path":          p,
		"base_revision": extractReadHeaderTag(out),
		"ops": []map[string]any{
			makeIntentOp("replace", extractViewField(out, "window"), 2, 2, "TWO"),
		},
	})
	_, err := (&EditTool{}).Execute(ctx, map[string]any{"patch": patch})
	if err == nil || !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("expected conflict error, got %v", err)
	}
}

func TestEditJSONIntentRebasesWhenTargetLinesUnchanged(t *testing.T) {
	td := t.TempDir()
	p := filepath.Join(td, "intent.txt")
	if err := os.WriteFile(p, []byte("one\ntwo\nthree\n"), 0644); err != nil {
		t.Fatal(err)
	}
	ctx := tools.WithExecutionState(context.Background(), tools.NewExecutionState())
	out := readFileForEdit(t, ctx, p, 1, 3)
	if err := os.WriteFile(p, []byte("one\ntwo\nTHREE\n"), 0644); err != nil {
		t.Fatal(err)
	}
	patch := mustIntent(t, map[string]any{
		"path":          p,
		"base_revision": extractReadHeaderTag(out),
		"ops": []map[string]any{
			makeIntentOp("replace", extractViewField(out, "window"), 2, 2, "TWO"),
		},
	})
	result, err := (&EditTool{}).Execute(ctx, map[string]any{"patch": patch})
	if err != nil {
		t.Fatalf("expected safe rebase, got %v", err)
	}
	if !strings.Contains(result, "rebased") {
		t.Fatalf("expected rebased note, got:\n%s", result)
	}
	if got, want := readFile(t, p), "one\nTWO\nTHREE\n"; got != want {
		t.Fatalf("unexpected content:\n%s", got)
	}
}

func TestEditJSONIntentRelocatesMovedUniqueTarget(t *testing.T) {
	td := t.TempDir()
	p := filepath.Join(td, "intent.txt")
	if err := os.WriteFile(p, []byte("one\ntwo\nthree\n"), 0644); err != nil {
		t.Fatal(err)
	}
	ctx := tools.WithExecutionState(context.Background(), tools.NewExecutionState())
	out := readFileForEdit(t, ctx, p, 1, 3)
	if err := os.WriteFile(p, []byte("zero\none\ntwo\nthree\n"), 0644); err != nil {
		t.Fatal(err)
	}
	patch := mustIntent(t, map[string]any{
		"path":          p,
		"base_revision": extractReadHeaderTag(out),
		"ops": []map[string]any{
			makeIntentOp("replace", extractViewField(out, "window"), 2, 2, "TWO"),
		},
	})
	result, err := (&EditTool{}).Execute(ctx, map[string]any{"patch": patch})
	if err != nil {
		t.Fatalf("expected relocated edit, got %v", err)
	}
	if !strings.Contains(result, "relocated") {
		t.Fatalf("expected relocated note, got:\n%s", result)
	}
	if got, want := readFile(t, p), "zero\none\nTWO\nthree\n"; got != want {
		t.Fatalf("unexpected content:\n%s", got)
	}
}

func TestEditJSONIntentRejectsAmbiguousRelocation(t *testing.T) {
	td := t.TempDir()
	p := filepath.Join(td, "intent.txt")
	if err := os.WriteFile(p, []byte("one\ntwo\nthree\n"), 0644); err != nil {
		t.Fatal(err)
	}
	ctx := tools.WithExecutionState(context.Background(), tools.NewExecutionState())
	out := readFileForEdit(t, ctx, p, 1, 3)
	if err := os.WriteFile(p, []byte("two\none\ntwo\nthree\n"), 0644); err != nil {
		t.Fatal(err)
	}
	patch := mustIntent(t, map[string]any{
		"path":          p,
		"base_revision": extractReadHeaderTag(out),
		"ops": []map[string]any{
			makeIntentOp("replace", extractViewField(out, "window"), 2, 2, "TWO"),
		},
	})
	_, err := (&EditTool{}).Execute(ctx, map[string]any{"patch": patch})
	if err == nil || !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("expected ambiguous relocation conflict, got %v", err)
	}
}

func TestEditJSONIntentPreviewIncludesStructuredPayload(t *testing.T) {
	td := t.TempDir()
	p := filepath.Join(td, "intent.txt")
	if err := os.WriteFile(p, []byte("one\ntwo\n"), 0644); err != nil {
		t.Fatal(err)
	}
	ctx := tools.WithExecutionState(context.Background(), tools.NewExecutionState())
	out := readFileForEdit(t, ctx, p, 1, 2)

	patch := mustIntent(t, map[string]any{
		"path":          p,
		"base_revision": extractReadHeaderTag(out),
		"ops": []map[string]any{
			makeIntentOp("replace", extractViewField(out, "window"), 2, 2, "TWO"),
		},
	})
	preview := (&EditTool{}).Preview(map[string]any{"patch": patch})
	if !strings.Contains(preview, structuredDiffMarker) {
		t.Fatalf("expected structured diff marker in preview, got:\n%s", preview)
	}
	if !strings.Contains(preview, "-2:two") || !strings.Contains(preview, "+2:TWO") {
		t.Fatalf("expected text diff to remain present, got:\n%s", preview)
	}
}

func TestEditRevert(t *testing.T) {
	td := t.TempDir()
	p := filepath.Join(td, "intent.txt")
	original := "one\ntwo\n"
	if err := os.WriteFile(p, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}
	ctx := tools.WithExecutionState(context.Background(), tools.NewExecutionState())
	out := readFileForEdit(t, ctx, p, 1, 2)
	patch := mustIntent(t, map[string]any{
		"path":          p,
		"base_revision": extractReadHeaderTag(out),
		"ops": []map[string]any{
			makeIntentOp("replace", extractViewField(out, "window"), 2, 2, "TWO"),
		},
	})
	if _, err := (&EditTool{}).Execute(ctx, map[string]any{"patch": patch}); err != nil {
		t.Fatalf("edit failed: %v", err)
	}
	result, err := (&EditTool{}).Execute(ctx, map[string]any{"patch": p, "revert": true})
	if err != nil {
		t.Fatalf("revert failed: %v", err)
	}
	if !strings.Contains(result, "Reverted") {
		t.Fatalf("expected revert message, got %q", result)
	}
	if got := readFile(t, p); got != original {
		t.Fatalf("after revert got %q, want %q", got, original)
	}
}

func readFileForEdit(t *testing.T, ctx context.Context, path string, start, end int) string {
	t.Helper()
	out, err := (&readtool.ReadTool{}).Execute(ctx, map[string]any{
		"path": path, "startLine": float64(start), "endLine": float64(end),
	})
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	return out
}

func mustIntent(t *testing.T, intent map[string]any) string {
	t.Helper()
	data, err := json.Marshal(intent)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func makeIntentOp(op, window string, start, end int, content string) map[string]any {
	return map[string]any{
		"op": op,
		"target": map[string]any{
			"window_id":  window,
			"start_line": start,
			"end_line":   end,
		},
		"content": content,
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func extractReadHeaderTag(out string) string {
	first, _, _ := strings.Cut(out, "\n")
	hashStart := strings.LastIndex(first, "#")
	if hashStart < 0 || !strings.HasSuffix(first, "]") {
		return ""
	}
	return strings.TrimSuffix(first[hashStart+1:], "]")
}

func extractViewField(out, key string) string {
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, "VIEW ") {
			continue
		}
		for _, field := range strings.Fields(line) {
			prefix := key + "="
			if strings.HasPrefix(field, prefix) {
				return strings.TrimPrefix(field, prefix)
			}
		}
	}
	return ""
}
