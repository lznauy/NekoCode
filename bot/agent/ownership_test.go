package agent

import (
	"context"
	"errors"
	"testing"
)

func TestRejectedRunDoesNotBindOrAbortOwner(t *testing.T) {
	a := newTestAgent()
	a.runMu.Lock()
	defer a.runMu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if result := a.RunWithCallbacks(ctx, "", nil, nil, Output{}, Interaction{}); result.Error == nil {
		t.Fatal("concurrent callback scope accepted")
	}
	if result := a.Run("", nil, nil); result.Error == nil {
		t.Fatal("concurrent run accepted")
	}
	if err := a.getCtx().Err(); err != nil {
		t.Fatalf("rejected caller aborted owner: %v", err)
	}
}

func TestCanceledCallbackRunNeverStarts(t *testing.T) {
	a := newTestAgent()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	before := a.deps.ctxMgr.Len()
	result := a.RunWithCallbacks(ctx, "must not run", nil, nil, Output{}, Interaction{})
	if !errors.Is(result.Error, context.Canceled) || !result.Interrupted {
		t.Fatalf("result=%+v", result)
	}
	if a.deps.ctxMgr.Len() != before {
		t.Fatal("canceled run changed conversation")
	}
}
