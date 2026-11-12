package runtime

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBackgroundQueryDoesNotWakeCoordinator(t *testing.T) {
	ran := make(chan struct{}, 1)
	rt := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) {
		ran <- struct{}{}
		return "", nil
	}), backgroundServices(t.TempDir()))
	defer rt.Close()
	b := rt.background
	b.mu.Lock()
	b.entries["done"] = &backgroundEntry{BackgroundTask: BackgroundTask{ID: "done", SessionID: "session", Status: "completed", Result: "evidence"}}
	b.order = append(b.order, "done")
	b.mu.Unlock()
	if _, err := rt.BackgroundTasks("session"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ran:
		t.Fatal("receipt query started a model turn")
	case <-time.After(100 * time.Millisecond):
	}
	// Explicit activation must still deliver the saved notification.
	b.signal()
	awaitBackground(t, ran)
}

func TestBackgroundReviewAdmissionFailureIsVisibleAndNotReplayed(t *testing.T) {
	root := t.TempDir()
	rt := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) {
		t.Error("recording failure must prevent execution")
		return "", nil
	}), backgroundServices(root))
	defer rt.Close()
	if err := rt.EnableEventRecording(root); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "session"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "session", "runs"), []byte("blocked"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	events, err := rt.Events(ctx, EventFilter{Types: []EventType{EventSystemMessage}})
	if err != nil {
		t.Fatal(err)
	}
	b := rt.background
	b.mu.Lock()
	b.entries["done"] = &backgroundEntry{BackgroundTask: BackgroundTask{ID: "done", SessionID: "session", Status: "completed", Result: "evidence"}}
	b.order = append(b.order, "done")
	b.mu.Unlock()
	b.signal()
	select {
	case <-events:
	case <-ctx.Done():
		t.Fatal("review admission failure was silent")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.entries["done"].Delivery != "failed" || b.entries["done"].Notified {
		t.Fatalf("failed review must remain available for explicit recovery: %+v", b.entries["done"])
	}
}

func TestBackgroundCoordinatorReleaseAlwaysWakes(t *testing.T) {
	b := &backgroundTasks{coordinating: true, coordinatorSession: "session", wake: make(chan struct{}, 1)}
	b.releaseCoordinator()
	if b.coordinating || b.coordinatorSession != "" || b.coordinatorCancel != nil {
		t.Fatal("coordinator reservation was retained")
	}
	select {
	case <-b.wake:
	default:
		t.Fatal("released reservation lost pending work wake")
	}
}
