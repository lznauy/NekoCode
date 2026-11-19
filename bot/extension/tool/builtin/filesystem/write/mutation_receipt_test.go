package write

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nekocode/bot/extension/tool/builtin/filesystem/edit"
	"nekocode/bot/extension/tool/builtin/filesystem/testutil"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/execution"
	textutil "nekocode/util/text"
)

func TestOwnWriteThenEditsRequireDeliveredCurrentVersion(t *testing.T) {
	root := testutil.SetupTemp(t)
	state := execution.NewExecutionState()
	state.OutputStore = core.NewOutputStore(filepath.Join(t.TempDir(), "outputs"))
	ctx := execution.WithExecutionState(testutil.Context(root), state)
	path := filepath.Join(root, "new.txt")
	out, err := (&WriteTool{}).Execute(ctx, map[string]any{"path": path, "content": "first\n"})
	if err != nil {
		t.Fatal(err)
	}
	change := func(old, new string) (core.ToolOutput, error) {
		return (&edit.EditTool{}).Execute(ctx, map[string]any{"path": path, "oldString": old, "newString": new})
	}
	if _, err = change("first", "second"); err == nil {
		t.Fatal("undelivered write authorized edit")
	}
	if out.ReadID == "" {
		t.Fatal("successful write has no version receipt")
	}
	state.Reads.Observe(map[string]string{out.ReadID: textutil.NormalizeTerminalOutput(out.Render())})
	edited, err := change("first", "second")
	if err != nil {
		t.Fatalf("own delivered write did not authorize edit: %v", err)
	}
	if _, err = change("second", "third"); err == nil {
		t.Fatal("undelivered edit authorized next edit")
	}
	state.Reads.Observe(map[string]string{edited.ReadID: edited.Render()})
	if _, err = change("second", "third"); err != nil {
		t.Fatal(err)
	}
}

func TestCompactWritePreservesDiffAndRevokesOnMissingReceipt(t *testing.T) {
	root := testutil.SetupTemp(t)
	state := execution.NewExecutionState()
	state.OutputStore = core.NewOutputStore(filepath.Join(t.TempDir(), "outputs"))
	ctx := execution.WithExecutionState(testutil.Context(root), state)
	path := filepath.Join(root, "large.txt")
	content := strings.Repeat("unique-source-line\n", 1000)
	out, err := (&WriteTool{}).Execute(ctx, map[string]any{"path": path, "content": content})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.Text, "unique-source-line") || len(out.Render()) >= 2048 {
		t.Fatal("full new source repeated", len(out.Render()))
	}
	if strings.Contains(out.Render(), "truncated") || !strings.Contains(out.Render(), "[Full diff:") {
		t.Fatal("misleading write receipt", out.Render())
	}
	if out.Artifact.Path == "" {
		t.Fatal("full diff lost")
	}
	diff, err := os.ReadFile(out.Artifact.Path)
	if err != nil || !strings.Contains(string(diff), "unique-source-line") {
		t.Fatalf("missing complete diff: %v", err)
	}
	t.Logf("full diff: %d bytes; model receipt: %d bytes", len(diff), len(out.Render()))
	state.Reads.Observe(map[string]string{out.ReadID: textutil.NormalizeTerminalOutput(out.Render())})
	if err = state.Reads.Check(path, []byte(content)); err != nil {
		t.Fatal(err)
	}
	state.Reads.Observe(map[string]string{out.ReadID: "shortened"})
	if state.Reads.Check(path, []byte(content)) == nil {
		t.Fatal("truncated receipt authorized mutation")
	}
	state.Reads.Observe(map[string]string{out.ReadID: textutil.NormalizeTerminalOutput(out.Render())})
	if state.Reads.Check(path, []byte("external change")) == nil {
		t.Fatal("external change bypassed version check")
	}
	other := execution.NewExecutionState()
	other.Reads.Observe(map[string]string{out.ReadID: textutil.NormalizeTerminalOutput(out.Render())})
	if other.Reads.Check(path, []byte(content)) == nil {
		t.Fatal("other actor reused receipt")
	}
	state.Reads.Observe(nil)
	if state.Reads.Check(path, []byte(content)) == nil {
		t.Fatal("receipt survived removed context")
	}
}

func TestWriteArtifactFailureKeepsInlineEvidenceAndExactBytes(t *testing.T) {
	root := testutil.SetupTemp(t)
	blocker := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocker, []byte("block"), 0600); err != nil {
		t.Fatal(err)
	}
	state := execution.NewExecutionState()
	state.OutputStore = core.NewOutputStore(filepath.Join(blocker, "outputs"))
	ctx := execution.WithExecutionState(testutil.Context(root), state)
	path := filepath.Join(root, "large.txt")
	content := strings.Repeat("exact-content\r\n", 1000)
	out, err := (&WriteTool{}).Execute(ctx, map[string]any{"path": path, "content": content})
	if err != nil {
		t.Fatalf("successful write reported failure: %v", err)
	}
	if out.Artifact.Path != "" || !strings.Contains(out.Text, "exact-content") {
		t.Fatal("fallback lost full inline evidence")
	}
	state.Reads.Observe(map[string]string{out.ReadID: textutil.NormalizeTerminalOutput(out.Render())})
	if err := state.Reads.Check(path, []byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := state.Reads.Check(path, []byte(strings.ReplaceAll(content, "\r\n", "\n"))); err == nil {
		t.Fatal("raw version comparison normalized line endings")
	}
}
