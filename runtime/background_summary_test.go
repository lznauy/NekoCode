package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"nekocode/mode"
	"nekocode/protocol"
)

func cleanReviewFixture() protocol.TaskReview {
	return protocol.TaskReview{TaskReport: protocol.TaskReport{Outcome: "complete", Summary: "checks complete", Evidence: []string{"actual command exit 0"}, Verification: "targeted checks passed", Checks: []protocol.TaskReviewCheck{{Name: "regression", Status: "passed", Evidence: "go test ./target: exit 0", Entrypoint: "public entrypoint", Scope: "current snapshot, isolated task"}}}}
}

func TestStructuredReviewOmitsDuplicatedPromptAndUsage(t *testing.T) {
	report := cleanReviewFixture()
	report.Evidence = append(report.Evidence, "counterevidence: cleanup retry works")
	report.Risks = []string{"integration not checked"}
	task := BackgroundTask{ID: "task", SessionID: "session", GroupID: "group", AttemptID: "attempt-1", Status: "completed", Prompt: strings.Repeat("REPEATED_REQUIREMENTS ", 100), PendingUpdates: []string{"OLD_USER_UPDATE"}, Result: "FORMATTED_DUPLICATE", Review: &report, ReviewComplete: true, Usage: BackgroundUsage{TotalTokens: 123456}}
	b := &backgroundTasks{entries: map[string]*backgroundEntry{"task": {BackgroundTask: task}}, order: []string{"task"}}
	review := b.reviewLocked("session", "group", []string{"task"})
	data, err := json.Marshal(review)
	if err != nil {
		t.Fatal(err)
	}
	for _, unwanted := range []string{"REPEATED_REQUIREMENTS", "OLD_USER_UPDATE", "FORMATTED_DUPLICATE", "total_tokens", "attempt_usage"} {
		if strings.Contains(string(data), unwanted) {
			t.Fatalf("redundant data leaked into review: %s", unwanted)
		}
	}
	got := review.Results[0]
	if !got.ReviewComplete || got.ResultComplete || got.ReadResult != nil {
		t.Fatalf("incorrect evidence completeness: %+v", got)
	}
	if len(got.Review.Evidence) != 2 || len(got.Review.Risks) != 1 {
		t.Fatal("counterevidence or risk lost")
	}
	got.Review.Checks[0].Scope = "mutated"
	if report.Checks[0].Scope == "mutated" || b.entries["task"].Result != "FORMATTED_DUPLICATE" {
		t.Fatal("projection mutated canonical evidence")
	}
	legacy := task
	legacy.Review = nil
	legacy.ReviewComplete = false
	legacy.Prompt = protocol.SubagentTextPreview(task.Prompt, 1024)
	oldData, _ := json.Marshal(legacy)
	if len(data) >= len(oldData) {
		t.Fatalf("notification did not shrink: new=%d legacy=%d", len(data), len(oldData))
	}
	t.Logf("structured delivery %d bytes; legacy task payload %d bytes (synthetic fixture, not token billing)", len(data), len(oldData))
}

func TestOversizedReviewKeepsFullReadAndAttentionRequirement(t *testing.T) {
	report := cleanReviewFixture()
	report.Summary = strings.Repeat("长报告证据", 3000)
	copy, complete := boundedTaskReview(report)
	if complete || len(copy.Summary) > 1024 {
		t.Fatal("oversized report falsely complete or unbounded")
	}
	task := BackgroundTask{ID: "task", SessionID: "session", GroupID: "group", Status: "completed", AttemptID: "pinned", ReviewPolicy: "batch", Review: copy, ReviewComplete: complete, Result: "full canonical result"}
	task.Review.Outcome = ""
	if batchableHandoff(task) {
		t.Fatal("oversized handoff deferred as fully reviewed")
	}
	b := &backgroundTasks{entries: map[string]*backgroundEntry{"task": {BackgroundTask: task}}, order: []string{"task"}}
	got := b.reviewLocked("session", "group", []string{"task"}).Results[0]
	if got.ReviewComplete || got.ResultComplete || got.Result != "" || got.ReadResult == nil {
		t.Fatal("missing full-report recovery")
	}
}

func TestBatchSelectionUsesSchedulingBlockers(t *testing.T) {
	for _, kind := range []string{"complete", "failed", "partial", "blocked", "missing outcome", "approval", "question", "other group"} {
		t.Run(kind, func(t *testing.T) {
			report := cleanReviewFixture()
			a := &backgroundEntry{BackgroundTask: BackgroundTask{ID: "a", SessionID: "session", GroupID: "group", Status: "completed", ReviewPolicy: "batch", Review: &report, ReviewComplete: true}}
			peer := &backgroundEntry{BackgroundTask: BackgroundTask{ID: "peer", SessionID: "session", GroupID: "group", Status: "running", ReviewPolicy: "batch"}}
			switch kind {
			case "failed", "partial", "blocked":
				a.Status = kind
			case "missing outcome":
				report.Outcome = ""
			case "approval":
				peer.Status = "waiting_approval"
			case "question":
				peer.Status = "waiting_question"
			case "other group":
				peer.GroupID = "unrelated"
			}
			b := &backgroundTasks{entries: map[string]*backgroundEntry{"a": a, "peer": peer}, order: []string{"a", "peer"}}
			ids, _ := b.reviewIDsLocked("session")
			if kind == "complete" {
				if len(ids) != 0 {
					t.Fatal("explicit completion did not wait for batch")
				}
			} else if len(ids) != 1 || ids[0] != "a" {
				t.Fatalf("required review was delayed or groups mixed: %v", ids)
			}
		})
	}
}

func TestReviewDeliveryLimitRetainsPendingReviewCount(t *testing.T) {
	b := &backgroundTasks{entries: map[string]*backgroundEntry{}}
	for i := 0; i < 17; i++ {
		id := fmt.Sprint(i)
		b.entries[id] = &backgroundEntry{BackgroundTask: BackgroundTask{ID: id, SessionID: "session", GroupID: "group", Status: "completed", Result: "evidence"}}
		b.order = append(b.order, id)
	}
	ids, group := b.reviewIDsLocked("session")
	review := b.reviewLocked("session", group, ids)
	if len(ids) != 16 || review.PendingTasks != 0 || review.PendingReviews != 1 {
		t.Fatalf("undelivered results disappeared: %+v", review)
	}
}

func TestCommittedReviewDoesNotLeakMutableState(t *testing.T) {
	services := backgroundServices(t.TempDir())
	services.Modes.Current = func() mode.ID { return mode.Standard }
	rt := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) { return "", nil }), services)
	defer rt.Close()
	_, err := rt.background.submit(t.Context(), protocol.BackgroundWork{ID: "task", SessionID: "session", ReadOnly: true, Run: func(context.Context, protocol.BackgroundHost) (protocol.BackgroundResult, error) {
		report := cleanReviewFixture()
		return protocol.BackgroundResult{Review: &report}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	waitBackgroundStatus(t, rt, "task", "completed")
	tasks, err := rt.BackgroundTasks("session")
	if err != nil {
		t.Fatal(err)
	}
	tasks[0].Review.Checks[0].Scope = "caller mutation"
	tasks, _ = rt.BackgroundTasks("session")
	if tasks[0].Review.Checks[0].Scope != "current snapshot, isolated task" {
		t.Fatal("caller mutated stored review")
	}
}

func TestStructuredReviewSurvivesPersistence(t *testing.T) {
	services := backgroundServices(t.TempDir())
	rt := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) { return "", nil }), services)
	defer rt.Close()
	if err := rt.background.load("session"); err != nil {
		t.Fatal(err)
	}
	review := cleanReviewFixture()
	review.WorkspacePath, review.WorkspaceBase, review.SourceHead = "/saved/worktree", "base-commit", "source-commit"
	rt.background.lockMutation()
	rt.background.entries["saved"] = &backgroundEntry{BackgroundTask: BackgroundTask{ID: "saved", SessionID: "session", GroupID: "plan", Status: "completed", Notified: true, ReviewPolicy: "batch", Review: &review, ReviewComplete: true}}
	rt.background.order = []string{"saved"}
	err := rt.background.saveLocked("session")
	rt.background.unlockMutation()
	if err != nil {
		t.Fatal(err)
	}
	recovered := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) { return "", nil }), services)
	defer recovered.Close()
	tasks, err := recovered.BackgroundTasks("session")
	if err != nil || len(tasks) != 1 {
		t.Fatalf("recovery: %v %v", tasks, err)
	}
	if tasks[0].ReviewPolicy != "batch" || !tasks[0].ReviewComplete || !reflect.DeepEqual(tasks[0].Review, &review) {
		t.Fatalf("review metadata lost: %+v", tasks[0])
	}
}

func TestResumePreservesReviewPolicyButClearsPriorReview(t *testing.T) {
	for _, policy := range []string{"", "immediate"} {
		t.Run("policy_"+policy, func(t *testing.T) {
			services := backgroundServices(t.TempDir())
			services.Modes.Current = func() mode.ID { return mode.Standard }
			rt := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) { return "", nil }), services)
			defer rt.Close()
			if err := rt.background.load("session"); err != nil {
				t.Fatal(err)
			}
			review := cleanReviewFixture()
			rt.background.mu.Lock()
			rt.background.entries["saved"] = &backgroundEntry{BackgroundTask: BackgroundTask{ID: "saved", SessionID: "session", GroupID: "plan", Status: "completed", Notified: true, ReviewPolicy: "batch", Review: &review, ReviewComplete: true}}
			rt.background.order = []string{"saved"}
			rt.background.mu.Unlock()
			_, err := rt.background.submit(t.Context(), protocol.BackgroundWork{ID: "saved", SessionID: "session", ReviewPolicy: policy, Run: func(ctx context.Context, _ protocol.BackgroundHost) (protocol.BackgroundResult, error) {
				<-ctx.Done()
				return protocol.BackgroundResult{Text: ""}, ctx.Err()
			}})
			if err != nil {
				t.Fatal(err)
			}
			tasks, err := rt.BackgroundTasks("session")
			want := policy
			if want == "" {
				want = "batch"
			}
			if err != nil || len(tasks) != 1 || tasks[0].ReviewPolicy != want || tasks[0].Review != nil || tasks[0].ReviewComplete {
				t.Fatalf("stale review or lost policy: %+v %v", tasks, err)
			}
		})
	}
}
