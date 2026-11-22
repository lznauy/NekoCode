package task

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/taskbridge"
	"nekocode/bot/extension/tool/runtime/workspace"
)

func TestBatchRejectsInvalidListBeforeDispatch(t *testing.T) {
	root := t.TempDir()
	ctx := workspace.WithManager(t.Context(), workspace.New(root, nil))
	if err := os.Mkdir(filepath.Join(root, "docs"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, pair := range []struct{ id, path string }{{"one", "different.md"}, {"two", "docs/../out.md"}, {"two", "out.md/sub.md"}, {"two", "../outside.md"}, {"two", "docs"}} {
		args := map[string]any{"profile": "coder", "prompt": "review", "items": []taskbridge.BatchItem{{ID: "one", Input: "source", Output: "out.md"}, {ID: pair.id, Input: "source", Output: pair.path}},
			"_sub_callback": taskbridge.BatchDispatch(func(context.Context, map[string]any, []taskbridge.BatchItem) (core.ToolOutput, error) {
				t.Fatal("dispatched invalid batch")
				return core.ToolOutput{}, nil
			})}
		if _, err := (&BatchTool{}).Execute(ctx, args); err == nil {
			t.Fatalf("accepted %+v", pair)
		}
	}
}

func TestBatchChecksOutputAsFileBeforeDispatch(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "nested", "dir"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Run("symlink followed by parent component", func(t *testing.T) {
		if err := os.Symlink(filepath.Join(root, "nested", "dir"), filepath.Join(root, "alias")); err != nil {
			t.Skip(err)
		}
		assertBatchRejectsOutputs(t, root, "alias/../report.md", "nested/report.md")
	})
	for _, path := range []string{"missing/", "missing/."} {
		t.Run(path, func(t *testing.T) { assertBatchRejectsOutputs(t, root, path) })
	}
	valid := "nested/dir/../new/report.md"
	called := false
	args := map[string]any{"profile": "coder", "prompt": "review", "items": []taskbridge.BatchItem{{ID: "check", Input: "inspect", Output: valid}},
		"_sub_callback": taskbridge.BatchDispatch(func(_ context.Context, _ map[string]any, items []taskbridge.BatchItem) (core.ToolOutput, error) {
			called = len(items) == 1 && items[0].Output == valid
			return core.ToolOutput{}, nil
		}),
	}
	ctx := workspace.WithManager(t.Context(), workspace.New(root, nil))
	if _, err := (&BatchTool{}).Execute(ctx, args); err != nil || !called {
		t.Fatalf("valid new file was not dispatched: %v", err)
	}

}

func assertBatchRejectsOutputs(t *testing.T, root string, paths ...string) {
	t.Helper()
	var items []taskbridge.BatchItem
	for i, p := range paths {
		items = append(items, taskbridge.BatchItem{ID: fmt.Sprint(i), Input: "inspect", Output: p})
	}
	called := false
	args := map[string]any{"profile": "coder", "prompt": "review", "items": items,
		"_sub_callback": taskbridge.BatchDispatch(func(context.Context, map[string]any, []taskbridge.BatchItem) (core.ToolOutput, error) {
			called = true
			return core.ToolOutput{}, nil
		}),
	}
	ctx := workspace.WithManager(t.Context(), workspace.New(root, nil))
	if _, err := (&BatchTool{}).Execute(ctx, args); err == nil || called {
		t.Fatalf("accepted output(s) %q, dispatched=%v, err=%v", paths, called, err)
	}
}

// A native caller can supply any JSON-marshalable slice, just like a provider
// tool call supplies []any. Oversized manifests are rejected by count.
type observedBatchItem struct{ encodes *int }

func (item observedBatchItem) MarshalJSON() ([]byte, error) {
	(*item.encodes)++
	return []byte(`{"id":"entry","input":"inspect"}`), nil
}

func TestBatchRejectsOversizeBeforeEncodingItems(t *testing.T) {
	var encodes int
	list := make([]observedBatchItem, 101)
	for i := range list {
		list[i].encodes = &encodes
	}
	args := map[string]any{"profile": "explore", "prompt": "review", "items": list,
		"_sub_callback": taskbridge.BatchDispatch(func(context.Context, map[string]any, []taskbridge.BatchItem) (core.ToolOutput, error) {
			t.Fatal("dispatched oversized batch")
			return core.ToolOutput{}, nil
		}),
	}
	if _, err := (&BatchTool{}).Execute(t.Context(), args); err == nil || encodes != 0 {
		t.Fatalf("oversized batch encoded %d items, err=%v", encodes, err)
	}
}
