package toolpolicy

import (
	"context"
	"nekocode/bot/contextmgr"
	"nekocode/bot/extension/tool/builtin/filesystem/testutil"
	"nekocode/bot/extension/tool/builtin/filesystem/write"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/execution"
	"nekocode/bot/provider/types"
	"path/filepath"
	"strings"
	"testing"
)

func TestObserveReadContextUsesActualPreview(t *testing.T) {
	state := execution.NewExecutionState()
	ctx := execution.WithExecutionState(context.Background(), state)
	id := state.Reads.Record("file", []byte("bytes"), "preview")
	for _, msg := range []types.Message{
		{Role: "tool", Content: "truncated", ReadID: id},
		{Role: "user", Content: "preview", ReadID: id},
		{Role: "tool", Content: "preview", ReadID: id, IsError: true},
	} {
		ObserveReadContext(ctx, []types.Message{msg})
		if state.Reads.Check("file", []byte("bytes")) == nil {
			t.Fatal("invalid delivery authorized mutation")
		}
	}
	ObserveReadContext(ctx, []types.Message{{Role: "tool", Content: "preview", ReadID: id}})
	if err := state.Reads.Check("file", []byte("bytes")); err != nil {
		t.Fatal(err)
	}
	ObserveReadContext(ctx, nil)
	if state.Reads.Check("file", []byte("bytes")) == nil {
		t.Fatal("absent context retained authorization")
	}
}

func TestOwnWriteReceiptSurvivesActualContextDelivery(t *testing.T) {
	root := testutil.SetupTemp(t)
	state := execution.NewExecutionState()
	state.OutputStore = core.NewOutputStore(filepath.Join(t.TempDir(), "outputs"))
	ctx := execution.WithExecutionState(testutil.Context(root), state)
	path := filepath.Join(root, "new.txt")
	content := strings.Repeat("model-authored-source\n", 1000)
	out, err := (&write.WriteTool{}).Execute(ctx, map[string]any{"path": path, "content": content})
	if err != nil {
		t.Fatal(err)
	}
	m := contextmgr.New(contextmgr.Config{ContextWindow: 10000})
	m.AddToolResultsBatch([]contextmgr.ToolResultMsg{{ToolName: "write", Message: types.Message{Content: out.Text, ToolOutput: out.Artifact, ToolCallID: "write-call", ReadID: out.ReadID}}})
	ObserveReadContext(ctx, m.Snapshot().Messages)
	if err := state.Reads.Check(path, []byte(content)); err != nil {
		t.Fatalf("actual delivered mutation lost its receipt: %v", err)
	}
	restored := contextmgr.New(contextmgr.Config{ContextWindow: 10000})
	restored.Restore(m.Snapshot())
	ObserveReadContext(ctx, restored.Snapshot().Messages)
	if state.Reads.Check(path, []byte(content)) == nil {
		t.Fatal("restored history revived authorization")
	}
}
