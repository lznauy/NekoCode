package runtime

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"nekocode/mode"
	"nekocode/protocol"
)

func backgroundServices(root string) ServiceGroups {
	return ServiceGroups{
		Modes:    ModesServices{Current: func() mode.ID { return mode.SuperAgent }, Set: func(mode.ID) error { return nil }, Options: mode.Definitions, Resolve: func(string) (mode.Snapshot, error) { d, err := mode.Resolve(mode.SuperAgent); return d.Snapshot, err }},
		Sessions: SessionsServices{CurrentSessionID: func() string { return "session" }, ListSessions: func() []SessionMeta { return nil }, SessionMessages: func() []DisplayMessage { return nil }, BackgroundStatePath: func(id string) string { return filepath.Join(root, id, "background.json") }},
	}
}
func awaitBackground(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("background operation timed out")
	}
}

func TestBackgroundDispatchReturnsAndCompletionWakesCoordinator(t *testing.T) {
	releaseA, releaseB := make(chan struct{}), make(chan struct{})
	startedA, startedB := make(chan struct{}), make(chan struct{})
	notices := make(chan string, 8)
	rt := New(RunnerFunc(func(ctx context.Context, input string, host RunHost) (string, error) {
		if protocol.CoordinationFromContext(ctx).Notification {
			notices <- input
			return "reviewed", nil
		}
		started, release := startedA, releaseA
		if input == "B" {
			started, release = startedB, releaseB
		}
		receipt, err := host.(protocol.BackgroundSubmitter).SubmitBackground(ctx, protocol.BackgroundWork{ID: input, SessionID: "session", GroupID: input, ReadOnly: true, Workspace: "workspace", Run: func(ctx context.Context, _ protocol.BackgroundHost) (string, error) {
			close(started)
			select {
			case <-release:
				return "result-" + input, nil
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}})
		if err != nil {
			return "", err
		}
		return "accepted " + receipt.ID, nil
	}), backgroundServices(t.TempDir()))
	defer rt.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	a, err := rt.StartRun(ctx, Input{Text: "A"})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.WaitRun(ctx, a); err != nil {
		t.Fatal(err)
	}
	awaitBackground(t, startedA)
	b, err := rt.StartRun(ctx, Input{Text: "B"})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.WaitRun(ctx, b); err != nil {
		t.Fatal(err)
	}
	awaitBackground(t, startedB)
	close(releaseB)
	select {
	case notice := <-notices:
		if !strings.Contains(notice, "result-B") || strings.Contains(notice, "result-A") {
			t.Fatalf("mixed task results: %s", notice)
		}
	case <-ctx.Done():
		t.Fatal("completion did not wake coordinator")
	}
	close(releaseA)
	select {
	case notice := <-notices:
		if !strings.Contains(notice, "result-A") {
			t.Fatal(notice)
		}
	case <-ctx.Done():
		t.Fatal("A completion did not wake coordinator")
	}
}

func TestBackgroundApprovalSurvivesForegroundAndOnlyPausesItsTask(t *testing.T) {
	requested := make(chan struct{})
	finished := make(chan struct{})
	var once sync.Once
	rt := New(RunnerFunc(func(ctx context.Context, input string, host RunHost) (string, error) {
		if input != "A" {
			return "other demand accepted", nil
		}
		_, err := host.(protocol.BackgroundSubmitter).SubmitBackground(ctx, protocol.BackgroundWork{ID: "approval-task", SessionID: "session", ReadOnly: true, Run: func(ctx context.Context, h protocol.BackgroundHost) (string, error) {
			reply := h.Confirm(protocol.ConfirmRequest{ToolName: "shell"})
			if !reply.Allowed {
				return "denied", nil
			}
			close(finished)
			return "approved", nil
		}})
		return "dispatched", err
	}), backgroundServices(t.TempDir()))
	defer rt.Close()
	subscription, err := rt.Events(t.Context(), EventFilter{})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for ev := range subscription {
			if ev.Type == EventApprovalRequested {
				once.Do(func() { close(requested) })
			}
		}
	}()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	a, err := rt.StartRun(ctx, Input{Text: "A"})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.WaitRun(ctx, a); err != nil {
		t.Fatal(err)
	}
	awaitBackground(t, requested)
	b, err := rt.StartRun(ctx, Input{Text: "B"})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.WaitRun(ctx, b); err != nil {
		t.Fatal(err)
	}
	approvals := rt.approvals.Pending()
	if len(approvals) != 1 {
		t.Fatalf("foreground completion cleared background approval: %+v", approvals)
	}
	if err := rt.DecideApproval(ctx, approvals[0].ID, ApprovalDecision{Allowed: true}); err != nil {
		t.Fatal(err)
	}
	awaitBackground(t, finished)
}

func TestBackgroundRecoveryDoesNotReplayExecution(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "session", "background.json")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	state := struct {
		Version int              `json:"version"`
		Tasks   []BackgroundTask `json:"tasks"`
	}{1, []BackgroundTask{{ID: "interrupted", SessionID: "session", Status: "running"}, {ID: "review", SessionID: "session", Status: "completed", Result: "saved evidence", Delivery: "claimed"}}}
	data, _ := json.Marshal(state)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	rt := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) { return "", nil }), backgroundServices(root))
	defer rt.Close()
	if err := rt.background.load("session"); err != nil {
		t.Fatal(err)
	}
	rt.background.mu.Lock()
	defer rt.background.mu.Unlock()
	if got := rt.background.entries["interrupted"].Status; got != "interrupted" {
		t.Fatal(got)
	}
	if got := rt.background.entries["review"].Delivery; got != "failed" {
		t.Fatalf("claimed review will be replayed: %s", got)
	}
	if rt.background.entries["interrupted"].work.Run != nil {
		t.Fatal("recovered an executable closure")
	}
}

func TestBackgroundQueuesNewDemandWhileCoordinatorIsRunning(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	bSeen := make(chan struct{})
	rt := New(RunnerFunc(func(ctx context.Context, input string, _ RunHost) (string, error) {
		if input == "A" {
			close(entered)
			select {
			case <-release:
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}
		if input == "B" {
			close(bSeen)
		}
		return input, nil
	}), backgroundServices(t.TempDir()))
	defer rt.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	a, err := rt.StartRun(ctx, Input{Text: "A"})
	if err != nil {
		t.Fatal(err)
	}
	awaitBackground(t, entered)
	queued, err := rt.StartRun(ctx, Input{Text: "B"})
	if err != nil {
		t.Fatal(err)
	}
	if queued == a {
		t.Fatal("new demand reused current run")
	}
	close(release)
	awaitBackground(t, bSeen)
	if err := rt.WaitRun(ctx, queued); err != nil {
		t.Fatal(err)
	}
}

func TestBackgroundQueuedWaitCancelAndMutationBoundary(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var bCalled bool
	rt := New(RunnerFunc(func(ctx context.Context, input string, _ RunHost) (string, error) {
		if input == "A" {
			close(entered)
			select {
			case <-release:
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}
		if input == "B" {
			bCalled = true
		}
		return input, nil
	}), backgroundServices(t.TempDir()))
	defer rt.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	a, err := rt.StartRun(ctx, Input{Text: "A"})
	if err != nil {
		t.Fatal(err)
	}
	awaitBackground(t, entered)
	queued, err := rt.StartRun(ctx, Input{Text: "B"})
	if err != nil {
		t.Fatal(err)
	}
	waitCtx, stop := context.WithTimeout(ctx, 10*time.Millisecond)
	defer stop()
	if err := rt.WaitRun(waitCtx, queued); err == nil {
		t.Fatal("queued run reported completion before execution")
	}
	// Hold the same lease a real scheduled coordinator uses while the frontend
	// sees idle; accepted inputs must still pin the session/mode boundary.
	rt.background.mu.Lock()
	if !(rt.background.pending[queued] != nil && rt.background.pending[queued].sessionID == "session") {
		t.Fatal("lost queued ownership")
	}
	rt.background.mu.Unlock()
	if err := rt.CancelRun(ctx, queued); err != nil {
		t.Fatal(err)
	}
	if err := rt.WaitRun(ctx, queued); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := rt.WaitRun(ctx, a); err != nil {
		t.Fatal(err)
	}
	if bCalled {
		t.Fatal("cancelled queued input executed")
	}
}

func TestBackgroundSharedWritersSerializeAndReadTasksProceed(t *testing.T) {
	started := make(chan string, 8)
	releases := map[string]chan struct{}{"write1": make(chan struct{}), "write2": make(chan struct{}), "read": make(chan struct{})}
	rt := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) { return "review", nil }), backgroundServices(t.TempDir()))
	defer rt.Close()
	for _, id := range []string{"write1", "write2", "read"} {
		name := id
		_, err := rt.background.submit(t.Context(), protocol.BackgroundWork{ID: id, SessionID: "session", GroupID: id, Workspace: "shared", ReadOnly: id == "read", Run: func(ctx context.Context, _ protocol.BackgroundHost) (string, error) {
			started <- name
			select {
			case <-releases[name]:
				return name, nil
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}})
		if err != nil {
			t.Fatal(err)
		}
	}
	seen := map[string]bool{}
	for len(seen) < 2 {
		select {
		case id := <-started:
			seen[id] = true
		case <-time.After(5 * time.Second):
			t.Fatal("independent read task was blocked")
		}
	}
	if !seen["read"] || (seen["write1"] && seen["write2"]) {
		t.Fatalf("workspace writers overlapped: %v", seen)
	}
	first := "write1"
	second := "write2"
	if seen["write2"] {
		first, second = second, first
	}
	close(releases[first])
	select {
	case id := <-started:
		if id != second {
			t.Fatal(id)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("queued writer did not start")
	}
	close(releases[second])
	close(releases["read"])
}

func TestBackgroundCloseCancelsTaskWaitingForApproval(t *testing.T) {
	rt := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) { return "", nil }), backgroundServices(t.TempDir()))
	requested := make(chan struct{})
	exited := make(chan struct{})
	_, err := rt.background.submit(t.Context(), protocol.BackgroundWork{ID: "approval-close", SessionID: "session", ReadOnly: true, Run: func(ctx context.Context, h protocol.BackgroundHost) (string, error) {
		defer close(exited)
		close(requested)
		h.Confirm(protocol.ConfirmRequest{ToolName: "shell"})
		return "", ctx.Err()
	}})
	if err != nil {
		t.Fatal(err)
	}
	awaitBackground(t, requested)
	done := make(chan struct{})
	go func() { _ = rt.Close(); close(done) }()
	awaitBackground(t, done)
	awaitBackground(t, exited)
}

func TestBackgroundCancellationPreservesAttemptIdentity(t *testing.T) {
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
		host.Step(protocol.StepEvent{Action: protocol.StepActionSubAgentStart, SubAgentAttemptID: "attempt", SubAgentAttemptSeq: 3})
		close(started)
		<-ctx.Done()
		return "", ctx.Err()
	}})
	if err != nil {
		t.Fatal(err)
	}
	awaitBackground(t, started)
	if err := rt.CancelBackgroundTask(ctx, "session", "task"); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-events:
		payload, ok := event.Payload.(SubAgentPayload)
		if !ok || payload.AttemptID != "attempt" || payload.AttemptSeq != 3 || payload.Status != "interrupted" {
			t.Fatalf("lost cancellation ownership: %+v", event.Payload)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}
