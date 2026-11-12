package runtime

import (
	"context"
	"testing"
)

func TestTodoBackgroundActivityIsScopedToSessionAndPlan(t *testing.T) {
	rt := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) { return "", nil }), backgroundServices(t.TempDir()))
	defer rt.Close()
	rt.background.mu.Lock()
	for _, task := range []BackgroundTask{
		{ID: "running", SessionID: "session", GroupID: "A", Status: "running"},
		{ID: "queued", SessionID: "session", GroupID: "A", Status: "waiting_dependencies"},
		{ID: "finished", SessionID: "session", GroupID: "A", Status: "completed"},
		{ID: "other-plan", SessionID: "session", GroupID: "B", Status: "running"},
		{ID: "other-session", SessionID: "elsewhere", GroupID: "A", Status: "running"},
	} {
		rt.background.entries[task.ID] = &backgroundEntry{BackgroundTask: task}
	}
	rt.background.mu.Unlock()
	host := runHost{runtime: rt}
	if count := host.PendingBackgroundPlan("A"); count != 2 {
		t.Fatalf("plan A pending=%d", count)
	}
	if count := host.PendingBackgroundPlan("B"); count != 1 {
		t.Fatalf("plan B pending=%d", count)
	}
	if count := host.PendingBackgroundPlan("unknown"); count != 0 {
		t.Fatal("unknown plan inherited work")
	}
}
