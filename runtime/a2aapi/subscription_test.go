package a2aapi

import (
	"context"
	"errors"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv/taskstore"
)

func TestSubscriptionDeliversTerminalTaskAfterEviction(t *testing.T) {
	ctx := context.Background()
	store := newRetainedTaskStore(1, func(context.Context) (string, error) { return "owner", nil })
	task := storedTaskForTest("task", a2a.TaskStateWorking)
	if _, err := store.Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	handler := &taskSubscriptionHandler{store: store}
	var states []a2a.TaskState
	for event, err := range handler.SubscribeToTask(ctx, &a2a.SubscribeToTaskRequest{ID: task.ID}) {
		if err != nil {
			t.Fatal(err)
		}
		current := event.(*a2a.Task)
		states = append(states, current.Status.State)
		if len(states) == 1 {
			stored, err := store.Get(ctx, task.ID)
			if err != nil {
				t.Fatal(err)
			}
			stored.Task.Status.State = a2a.TaskStateCompleted
			if _, err := store.Update(ctx, &taskstore.UpdateRequest{Task: stored.Task, PrevVersion: stored.Version}); err != nil {
				t.Fatal(err)
			}
			if _, err := store.Create(ctx, storedTaskForTest("replacement", a2a.TaskStateWorking)); err != nil {
				t.Fatal(err)
			}
			if _, err := store.Get(ctx, task.ID); !errors.Is(err, a2a.ErrTaskNotFound) {
				t.Fatalf("task not evicted: %v", err)
			}
		}
	}
	if len(states) != 2 || states[0] != a2a.TaskStateWorking || states[1] != a2a.TaskStateCompleted {
		t.Fatalf("subscription states=%v", states)
	}
}

func TestSubscriptionEndsWhenClientCancels(t *testing.T) {
	store := newRetainedTaskStore(1, func(context.Context) (string, error) { return "owner", nil })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if _, err := store.Create(ctx, storedTaskForTest("task", a2a.TaskStateWorking)); err != nil {
		t.Fatal(err)
	}
	handler := &taskSubscriptionHandler{store: store}
	events := 0
	for _, err := range handler.SubscribeToTask(ctx, &a2a.SubscribeToTaskRequest{ID: "task"}) {
		if err != nil {
			t.Fatal(err)
		}
		events++
		cancel()
	}
	if events != 1 {
		t.Fatalf("events after client cancellation=%d", events)
	}
}

func TestSubscriptionChecksTaskOwner(t *testing.T) {
	type ownerKey struct{}
	store := newRetainedTaskStore(1, func(ctx context.Context) (string, error) { return ctx.Value(ownerKey{}).(string), nil })
	owner := context.WithValue(context.Background(), ownerKey{}, "owner")
	if _, err := store.Create(owner, storedTaskForTest("task", a2a.TaskStateWorking)); err != nil {
		t.Fatal(err)
	}
	other := context.WithValue(context.Background(), ownerKey{}, "other")
	handler := &taskSubscriptionHandler{store: store}
	count := 0
	for event, err := range handler.SubscribeToTask(other, &a2a.SubscribeToTaskRequest{ID: "task"}) {
		count++
		if event != nil || !errors.Is(err, a2a.ErrTaskNotFound) {
			t.Fatalf("owner check bypassed: event=%v err=%v", event, err)
		}
	}
	if count != 1 {
		t.Fatalf("owner check results=%d", count)
	}
}
