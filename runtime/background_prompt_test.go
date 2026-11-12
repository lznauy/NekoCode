package runtime

import (
	"context"
	"encoding/json"
	"nekocode/protocol"
	"strings"
	"testing"
	"time"
)

func TestQueuedBackgroundPromptIsVisibleAndDurable(t *testing.T) {
	root := t.TempDir()
	services := backgroundServices(root)
	services.Sessions.Subagents = func(string) ([]SubagentSummary, error) { return nil, nil }
	rt := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) { return "", nil }), services)
	prompt := "Inspect these files:\n" + strings.Repeat("details and constraints\n", 100) + "PROMPT_END"
	b := rt.background
	b.mu.Lock()
	b.workspaces["occupied"] = true
	b.mu.Unlock()
	_, err := b.submit(t.Context(), protocol.BackgroundWork{ID: "queued-task", SessionID: "session", Workspace: "occupied", Profile: "executor", Prompt: prompt, Run: func(context.Context, protocol.BackgroundHost) (string, error) {
		t.Error("queued task executed before slot available")
		return "", nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	page, err := rt.Subagents(SubagentFilter{SessionID: "session", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].Status != "queued" || !strings.Contains(page.Items[0].Prompt, "Inspect these files") {
		t.Fatalf("queued row lost prompt: %+v", page)
	}
	full, err := rt.BackgroundTaskPrompt("session", "queued-task")
	if err != nil || full != prompt {
		t.Fatalf("full prompt unavailable: %q %v", full, err)
	}
	if other, err := rt.BackgroundTaskPrompt("other", "queued-task"); err != nil || other != "" {
		t.Fatal("prompt leaked across sessions")
	}
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}
	restored := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) { return "", nil }), services)
	defer restored.Close()
	full, err = restored.BackgroundTaskPrompt("session", "queued-task")
	if err != nil || full != prompt {
		t.Fatalf("accepted prompt lost after restart: %q %v", full, err)
	}
}

func TestQueuedResumeUsesNewPromptRatherThanPreviousAttempt(t *testing.T) {
	services := backgroundServices(t.TempDir())
	services.Sessions.Subagents = func(string) ([]SubagentSummary, error) {
		return []SubagentSummary{{ID: "task", Recorded: true, Status: "completed", Prompt: "old instruction", LatestAttempt: &SubagentAttemptSummary{ID: "old-attempt", Seq: 1, Status: "completed", Prompt: "old instruction"}}}, nil
	}
	rt := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) { return "", nil }), services)
	defer rt.Close()
	rt.background.mu.Lock()
	rt.background.workspaces["occupied"] = true
	rt.background.mu.Unlock()
	_, err := rt.background.submit(t.Context(), protocol.BackgroundWork{ID: "task", SessionID: "session", Workspace: "occupied", Prompt: "new queued instruction", Run: func(context.Context, protocol.BackgroundHost) (string, error) { return "", nil }})
	if err != nil {
		t.Fatal(err)
	}
	page, err := rt.Subagents(SubagentFilter{SessionID: "session", Limit: 10})
	if err != nil || len(page.Items) != 1 || page.Items[0].Prompt != "new queued instruction" {
		t.Fatalf("previous instruction shown for queued resume: %+v %v", page, err)
	}
}

func TestInterruptedTaskReviewExplainsHowToContinueExistingTask(t *testing.T) {
	notices := make(chan string, 1)
	rt := New(RunnerFunc(func(ctx context.Context, input string, _ RunHost) (string, error) {
		if protocol.CoordinationFromContext(ctx).Notification {
			notices <- input
		}
		return "reviewed", nil
	}), backgroundServices(t.TempDir()))
	defer rt.Close()
	_, err := rt.background.submit(t.Context(), protocol.BackgroundWork{ID: "saved-executor", SessionID: "session", GroupID: "original-demand", Profile: "executor", Run: func(ctx context.Context, _ protocol.BackgroundHost) (string, error) {
		<-ctx.Done()
		return "partial work", ctx.Err()
	}})
	if err != nil {
		t.Fatal(err)
	}
	waitBackgroundStatus(t, rt, "saved-executor", "running")
	if err := rt.CancelBackgroundTask(t.Context(), "session", "saved-executor"); err != nil {
		t.Fatal(err)
	}
	select {
	case input := <-notices:
		for _, hint := range []string{"saved-executor", "resume_id", "saved context", "user asks to continue"} {
			if !strings.Contains(input, hint) {
				t.Errorf("interruption notice missing recovery guidance %q: %s", hint, input)
			}
		}
	case <-time.After(5 * time.Second):
		t.Fatal("missing interruption review")
	}
}

func TestBackgroundReviewIncludesReportTailAndOutstandingSibling(t *testing.T) {
	type delivery struct {
		text         string
		coordination protocol.Coordination
	}
	notices := make(chan delivery, 4)
	rt := New(RunnerFunc(func(ctx context.Context, input string, _ RunHost) (string, error) {
		notices <- delivery{input, protocol.CoordinationFromContext(ctx)}
		return "reviewed", nil
	}), backgroundServices(t.TempDir()))
	defer rt.Close()
	startedA, startedB, releaseA, releaseB := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	body := strings.Repeat("完整报告证据\n", 1000) + "TAIL_FINDING_MUST_NOT_DISAPPEAR"
	for _, id := range []string{"A", "B"} {
		started, release := startedA, releaseA
		if id == "B" {
			started, release = startedB, releaseB
		}
		_, err := rt.background.submit(t.Context(), protocol.BackgroundWork{ID: id, SessionID: "session", GroupID: "review-group", ReadOnly: true, Run: func(ctx context.Context, _ protocol.BackgroundHost) (string, error) {
			close(started)
			select {
			case <-release:
				return body, nil
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}})
		if err != nil {
			t.Fatal(err)
		}
	}
	awaitBackground(t, startedA)
	awaitBackground(t, startedB)
	close(releaseA)
	select {
	case delivered := <-notices:
		input := delivered.text
		if delivered.coordination.GroupID != "review-group" || !delivered.coordination.Notification || delivered.coordination.PendingTasks != 1 {
			t.Fatalf("untrusted or wrong pending-task scope: %+v", delivered.coordination)
		}
		if !strings.Contains(input, "TAIL_FINDING_MUST_NOT_DISAPPEAR") {
			t.Error("report tail missing from coordinator handoff")
		}
		var notice struct {
			Results []struct {
				ID       string `json:"id"`
				Result   string `json:"result"`
				Complete bool   `json:"result_complete"`
			} `json:"results"`
			Tasks []struct {
				ID     string `json:"id"`
				Status string `json:"status"`
			} `json:"group_tasks"`
		}
		start := strings.Index(input, "\n{")
		if start < 0 {
			t.Fatal("missing structured coordination inventory")
		}
		if err := json.Unmarshal([]byte(input[start+1:]), &notice); err != nil {
			t.Fatal(err)
		}
		if len(notice.Results) != 1 || !notice.Results[0].Complete || notice.Results[0].Result != body {
			t.Fatalf("report not delivered in full: %+v", notice.Results)
		}
		found := false
		for _, item := range notice.Tasks {
			if item.ID == "B" && item.Status == "running" {
				found = true
			}
		}
		if !found {
			t.Fatalf("unfinished sibling omitted: %+v", notice.Tasks)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("missing review")
	}
	close(releaseB)
}
