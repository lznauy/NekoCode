package core

import (
	"context"
	"errors"
	"testing"

	"nekocode/bot/extension/tool/runtime/taskbridge"
	"nekocode/bot/session"
)

func TestTaskReferencesUseCurrentParentAndOwnerAttempts(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	b := &Bot{sess: session.New(t.TempDir())}
	parent := b.sess.CurrentID()
	attempt := "old-attempt"
	b.taskLister = func(ctx context.Context, filter taskbridge.TaskListFilter) (taskbridge.TaskPage, error) {
		if filter.SessionID != parent || !filter.PrioritizeActive || filter.Limit != 16 || filter.Offset != 0 {
			t.Fatalf("unbounded or unscoped read: %+v", filter)
		}
		return taskbridge.TaskPage{SessionID: parent, Total: 1000, Items: []taskbridge.TaskSummary{{ID: "task", Status: "running", LatestAttempt: &taskbridge.TaskAttemptSummary{ID: attempt, Seq: 2}}}}, nil
	}
	for _, id := range []string{"old-attempt", "resumed-attempt"} {
		attempt = id
		refs := b.taskReferences(t.Context(), parent)
		if refs.Unavailable || refs.Total != 1000 || len(refs.Items) != 1 || refs.Items[0].AttemptID != id {
			t.Fatalf("wrong owner projection: %+v", refs)
		}
	}
	b.taskLister = func(context.Context, taskbridge.TaskListFilter) (taskbridge.TaskPage, error) {
		return taskbridge.TaskPage{SessionID: "different-parent", Items: []taskbridge.TaskSummary{{ID: "private-task"}}}, nil
	}
	refs := b.taskReferences(t.Context(), parent)
	if !refs.Unavailable || len(refs.Items) != 0 {
		t.Fatal("cross-session task leaked")
	}
	b.taskLister = func(context.Context, taskbridge.TaskListFilter) (taskbridge.TaskPage, error) {
		return taskbridge.TaskPage{}, errors.New("sensitive store error")
	}
	refs = b.taskReferences(t.Context(), parent)
	if !refs.Unavailable || len(refs.Items) != 0 {
		t.Fatal("read failure invented task state")
	}
}
