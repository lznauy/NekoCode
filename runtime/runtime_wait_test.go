package runtime

import (
	"context"
	"testing"
)

func TestWaitRunWaitsForLifecycleCleanup(t *testing.T) {
	release := make(chan struct{})
	runtime := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) {
		<-release
		return "done", nil
	}), ServiceGroups{})
	defer runtime.Close()
	runID, err := runtime.StartRun(context.Background(), Input{Text: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		close(started)
		done <- runtime.WaitRun(context.Background(), runID)
	}()
	<-started
	select {
	case err := <-done:
		t.Fatalf("WaitRun returned before cleanup: %v", err)
	default:
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if status := runtime.Status(); status.State != RuntimeReady {
		t.Fatalf("state = %s, want %s", status.State, RuntimeReady)
	}
}
