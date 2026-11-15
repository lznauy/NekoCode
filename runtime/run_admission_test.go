package runtime

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestDetachedRunOutlivesAdmissionContext(t *testing.T) {
	started := make(chan context.Context, 1)
	rt := New(RunnerFunc(func(ctx context.Context, _ string, _ RunHost) (string, error) {
		started <- ctx
		<-ctx.Done()
		return "", ctx.Err()
	}), ServiceGroups{})
	t.Cleanup(func() { rt.Close() })
	parent, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	id, err := rt.StartRun(WithDetachedRun(parent), Input{Text: "task"})
	if err != nil {
		t.Fatal(err)
	}
	var execution context.Context
	select {
	case execution = <-started:
	case <-time.After(time.Second):
		t.Fatal("runner did not start")
	}
	cancel()
	if execution.Err() != nil {
		t.Fatalf("accepted run cancelled with admission: %v", execution.Err())
	}
	if _, ok := execution.Deadline(); ok {
		t.Fatal("accepted run retained admission deadline")
	}
	if err := rt.CancelRun(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	if execution.Err() == nil {
		t.Fatal("explicit abort did not cancel detached run")
	}
}

func TestRunAdmissionWaitHonorsDeadline(t *testing.T) {
	rt := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) {
		t.Error("cancelled input executed")
		return "", nil
	}), ServiceGroups{})
	t.Cleanup(func() { rt.Close() })
	rt.mutationMu.Lock()
	defer rt.mutationMu.Unlock()
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := rt.StartRun(WithDetachedRun(ctx), Input{Text: "task"}); done <- err }()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("err=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("admission blocked after deadline")
	}
	if runs := rt.Runs(100); len(runs) != 0 {
		t.Fatalf("cancelled admission created runs: %v", runs)
	}
}
