package runtime

import (
	"context"
	"fmt"
	"nekocode/protocol"
	"testing"
	"time"
)

func TestBackgroundFullHistoryStillAllowsExistingTaskResume(t *testing.T) {
	rt := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) { return "", nil }), backgroundServices(t.TempDir()))
	defer rt.Close()
	b := rt.background
	b.mu.Lock()
	for i := range backgroundHistoryLimit {
		id := fmt.Sprintf("task-%d", i)
		b.entries[id] = &backgroundEntry{BackgroundTask: BackgroundTask{ID: id, SessionID: "session", Status: "completed", Notified: true, PendingUpdates: []string{"unread instruction"}, UpdateSequence: 1}}
		b.order = append(b.order, id)
	}
	b.mu.Unlock()
	_, err := b.submit(t.Context(), protocol.BackgroundWork{ID: "task-0", SessionID: "session", ReadOnly: true, Run: func(ctx context.Context, _ protocol.BackgroundHost) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	}})
	if err != nil {
		t.Fatalf("resume does not add history and must remain possible: %v", err)
	}
	b.mu.Lock()
	if len(b.entries) != backgroundHistoryLimit || len(b.entries["task-0"].PendingUpdates) != 1 {
		t.Error("resume lost history or unread instructions")
	}
	b.mu.Unlock()
	if _, err := b.submit(t.Context(), protocol.BackgroundWork{ID: "brand-new", SessionID: "session", ReadOnly: true, Run: func(context.Context, protocol.BackgroundHost) (string, error) { return "", nil }}); err == nil {
		t.Fatal("new task bypassed full history bound")
	}
}

func TestBackgroundCancellationOverridesPartialHandoff(t *testing.T) {
	rt := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) { return "", nil }), backgroundServices(t.TempDir()))
	defer rt.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	events, err := rt.Events(ctx, EventFilter{SubAgentID: "task", Types: []EventType{EventSubAgentEnded}})
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	_, err = rt.background.submit(ctx, protocol.BackgroundWork{ID: "task", SessionID: "session", ReadOnly: true, Run: func(ctx context.Context, host protocol.BackgroundHost) (string, error) {
		host.Step(protocol.StepEvent{Action: protocol.StepActionSubAgentEnd, SubAgentStatus: "partial", SubAgentAttemptID: "attempt", SubAgentAttemptSeq: 1})
		close(started)
		<-ctx.Done()
		return "partial handoff", nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	awaitBackground(t, started)
	select {
	case event := <-events:
		t.Errorf("worker published premature terminal event: %+v", event.Payload)
	default:
	}
	if err := rt.CancelBackgroundTask(ctx, "session", "task"); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-events:
		if event.Payload.(SubAgentPayload).Status != "interrupted" {
			t.Fatalf("partial result overrode cancellation: %+v", event.Payload)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	tasks, err := rt.BackgroundTasks("session")
	if err != nil || len(tasks) != 1 || tasks[0].Status != "interrupted" {
		t.Fatalf("wrong final receipt: %+v %v", tasks, err)
	}
}

func TestBackgroundAdmissionPrunesEnoughLoadedTerminalHistory(t *testing.T) {
	rt := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) { return "", nil }), backgroundServices(t.TempDir()))
	defer rt.Close()
	b := rt.background
	b.mu.Lock()
	// Session history can be loaded independently, exceeding the admission cache.
	for i := range backgroundHistoryLimit + 3 {
		id := fmt.Sprintf("history-%d", i)
		sessionID := "session"
		if i >= backgroundHistoryLimit {
			sessionID = "previous-session"
		}
		b.entries[id] = &backgroundEntry{BackgroundTask: BackgroundTask{ID: id, SessionID: sessionID, Status: "completed", Notified: true}}
		b.order = append(b.order, id)
	}
	b.mu.Unlock()
	_, err := b.submit(t.Context(), protocol.BackgroundWork{ID: "new-task", SessionID: "session", ReadOnly: true, Run: func(ctx context.Context, _ protocol.BackgroundHost) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	}})
	if err != nil {
		t.Fatalf("eligible history required multiple failed retries: %v", err)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.entries) > backgroundHistoryLimit {
		t.Fatal("admission did not restore the history bound")
	}
}

func TestPendingBackgroundPlanScopesActiveTasks(t *testing.T) {
	rt := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) { return "", nil }), backgroundServices(t.TempDir()))
	defer rt.Close()
	b := rt.background
	b.mu.Lock()
	for i, status := range []string{"queued", "running", "waiting_dependencies", "waiting_approval", "waiting_question", "completed", "failed", "interrupted"} {
		id := fmt.Sprintf("plan-task-%d", i)
		b.entries[id] = &backgroundEntry{BackgroundTask: BackgroundTask{ID: id, SessionID: "session", GroupID: "plan", Status: status}}
	}
	b.entries["other-plan"] = &backgroundEntry{BackgroundTask: BackgroundTask{ID: "other-plan", SessionID: "session", GroupID: "other", Status: "running"}}
	b.entries["other-session"] = &backgroundEntry{BackgroundTask: BackgroundTask{ID: "other-session", SessionID: "previous", GroupID: "plan", Status: "running"}}
	b.mu.Unlock()

	host := runHost{runtime: rt}
	if got := host.PendingBackgroundPlan("plan"); got != 5 {
		t.Fatalf("active plan tasks = %d, want 5", got)
	}
	if got := host.PendingBackgroundPlan("missing"); got != 0 {
		t.Fatalf("unknown plan tasks = %d, want 0", got)
	}
}
