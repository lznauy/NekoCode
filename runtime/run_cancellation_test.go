package runtime

import (
	"context"
	"testing"
)

func TestCancellationReceiptDistinguishesQueueAndExecution(t *testing.T) {
	started := make(chan struct{})
	runtime := New(RunnerFunc(func(ctx context.Context, _ string, _ RunHost) (string, error) {
		close(started)
		<-ctx.Done()
		return "", ctx.Err()
	}), backgroundServices(t.TempDir()))
	t.Cleanup(func() { runtime.Close() })
	active, err := runtime.StartRun(t.Context(), Input{Text: "hold"})
	if err != nil {
		t.Fatal(err)
	}
	awaitBackground(t, started)
	queued, err := runtime.StartRun(t.Context(), Input{Text: "next"})
	if err != nil {
		t.Fatal(err)
	}
	runtime.background.mu.Lock()
	queuedContext := runtime.background.pending[queued].ctx
	runtime.background.mu.Unlock()
	result, err := runtime.CancelRunWithResult(t.Context(), queued)
	if err != nil || result.RunID != queued || result.State != CancellationQueued {
		t.Fatalf("queue receipt=%+v err=%v", result, err)
	}
	if _, ok := runtime.LookupRun(queued); ok {
		t.Fatal("removed demand produced a started run")
	}
	if queuedContext.Err() == nil {
		t.Fatal("claimed demand retained permission to start after removal")
	}
	if _, err := runtime.StartRun(context.WithValue(queuedContext, queuedRunKey{}, queued), Input{Text: "next"}); err == nil {
		t.Fatal("removed demand started after cancellation")
	}
	result, err = runtime.CancelRunWithResult(t.Context(), "")
	if err != nil || result.RunID != active || result.State != CancellationStarted {
		t.Fatalf("current receipt=%+v err=%v", result, err)
	}
	if err := runtime.WaitRun(t.Context(), active); err != nil {
		t.Fatal(err)
	}
	result, err = runtime.CancelRunWithResult(t.Context(), active)
	if err != nil || result.State != CancellationTerminal {
		t.Fatalf("terminal receipt=%+v err=%v", result, err)
	}
	view, ok := runtime.LookupRun(active)
	if !ok || view.Status != RunCancelled {
		t.Fatalf("terminal run lost: %+v %v", view, ok)
	}
}

func TestTerminalReceiptSurvivesNewCurrentRun(t *testing.T) {
	release := make(chan struct{})
	runtime := New(RunnerFunc(func(ctx context.Context, input string, _ RunHost) (string, error) {
		if input == "next" {
			select {
			case <-release:
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}
		return "done", nil
	}), ServiceGroups{})
	t.Cleanup(func() { close(release); runtime.Close() })
	old, err := runtime.StartRun(t.Context(), Input{Text: "first"})
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.WaitRun(t.Context(), old); err != nil {
		t.Fatal(err)
	}
	next, err := runtime.StartRun(t.Context(), Input{Text: "next"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.CancelRunWithResult(t.Context(), old)
	if err != nil || result.RunID != old || result.State != CancellationTerminal {
		t.Fatalf("old terminal receipt=%+v err=%v", result, err)
	}
	if runtime.Status().ActiveRun != next || runtime.Status().RunStatus != RunRunning {
		t.Fatal("cancelling settled run changed current execution")
	}
}
