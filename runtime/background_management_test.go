package runtime

import (
	"context"
	"errors"
	"nekocode/mode"
	"nekocode/protocol"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func waitBackgroundStatus(t *testing.T, rt *Runtime, id, status string) BackgroundTask {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		tasks, err := rt.BackgroundTasks("session")
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range tasks {
			if item.ID == id && item.Status == status {
				return item
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("task %s did not become %s", id, status)
	return BackgroundTask{}
}
func TestBackgroundDependenciesDoNotOccupySlotsAndPinUpstream(t *testing.T) {
	services := backgroundServices(t.TempDir())
	services.Modes.Current = func() mode.ID { return mode.Standard }
	rt := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) { return "", nil }), services)
	defer rt.Close()
	release := make(chan struct{})
	defer close(release)
	_, err := rt.background.submit(t.Context(), protocol.BackgroundWork{ID: "A", SessionID: "session", ReadOnly: true, Run: func(ctx context.Context, _ protocol.BackgroundHost) (string, error) {
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
	var ran atomic.Int32
	for _, id := range []string{"B", "C"} {
		_, err = rt.background.submit(t.Context(), protocol.BackgroundWork{ID: id, SessionID: "session", ReadOnly: true, After: []string{"A"}, Run: func(context.Context, protocol.BackgroundHost) (string, error) { ran.Add(1); return "done", nil }})
		if err != nil {
			t.Fatal(err)
		}
	}
	waitBackgroundStatus(t, rt, "A", "running")
	waitBackgroundStatus(t, rt, "B", "waiting_dependencies")
	rt.background.mu.Lock()
	active := rt.background.active
	rt.background.mu.Unlock()
	if active != 1 {
		t.Fatalf("waiting tasks occupied slots: %d", active)
	}
	if _, err := rt.background.submit(t.Context(), protocol.BackgroundWork{ID: "A", SessionID: "session", After: []string{"B"}, Run: func(context.Context, protocol.BackgroundHost) (string, error) { return "", nil }}); err == nil {
		t.Fatal("cycle/resume of leased dependency accepted")
	}
	if err := rt.CancelBackgroundTask(t.Context(), "session", "A"); err != nil {
		t.Fatal(err)
	}
	waitBackgroundStatus(t, rt, "A", "interrupted")
	waitBackgroundStatus(t, rt, "B", "blocked")
	waitBackgroundStatus(t, rt, "C", "blocked")
	if ran.Load() != 0 {
		t.Fatal("dependent ran after upstream cancellation")
	}
}
func TestBackgroundDependenciesRunAfterSuccess(t *testing.T) {
	services := backgroundServices(t.TempDir())
	services.Modes.Current = func() mode.ID { return mode.Standard }
	rt := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) { return "", nil }), services)
	defer rt.Close()
	release := make(chan struct{})
	done := make(chan struct{})
	_, err := rt.background.submit(t.Context(), protocol.BackgroundWork{ID: "A", SessionID: "session", ReadOnly: true, Run: func(context.Context, protocol.BackgroundHost) (string, error) { <-release; return "ready", nil }})
	if err != nil {
		t.Fatal(err)
	}
	_, err = rt.background.submit(t.Context(), protocol.BackgroundWork{ID: "B", SessionID: "session", ReadOnly: true, After: []string{"A"}, Run: func(context.Context, protocol.BackgroundHost) (string, error) { close(done); return "done", nil }})
	if err != nil {
		t.Fatal(err)
	}
	close(release)
	awaitBackground(t, done)
	waitBackgroundStatus(t, rt, "B", "completed")
}
func TestRetryBackgroundReviewDoesNotReplayWorker(t *testing.T) {
	var reviews, workers atomic.Int32
	rt := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) {
		if reviews.Add(1) == 1 {
			return "", errors.New("model unavailable")
		}
		return "reviewed", nil
	}), backgroundServices(t.TempDir()))
	defer rt.Close()
	_, err := rt.background.submit(t.Context(), protocol.BackgroundWork{ID: "A", SessionID: "session", ReadOnly: true, Run: func(context.Context, protocol.BackgroundHost) (string, error) { workers.Add(1); return "evidence", nil }})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		tasks, _ := rt.BackgroundTasks("session")
		if len(tasks) > 0 && tasks[0].Delivery == "failed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no failed receipt")
		}
		time.Sleep(time.Millisecond)
	}
	if err := rt.RetryBackgroundNotification(t.Context(), "other", "A"); err == nil {
		t.Fatal("cross-session retry accepted")
	}
	if err := rt.RetryBackgroundNotification(t.Context(), "session", "A"); err != nil {
		t.Fatal(err)
	}
	for {
		tasks, _ := rt.BackgroundTasks("session")
		if tasks[0].Notified {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("retry not delivered")
		}
		time.Sleep(time.Millisecond)
	}
	if workers.Load() != 1 || reviews.Load() != 2 {
		t.Fatalf("workers=%d reviews=%d", workers.Load(), reviews.Load())
	}
}
func TestBackgroundKeepsOriginOnSessionSwitch(t *testing.T) {
	var selected atomic.Value
	selected.Store("session")
	root := t.TempDir()
	services := backgroundServices(root)
	services.Sessions.CurrentSessionID = func() string { return selected.Load().(string) }
	services.Sessions.ResumeSession = func(id string) error { selected.Store(id); return nil }
	services.Models.SwitchModel = func(string) (ModelSelection, error) { return ModelSelection{}, nil }
	services.Skills.RefreshSkillManagement = func() SkillManagementView { return SkillManagementView{} }
	notices := make(chan string, 8)
	rt := New(RunnerFunc(func(_ context.Context, text string, _ RunHost) (string, error) {
		notices <- text
		return "reviewed", nil
	}), services)
	defer rt.Close()
	if err := rt.EnableEventRecording(filepath.Join(root, "recordings")); err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	started := make(chan struct{})
	_, err := rt.background.submit(t.Context(), protocol.BackgroundWork{ID: "A", SessionID: "session", ReadOnly: true, Run: func(ctx context.Context, h protocol.BackgroundHost) (string, error) {
		h.Step(protocol.StepEvent{Action: protocol.StepActionSubAgentStart, SubAgentAttemptID: "attempt", SubAgentStatus: "running"})
		close(started)
		<-release
		h.(protocol.BackgroundUsageSink).Usage(protocol.BackgroundUsage{TotalTokens: 12})
		return "origin evidence", nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	awaitBackground(t, started)
	if err := rt.ResumeSession("other"); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.SwitchModel("new-model"); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.RefreshSkillManagement(); err == nil {
		t.Fatal("destructive resource refresh bypassed lease")
	}
	close(release)
	waitBackgroundStatus(t, rt, "A", "completed")
	select {
	case <-notices:
		t.Fatal("old result injected into selected session")
	default:
	}
	usage, err := rt.BackgroundUsage("session")
	if err != nil || usage.TotalTokens != 12 {
		t.Fatalf("usage %+v %v", usage, err)
	}
	other, _ := rt.BackgroundUsage("other")
	if other.TotalTokens != 0 {
		t.Fatal("usage leaked")
	}
	entries, _ := os.ReadDir(filepath.Join(root, "recordings", "session", "runs"))
	if len(entries) == 0 {
		t.Fatal("worker events not archived in original session")
	}
	if err := rt.ResumeSession("session"); err != nil {
		t.Fatal(err)
	}
	select {
	case text := <-notices:
		if !strings.Contains(text, "origin evidence") {
			t.Fatal(text)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("returning to origin did not wake review")
	}
}

func TestBackgroundUsageCheckpointRecoveryAndRetiredTotals(t *testing.T) {
	root := t.TempDir()
	services := backgroundServices(root)
	services.Modes.Current = func() mode.ID { return mode.Standard }
	usageSaved, release := make(chan struct{}), make(chan struct{})
	rt := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) { return "", nil }), services)
	_, err := rt.background.submit(t.Context(), protocol.BackgroundWork{ID: "usage", SessionID: "session", ReadOnly: true, Run: func(ctx context.Context, h protocol.BackgroundHost) (string, error) {
		sink := h.(protocol.BackgroundUsageSink)
		if err := sink.Usage(protocol.BackgroundUsage{TotalTokens: 15, ToolUseCount: 2}); err != nil {
			return "", err
		}
		if err := sink.Usage(protocol.BackgroundUsage{TotalTokens: 20, ToolUseCount: 3}); err != nil {
			return "", err
		}
		close(usageSaved)
		select {
		case <-release:
			return "done", nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}})
	if err != nil {
		rt.Close()
		t.Fatal(err)
	}
	awaitBackground(t, usageSaved)
	raw, err := os.ReadFile(filepath.Join(root, "session", "background.json"))
	if err != nil {
		t.Fatal(err)
	}
	// Recover a copy of the committed state as if the worker process died now.
	copied := t.TempDir()
	if err := os.MkdirAll(filepath.Join(copied, "session"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(copied, "session", "background.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	recoveredServices := backgroundServices(copied)
	recoveredServices.Modes.Current = func() mode.ID { return mode.Standard }
	recoveredServices.Sessions.SubagentUsage = func(string, string) (BackgroundUsage, error) {
		return BackgroundUsage{TotalTokens: 22, ToolUseCount: 3}, nil
	}
	recovered := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) { return "", nil }), recoveredServices)
	defer recovered.Close()
	total, err := recovered.BackgroundUsage("session")
	if err != nil || total.TotalTokens != 22 {
		t.Fatalf("canonical recovery total %+v %v", total, err)
	}
	close(release)
	waitBackgroundStatus(t, rt, "usage", "completed")
	rt.background.mu.Lock()
	e := rt.background.entries["usage"]
	rt.background.retireUsageLocked(e)
	delete(rt.background.entries, "usage")
	rt.background.order = nil
	err = rt.background.saveLocked("session")
	rt.background.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	total, err = rt.BackgroundUsage("session")
	if err != nil || total.TotalTokens != 20 || total.ToolUseCount != 3 {
		t.Fatalf("checkpoint counted twice or retired use lost %+v %v", total, err)
	}
	rt.Close()
	again := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) { return "", nil }), services)
	defer again.Close()
	total, err = again.BackgroundUsage("session")
	if err != nil || total.TotalTokens != 20 {
		t.Fatalf("retired use lost after restart %+v %v", total, err)
	}
}

func TestTaskListLoadsAdmissionReceiptsWithoutBackgroundPanel(t *testing.T) {
	root := t.TempDir()
	services := backgroundServices(root)
	services.Modes.Current = func() mode.ID { return mode.Standard }
	services.Sessions.Subagents = func(string) ([]SubagentSummary, error) { return nil, nil }
	path := filepath.Join(root, "session", "background.json")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"version":2,"tasks":[{"id":"queued-before-exit","session_id":"session","status":"waiting_dependencies","after":["upstream"]}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	rt := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) { return "", nil }), services)
	defer rt.Close()
	page, err := rt.Subagents(SubagentFilter{SessionID: "session", Limit: 20})
	if err != nil || page.Total != 1 || page.Items[0].Status != "interrupted" {
		t.Fatalf("admission receipt missing after restart: %+v %v", page, err)
	}
}

func TestBackgroundReleasesCapturedResourcesForUnstartedTasks(t *testing.T) {
	for _, action := range []string{"cancel", "stop", "close", "blocked"} {
		t.Run(action, func(t *testing.T) {
			services := backgroundServices(t.TempDir())
			services.Modes.Current = func() mode.ID { return mode.Standard }
			rt := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) { return "", nil }), services)
			var released atomic.Int32
			rt.background.mu.Lock()
			rt.background.entries["upstream"] = &backgroundEntry{BackgroundTask: BackgroundTask{ID: "upstream", SessionID: "session", Status: "running"}}
			rt.background.order = append(rt.background.order, "upstream")
			rt.background.mu.Unlock()
			_, err := rt.background.submit(t.Context(), protocol.BackgroundWork{ID: "dependent", SessionID: "session", ReadOnly: true, After: []string{"upstream"}, Release: func() { released.Add(1) }, Run: func(context.Context, protocol.BackgroundHost) (string, error) {
				t.Error("unstarted task ran")
				return "", nil
			}})
			if err != nil {
				rt.Close()
				t.Fatal(err)
			}
			switch action {
			case "cancel":
				err = rt.CancelBackgroundTask(t.Context(), "session", "dependent")
			case "stop":
				err = rt.StopBackgroundTasks(t.Context(), "session")
			case "close":
				err = rt.Close()
			case "blocked":
				rt.background.mu.Lock()
				rt.background.entries["upstream"].Status = "failed"
				rt.background.mu.Unlock()
				rt.background.signal()
				waitBackgroundStatus(t, rt, "dependent", "blocked")
			}
			if err != nil {
				t.Fatal(err)
			}
			rt.Close()
			if released.Load() != 1 {
				t.Fatalf("resource released %d times", released.Load())
			}
		})
	}
}

func TestRetiredResumeUsageDoesNotDoubleAfterRestart(t *testing.T) {
	root := t.TempDir()
	services := backgroundServices(root)
	services.Modes.Current = func() mode.ID { return mode.Standard }
	rt := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) { return "", nil }), services)
	if err := rt.background.load("session"); err != nil {
		t.Fatal(err)
	}
	rt.background.mu.Lock()
	rt.background.retireUsageLocked(&backgroundEntry{BackgroundTask: BackgroundTask{ID: "resumed", SessionID: "session", Usage: BackgroundUsage{TotalTokens: 20, ToolUseCount: 2}}})
	rt.background.entries["resumed"] = &backgroundEntry{BackgroundTask: BackgroundTask{ID: "resumed", SessionID: "session", Status: "completed", Usage: BackgroundUsage{TotalTokens: 10, ToolUseCount: 1}}}
	rt.background.order = []string{"resumed"}
	err := rt.background.saveLocked("session")
	rt.background.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	rt.Close()
	services.Sessions.SubagentUsage = func(string, string) (BackgroundUsage, error) {
		return BackgroundUsage{TotalTokens: 30, ToolUseCount: 3}, nil
	}
	recovered := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) { return "", nil }), services)
	defer recovered.Close()
	total, err := recovered.BackgroundUsage("session")
	if err != nil || total.TotalTokens != 30 || total.ToolUseCount != 3 {
		t.Fatalf("double counted retired attempt: %+v %v", total, err)
	}

	// Retire the resumed attempt again, then recover a newer canonical checkpoint.
	recovered.background.mu.Lock()
	recovered.background.retireUsageLocked(recovered.background.entries["resumed"])
	recovered.background.entries["resumed"].Usage = BackgroundUsage{TotalTokens: 5, ToolUseCount: 1}
	err = recovered.background.saveLocked("session")
	recovered.background.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	recovered.Close()
	services.Sessions.SubagentUsage = func(string, string) (BackgroundUsage, error) {
		return BackgroundUsage{TotalTokens: 37, ToolUseCount: 4}, nil
	}
	again := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) { return "", nil }), services)
	defer again.Close()
	total, err = again.BackgroundUsage("session")
	if err != nil || total.TotalTokens != 37 || total.ToolUseCount != 4 {
		t.Fatalf("repeated retirement lost canonical checkpoint: %+v %v", total, err)
	}
}
