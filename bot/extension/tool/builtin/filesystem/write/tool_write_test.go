package write

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nekocode/bot/extension/tool/builtin/filesystem/testutil"
)

func TestWriteTool(t *testing.T) {
	td := testutil.SetupTemp(t)
	w := &WriteTool{}
	p := filepath.Join(td, "new.txt")

	out, err := w.Execute(testutil.Context(td), map[string]any{"path": p, "content": "hello"})
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if out.Render() == "" {
		t.Error("empty output")
	}
	data, _ := os.ReadFile(p)
	if string(data) != "hello" {
		t.Errorf("content = %q, want %q", string(data), "hello")
	}
}

func TestWritePreviewAllowsEmptyContent(t *testing.T) {
	td := testutil.SetupTemp(t)
	w := &WriteTool{}
	p := filepath.Join(td, "existing.txt")
	if err := os.WriteFile(p, []byte("remove me\n"), 0644); err != nil {
		t.Fatal(err)
	}

	out := w.PreviewContext(testutil.Context(td), map[string]any{"path": p, "content": ""})
	if !strings.Contains(out, "-1:remove me") {
		t.Fatalf("preview = %q, want deletion diff", out)
	}
}

func TestWritePreviewNoChangesIsPlain(t *testing.T) {
	td := testutil.SetupTemp(t)
	w := &WriteTool{}
	p := filepath.Join(td, "existing.txt")
	if err := os.WriteFile(p, []byte("same\n"), 0644); err != nil {
		t.Fatal(err)
	}

	out := w.PreviewContext(testutil.Context(td), map[string]any{"path": p, "content": "same\n"})
	if out != "" {
		t.Fatalf("preview = %q, want empty no-change preview", out)
	}
}

func TestWrittenJSONSyntaxReceipt(t *testing.T) {
	for _, tc := range []struct{ content, diagnostic string }{
		{`{"ok":true}`, "JSON syntax: valid."},
		{`{"missing":}`, "JSON syntax: invalid (file saved)."},
	} {
		root := testutil.SetupTemp(t)
		path := filepath.Join(root, "config.json")
		out, err := (&WriteTool{}).Execute(testutil.Context(root), map[string]any{"path": path, "content": tc.content})
		if err != nil || !strings.Contains(out.Render(), tc.diagnostic) {
			t.Fatalf("receipt %q; error %v", out.Render(), err)
		}
		data, err := os.ReadFile(path)
		if err != nil || string(data) != tc.content {
			t.Fatalf("saved %q; error %v", data, err)
		}
	}
}
