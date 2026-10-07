package a2aapi

import (
	"context"
	"errors"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv/taskstore"
)

func TestRetainedTaskStoreEvictsOldestTerminalTask(t *testing.T) {
	store := newRetainedTaskStore(2, func(context.Context) (string, error) { return "test-user", nil })
	ctx := context.Background()
	for _, id := range []a2a.TaskID{"task_1", "task_2"} {
		if _, err := store.Create(ctx, storedTaskForTest(id, a2a.TaskStateCompleted)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.Create(ctx, storedTaskForTest("task_3", a2a.TaskStateSubmitted)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(ctx, "task_1"); !errors.Is(err, a2a.ErrTaskNotFound) {
		t.Fatalf("oldest task error = %v, want task not found", err)
	}
	for _, id := range []a2a.TaskID{"task_2", "task_3"} {
		if _, err := store.Get(ctx, id); err != nil {
			t.Fatalf("retained task %s: %v", id, err)
		}
	}
}

func TestRetainedTaskStoreDoesNotEvictActiveTask(t *testing.T) {
	store := newRetainedTaskStore(1, func(context.Context) (string, error) { return "test-user", nil })
	ctx := context.Background()
	if _, err := store.Create(ctx, storedTaskForTest("active", a2a.TaskStateWorking)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create(ctx, storedTaskForTest("next", a2a.TaskStateSubmitted)); !errors.Is(err, a2a.ErrServerError) {
		t.Fatalf("create error = %v, want server error", err)
	}
}

func TestRetainedTaskStoreCreateDoesNotEvictWhenBudgetCannotBeSatisfied(t *testing.T) {
	auth := func(context.Context) (string, error) { return "test-user", nil }
	store := newRetainedTaskStoreWithBudget(10, maxRetainedStoreBytes, auth)
	ctx := context.Background()
	terminal := storedTaskForTest("terminal", a2a.TaskStateCompleted)
	active := storedTaskForTest("active", a2a.TaskStateWorking)
	if _, err := store.Create(ctx, terminal); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create(ctx, active); err != nil {
		t.Fatal(err)
	}
	incoming := storedTaskForTest("incoming", a2a.TaskStateSubmitted)
	incomingSize, err := boundRetainedTask(incoming)
	if err != nil {
		t.Fatal(err)
	}
	store.maxBytes = store.tasks["active"].size + incomingSize - 1
	if _, err := store.Create(ctx, incoming); !errors.Is(err, a2a.ErrServerError) {
		t.Fatalf("create error = %v, want server error", err)
	}
	if _, err := store.Get(ctx, "terminal"); err != nil {
		t.Fatalf("failed create evicted terminal task: %v", err)
	}
}

func TestRetainedTaskStoreListReportsRequestedPageSize(t *testing.T) {
	store := newRetainedTaskStore(10, func(context.Context) (string, error) { return "test-user", nil })
	ctx := context.Background()
	if _, err := store.Create(ctx, storedTaskForTest("task_1", a2a.TaskStateCompleted)); err != nil {
		t.Fatal(err)
	}
	response, err := store.List(ctx, &a2a.ListTasksRequest{PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if response.PageSize != 10 || len(response.Tasks) != 1 {
		t.Fatalf("page size = %d, task count = %d", response.PageSize, len(response.Tasks))
	}
}

func TestBoundRetainedTaskTrimsHistory(t *testing.T) {
	task := storedTaskForTest("task", a2a.TaskStateCompleted)
	for range maxTaskHistoryEntries + 1 {
		task.History = append(task.History, a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("hello")))
	}
	if _, err := boundRetainedTask(task); err != nil {
		t.Fatal(err)
	}
	if len(task.History) != maxTaskHistoryEntries {
		t.Fatalf("history length = %d", len(task.History))
	}
}

func TestBoundRetainedTaskRejectsOversizedSingleRecord(t *testing.T) {
	task := storedTaskForTest("task", a2a.TaskStateCompleted)
	task.Metadata = map[string]any{"payload": make([]byte, maxRetainedTaskBytes)}
	if _, err := boundRetainedTask(task); !errors.Is(err, a2a.ErrInvalidParams) {
		t.Fatalf("error = %v, want invalid params", err)
	}
}

func TestRetainedTaskStoreEnforcesGlobalByteBudget(t *testing.T) {
	auth := func(context.Context) (string, error) { return "test-user", nil }
	first := storedTaskForTest("task_1", a2a.TaskStateCompleted)
	first.Metadata = map[string]any{"payload": "some retained data"}
	size, err := boundRetainedTask(first)
	if err != nil {
		t.Fatal(err)
	}
	store := newRetainedTaskStoreWithBudget(10, size*2, auth)
	ctx := context.Background()
	if _, err := store.Create(ctx, first); err != nil {
		t.Fatal(err)
	}
	for _, id := range []a2a.TaskID{"task_2", "task_3"} {
		task := storedTaskForTest(id, a2a.TaskStateCompleted)
		task.Metadata = map[string]any{"payload": "some retained data"}
		if _, err := store.Create(ctx, task); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.Get(ctx, "task_1"); !errors.Is(err, a2a.ErrTaskNotFound) {
		t.Fatalf("oldest task error = %v, want task not found", err)
	}
	if store.totalBytes > store.maxBytes {
		t.Fatalf("retained bytes = %d, budget = %d", store.totalBytes, store.maxBytes)
	}
}

func TestRetainedTaskStoreUpdateEvictsOnlyAfterSuccessfulUpdate(t *testing.T) {
	auth := func(context.Context) (string, error) { return "test-user", nil }
	store := newRetainedTaskStoreWithBudget(10, maxRetainedStoreBytes, auth)
	ctx := context.Background()
	terminal := storedTaskForTest("terminal", a2a.TaskStateCompleted)
	active := storedTaskForTest("active", a2a.TaskStateWorking)
	if _, err := store.Create(ctx, terminal); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create(ctx, active); err != nil {
		t.Fatal(err)
	}
	updated := storedTaskForTest("active", a2a.TaskStateWorking)
	updated.Metadata = map[string]any{"payload": "larger retained data"}
	updatedSize, err := boundRetainedTask(updated)
	if err != nil {
		t.Fatal(err)
	}
	store.maxBytes = store.totalBytes - store.tasks["terminal"].size - store.tasks["active"].size + updatedSize

	if _, err := store.Update(ctx, &taskstore.UpdateRequest{Task: updated, PrevVersion: 99}); !errors.Is(err, taskstore.ErrConcurrentModification) {
		t.Fatalf("stale update error = %v, want concurrent modification", err)
	}
	if _, err := store.Get(ctx, "terminal"); err != nil {
		t.Fatalf("failed update evicted terminal task: %v", err)
	}
	if _, err := store.Update(ctx, &taskstore.UpdateRequest{Task: updated, PrevVersion: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(ctx, "terminal"); !errors.Is(err, a2a.ErrTaskNotFound) {
		t.Fatalf("successful update did not evict terminal task: %v", err)
	}
	if store.totalBytes > store.maxBytes {
		t.Fatalf("retained bytes = %d, budget = %d", store.totalBytes, store.maxBytes)
	}
}

func storedTaskForTest(id a2a.TaskID, state a2a.TaskState) *a2a.Task {
	return &a2a.Task{ID: id, ContextID: "context_1", Status: a2a.TaskStatus{State: state}}
}

var _ taskstore.Store = (*retainedTaskStore)(nil)
