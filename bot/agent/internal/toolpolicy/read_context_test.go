package toolpolicy

import (
	"context"
	"nekocode/bot/extension/tool/runtime/execution"
	"nekocode/bot/provider/types"
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
