package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"nekocode/mode"
	"nekocode/protocol"
)

func TestBackgroundCanonicalReportSurvivesPreviewRestartAndResume(t *testing.T) {
	services := backgroundServices(t.TempDir())
	services.Sessions.DeleteSubagent = func(string, string) error { return nil }
	services.Modes.Current = func() mode.ID { return mode.Standard }
	rt := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) { return "", nil }), services)
	report := cleanReviewFixture()
	report.Evidence = []string{strings.Repeat("完整证据;", 12000)}
	report.Risks = []string{"preserve this risk"}
	_, err := rt.background.submit(t.Context(), protocol.BackgroundWork{ID: "task", SessionID: "session", ReadOnly: true, Run: func(context.Context, protocol.BackgroundHost) (protocol.BackgroundResult, error) {
		// Deliberately supply no formatted copy of the structured evidence.
		return protocol.BackgroundResult{Text: "unrelated display text", Review: &report}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	waitBackgroundStatus(t, rt, "task", "completed")
	tasks, _ := rt.BackgroundTasks("session")
	ref, attempt := tasks[0].ResultRef, tasks[0].AttemptID
	rt.background.mu.Lock()
	notice := rt.background.reviewLocked("session", "", []string{"task"}).Results[0]
	rt.background.mu.Unlock()
	if notice.ReviewComplete || notice.ResultComplete || notice.ReadResult == nil || notice.ReadResult.Ref != ref {
		t.Fatalf("oversized evidence lost its exact read reference: %+v", notice)
	}
	state, err := os.ReadFile(services.Sessions.BackgroundStatePath("session"))
	if err != nil {
		t.Fatal(err)
	}
	if len(state) > 40<<10 {
		t.Fatalf("full report duplicated into mutable state: %d", len(state))
	}
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}
	recovered := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) { return "", nil }), services)
	defer recovered.Close()
	var text strings.Builder
	q := protocol.ResultQuery{ID: "task", AttemptID: attempt, Ref: ref, Limit: 2000}
	for {
		page, err := recovered.readBackgroundResult(t.Context(), "session", q)
		if err != nil {
			t.Fatal(err)
		}
		text.WriteString(page.Content)
		if !page.HasMore {
			break
		}
		q.Offset = page.NextOffset
	}
	var got protocol.TaskReview
	if err := json.Unmarshal([]byte(text.String()), &got); err != nil {
		t.Fatal(err)
	}
	if got.Evidence[0] != report.Evidence[0] || got.Risks[0] != report.Risks[0] || !reflect.DeepEqual(got.Checks, report.Checks) {
		t.Fatal("canonical evidence changed")
	}
	recovered.background.mu.Lock()
	recovered.background.entries["task"].Notified = true
	recovered.background.entries["task"].Delivery = "delivered"
	recovered.background.mu.Unlock()
	// A new attempt never overwrites a previously issued evidence reference.
	_, err = recovered.background.submit(t.Context(), protocol.BackgroundWork{ID: "task", SessionID: "session", ReadOnly: true, Run: func(context.Context, protocol.BackgroundHost) (protocol.BackgroundResult, error) {
		return protocol.BackgroundResult{Text: "new attempt"}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	waitBackgroundStatus(t, recovered, "task", "completed")
	if _, err := recovered.readBackgroundResult(t.Context(), "session", protocol.ResultQuery{ID: "task", AttemptID: attempt, Limit: 20}); err != nil {
		t.Fatal("historical attempt lookup:", err)
	}
	// Scheduler receipt eviction does not revoke evidence or require a child session.
	recovered.background.mu.Lock()
	delete(recovered.background.entries, "task")
	recovered.background.order = nil
	recovered.background.mu.Unlock()
	if _, err := recovered.readBackgroundResult(t.Context(), "session", protocol.ResultQuery{Ref: ref, Limit: 20}); err != nil {
		t.Fatal("evicted receipt invalidated report:", err)
	}
	if _, err := recovered.readBackgroundResult(t.Context(), "session", protocol.ResultQuery{ID: "task", AttemptID: attempt, Limit: 20}); err != nil {
		t.Fatal("evicted receipt invalidated attempt lookup:", err)
	}
	if _, err := recovered.readBackgroundResult(t.Context(), "other-parent", protocol.ResultQuery{Ref: ref, Limit: 20}); err == nil {
		t.Fatal("cross-parent reference accepted")
	}
	if err := recovered.DeleteSubagent(t.Context(), "session", "task"); err != nil {
		t.Fatal(err)
	}
	if _, err := recovered.readBackgroundResult(t.Context(), "session", protocol.ResultQuery{Ref: ref, Limit: 20}); err == nil {
		t.Fatal("explicit deletion retained canonical evidence")
	}
}

func TestBackgroundResultCommitFailureCannotCompleteDependencies(t *testing.T) {
	root := t.TempDir()
	services := backgroundServices(root)
	services.Modes.Current = func() mode.ID { return mode.Standard }
	rt := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) { return "", nil }), services)
	defer rt.Close()
	if err := rt.background.load("session"); err != nil {
		t.Fatal(err)
	}
	// Prevent creation of the canonical result directory while state remains writable.
	if err := os.WriteFile(filepath.Join(root, "session", "background-results"), []byte("blocked"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := rt.background.submit(t.Context(), protocol.BackgroundWork{ID: "task", SessionID: "session", ReadOnly: true, Run: func(context.Context, protocol.BackgroundHost) (protocol.BackgroundResult, error) {
		report := cleanReviewFixture()
		return protocol.BackgroundResult{Review: &report}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	waitBackgroundStatus(t, rt, "task", "failed")
	tasks, _ := rt.BackgroundTasks("session")
	if _, err := rt.readBackgroundResult(t.Context(), "session", protocol.ResultQuery{ID: "task", Limit: 20}); err == nil || errors.Is(err, protocol.ErrBackgroundResultNotFound) {
		t.Fatalf("commit failure allowed legacy fallback: %v", err)
	}
	if tasks[0].ResultRef != "" || !strings.Contains(tasks[0].Error, "persist background result") {
		t.Fatalf("failed commit reported success: %+v", tasks[0])
	}
}

func TestPendingBackgroundResultCannotFallBackToEarlierText(t *testing.T) {
	services := backgroundServices(t.TempDir())
	services.Modes.Current = func() mode.ID { return mode.Standard }
	rt := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) { return "", nil }), services)
	defer rt.Close()
	ready := make(chan struct{})
	_, err := rt.background.submit(t.Context(), protocol.BackgroundWork{ID: "task", SessionID: "session", ReadOnly: true, Run: func(ctx context.Context, _ protocol.BackgroundHost) (protocol.BackgroundResult, error) {
		close(ready)
		<-ctx.Done()
		return protocol.BackgroundResult{}, ctx.Err()
	}})
	if err != nil {
		t.Fatal(err)
	}
	awaitBackground(t, ready)
	_, err = rt.readBackgroundResult(t.Context(), "session", protocol.ResultQuery{ID: "task", Limit: 20})
	var pending *protocol.ResultNotReadyError
	if !errors.As(err, &pending) {
		t.Fatalf("active result permitted fallback or became a read failure: %v", err)
	}
}

func TestReceiptPersistenceFailureDoesNotRewriteCommittedOutcome(t *testing.T) {
	services := backgroundServices(t.TempDir())
	services.Modes.Current = func() mode.ID { return mode.Standard }
	rt := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) { return "", nil }), services)
	defer rt.Close()
	_, err := rt.background.submit(t.Context(), protocol.BackgroundWork{ID: "task", SessionID: "session", ReadOnly: true, Run: func(context.Context, protocol.BackgroundHost) (protocol.BackgroundResult, error) {
		// Keep the canonical result directory writable, but prevent replacement of
		// the mutable receipt. The execution itself still has a durable result.
		path := services.Sessions.BackgroundStatePath("session")
		if err := os.Remove(path); err != nil {
			return protocol.BackgroundResult{}, err
		}
		if err := os.Mkdir(path, 0700); err != nil {
			return protocol.BackgroundResult{}, err
		}
		return protocol.BackgroundResult{Text: "committed evidence"}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	waitBackgroundStatus(t, rt, "task", "completed")
	tasks, _ := rt.BackgroundTasks("session")
	if tasks[0].Delivery != "failed" || tasks[0].ResultRef == "" || !strings.Contains(tasks[0].DeliveryError, "receipt persistence failed") {
		t.Fatalf("receipt failure changed outcome or disappeared: %+v", tasks[0])
	}
	attempt := tasks[0].AttemptID
	rt.background.mu.Lock()
	delete(rt.background.entries, "task")
	rt.background.order = nil
	rt.background.mu.Unlock()
	page, err := rt.readBackgroundResult(t.Context(), "session", protocol.ResultQuery{ID: "task", AttemptID: attempt, Limit: 100})
	if err != nil || page.Status != "completed" || page.Content != "committed evidence" {
		t.Fatalf("committed outcome changed after receipt eviction: %+v %v", page, err)
	}
}
