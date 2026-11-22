package runtime

import (
	"context"
	"nekocode/protocol"
	"nekocode/util/modelbudget"
	"testing"
	"time"
)

func TestBudgetSurvivesWorkerAndResultCoordination(t *testing.T) {
	budget, _ := modelbudget.New(100)
	observations := make(chan *modelbudget.Budget, 2)
	rt := New(RunnerFunc(func(ctx context.Context, input string, host RunHost) (string, error) {
		if protocol.CoordinationFromContext(ctx).Notification {
			observations <- modelbudget.FromContext(ctx)
			return "reviewed", nil
		}
		_, err := host.(protocol.BackgroundSubmitter).SubmitBackground(ctx, protocol.BackgroundWork{ID: "task", SessionID: "session", GroupID: "plan", ReadOnly: true, Run: func(ctx context.Context, _ protocol.BackgroundHost) (protocol.BackgroundResult, error) {
			observations <- modelbudget.FromContext(ctx)
			return protocol.BackgroundResult{Text: "evidence"}, nil
		}})
		return "accepted", err
	}), backgroundServices(t.TempDir()))
	defer rt.Close()
	ctx, cancel := context.WithTimeout(modelbudget.WithBudget(t.Context(), budget), 5*time.Second)
	defer cancel()
	id, err := rt.StartRun(ctx, Input{Text: "review"})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.WaitRun(ctx, id); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		select {
		case got := <-observations:
			if got != budget {
				t.Fatal("budget detached from execution tree")
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
}
func TestQueuedDemandRetainsConnectionBudget(t *testing.T) {
	budget, _ := modelbudget.New(100)
	release := make(chan struct{})
	started := make(chan struct{})
	observed := make(chan *modelbudget.Budget, 1)
	rt := New(RunnerFunc(func(ctx context.Context, input string, _ RunHost) (string, error) {
		if input == "first" {
			close(started)
			select {
			case <-release:
			case <-ctx.Done():
				return "", ctx.Err()
			}
		} else {
			observed <- modelbudget.FromContext(ctx)
		}
		return "done", nil
	}), backgroundServices(t.TempDir()))
	defer rt.Close()
	ctx, cancel := context.WithTimeout(modelbudget.WithBudget(t.Context(), budget), 5*time.Second)
	defer cancel()
	if _, err := rt.StartRun(ctx, Input{Text: "first"}); err != nil {
		t.Fatal(err)
	}
	awaitBackground(t, started)
	if _, err := rt.StartRun(ctx, Input{Text: "queued"}); err != nil {
		t.Fatal(err)
	}
	close(release)
	select {
	case got := <-observed:
		if got != budget {
			t.Fatal("queued demand dropped budget")
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

func TestResultDeliveriesNeverMixExecutionBudgetsInOnePlan(t *testing.T) {
	first, _ := modelbudget.New(20)
	second, _ := modelbudget.New(30)
	old, _ := modelbudget.New(40)
	observed := make(chan *modelbudget.Budget, 2)
	rt := New(RunnerFunc(func(ctx context.Context, input string, _ RunHost) (string, error) {
		observed <- modelbudget.FromContext(ctx)
		return "reviewed", nil
	}), backgroundServices(t.TempDir()))
	defer rt.Close()
	b := rt.background
	b.mu.Lock()
	b.loaded["session"] = true
	for _, task := range []struct {
		id       string
		budget   *modelbudget.Budget
		notified bool
	}{{"old", old, true}, {"first", first, false}, {"second", second, false}} {
		b.entries[task.id] = &backgroundEntry{BackgroundTask: BackgroundTask{ID: task.id, SessionID: "session", GroupID: "same-plan", Status: "completed", Result: "evidence", Notified: task.notified}, callBudget: task.budget}
		b.order = append(b.order, task.id)
	}
	b.mu.Unlock()
	b.signal()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	seen := map[*modelbudget.Budget]bool{}
	for range 2 {
		select {
		case got := <-observed:
			seen[got] = true
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	if len(seen) != 2 || !seen[first] || !seen[second] || seen[old] {
		t.Fatal("deliveries mixed or charged historical budgets")
	}
}

func TestPendingWorkerKeepsItsOwnBudgetReserve(t *testing.T) {
	b := &backgroundTasks{entries: map[string]*backgroundEntry{}}
	budget, _ := modelbudget.New(400)
	other, _ := modelbudget.New(400)
	ctx := modelbudget.WithBudget(t.Context(), budget)
	for modelbudget.Admit(ctx) == nil {
	}
	for _, status := range []string{"queued", "waiting_dependencies", "running", "waiting_approval", "waiting_question"} {
		b.entries["worker"] = &backgroundEntry{BackgroundTask: BackgroundTask{Status: status}, callBudget: budget}
		b.releaseIdleHandoffs(budget)
		if budget.Snapshot().HandoffRemaining != 8 {
			t.Fatalf("released reserve while %s", status)
		}
	}
	b.entries["worker"].Status = "completed"
	b.entries["other"] = &backgroundEntry{BackgroundTask: BackgroundTask{Status: "running"}, callBudget: other}
	b.releaseIdleHandoffs(budget)
	if s := budget.Snapshot(); s.HandoffRemaining != 0 || s.CoordinatorRemaining != 12 {
		t.Fatal(s)
	}
}
