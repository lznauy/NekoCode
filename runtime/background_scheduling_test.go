package runtime

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"nekocode/protocol"
)

func TestBackgroundConfiguredLimitAndIndependentWorkspaces(t *testing.T) {
	services := backgroundServices(t.TempDir())
	services.Sessions.Subagents = func(string) ([]SubagentSummary, error) { return nil, nil }
	var limit atomic.Int32
	limit.Store(2)
	services.Config.ConfigView = func() ConfigView { return ConfigView{MaxConcurrentSubagents: int(limit.Load())} }
	services.Config.ResolveModelProfile = func(ModelSpec) ModelProfile { return ModelProfile{} }
	services.Config.ApplyConfig = func(view ConfigView) (ConfigView, error) {
		limit.Store(int32(view.MaxConcurrentSubagents))
		return view, nil
	}
	rt := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) { return "", nil }), services)
	defer rt.Close()
	started := make(chan string, 3)
	release := make(chan struct{})
	for _, id := range []string{"A", "B", "C"} {
		id := id
		_, err := rt.background.submit(t.Context(), protocol.BackgroundWork{ID: id, SessionID: "session", Isolation: "worktree", Workspace: "repo#worktree:" + id, Run: func(ctx context.Context, _ protocol.BackgroundHost) (string, error) {
			started <- id
			select {
			case <-release:
				return "done", nil
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}})
		if err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatal("independent writers did not overlap")
		}
	}
	task := waitBackgroundStatus(t, rt, "C", "queued")
	if task.QueueReason != "concurrency_limit" {
		t.Fatalf("queue reason: %+v", task)
	}
	page, err := rt.Subagents(SubagentFilter{SessionID: "session", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range page.Items {
		if item.ID == "C" && (item.QueueReason != "concurrency_limit" || item.Isolation != "worktree") {
			t.Fatalf("projection lost scheduling: %+v", item)
		}
	}
	select {
	case id := <-started:
		t.Fatalf("configured limit exceeded: %s", id)
	default:
	}
	// A larger limit admits queued work without restarting existing workers.
	limit.Store(3)
	rt.background.signal()
	select {
	case id := <-started:
		if id != "C" {
			t.Fatal(id)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("new limit did not wake queued task")
	}
	close(release)
}

func TestBackgroundQueueReasons(t *testing.T) {
	b := &backgroundTasks{workspaces: map[string]bool{"shared": true}, active: 1}
	for _, tc := range []struct {
		status, workspace  string
		reader, foreground bool
		want               string
	}{
		{"waiting_dependencies", "shared", false, true, "waiting_dependencies"},
		{"queued", "shared", false, true, "foreground_writer"},
		{"queued", "shared", false, false, "workspace_busy"},
		{"queued", "separate", false, false, "concurrency_limit"},
		{"queued", "shared", true, true, "concurrency_limit"},
		{"running", "shared", false, true, ""},
	} {
		e := &backgroundEntry{BackgroundTask: BackgroundTask{Status: tc.status}, work: protocol.BackgroundWork{Workspace: tc.workspace, ReadOnly: tc.reader}}
		if got := b.queueReasonLocked(e, 1, tc.foreground); got != tc.want {
			t.Fatalf("%+v: %q", tc, got)
		}
	}
}

func TestWorktreeApplyCannotBypassActiveBackgroundBarrier(t *testing.T) {
	services := backgroundServices(t.TempDir())
	var commandCalls atomic.Int32
	services.Commands.ExecuteCommand = func(context.Context, string, RunHost) (CommandResult, error) {
		commandCalls.Add(1)
		return CommandResult{Action: CommandHandled}, nil
	}
	services.Commands.CommandMenu = func(context.Context, string) (CommandMenu, bool) { return CommandMenu{}, false }
	rt := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) { return "", nil }), services)
	defer rt.Close()
	started := make(chan struct{})
	_, err := rt.background.submit(t.Context(), protocol.BackgroundWork{ID: "writer", SessionID: "session", Workspace: "shared", Run: func(ctx context.Context, _ protocol.BackgroundHost) (string, error) {
		close(started)
		<-ctx.Done()
		return "", ctx.Err()
	}})
	if err != nil {
		t.Fatal(err)
	}
	awaitBackground(t, started)
	id, err := rt.StartRun(t.Context(), Input{Text: "/worktree apply completed-task"})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.WaitRun(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	view, ok := rt.LookupRun(id)
	if !ok || view.Status != RunFailed || commandCalls.Load() != 0 {
		t.Fatalf("apply bypassed background barrier: %+v calls=%d", view, commandCalls.Load())
	}
}
