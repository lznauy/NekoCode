package policy

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
)

func TestInspectIsDetachedAndAvailableDuringExecution(t *testing.T) {
	p := New()
	p.SetSessionID("session")
	p.ResetRun()
	entered, release := make(chan struct{}), make(chan struct{})
	p.Register(Hook{Name: "waiting", Point: PreModel, On: func(s State) *Result {
		close(entered)
		<-release
		s.SetString("secret", "private-state-value")
		return nil
	}})
	done := make(chan struct{})
	go func() { defer close(done); p.BeforeModel() }()
	<-entered
	inspection := p.Inspect()
	if inspection.Active == nil || inspection.Active.Hook != "waiting" || inspection.Active.EvaluationID != inspection.LastEvaluationID || inspection.RunID == "" {
		close(release)
		<-done
		t.Fatalf("incomplete active view: %+v", inspection)
	}
	inspection.Active.Hook = "changed"
	inspection.Hooks[0].Name = "changed"
	if got := p.Inspect(); got.Active.Hook != "waiting" || got.Hooks[0].Name != "waiting" {
		t.Error("caller mutated inspection state")
	}
	close(release)
	<-done
	final := p.Inspect()
	if final.Active != nil || len(final.Recent) != 1 || !final.Recent[0].StateCommitted || final.Recent[0].EvaluationID != inspection.LastEvaluationID || final.Hooks[0].StateEntries != 1 {
		t.Fatalf("incomplete final view: %+v", final)
	}
	final.Recent[0].Hook = "changed"
	if p.Inspect().Recent[0].Hook != "waiting" {
		t.Fatal("caller mutated audit ring")
	}
	raw, err := json.Marshal(final)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "private-state-value") {
		t.Fatal("inspection exposed private state")
	}
}

func TestInspectCountersClassifyRejectedActions(t *testing.T) {
	p := New()
	p.ResetRun()
	for _, point := range []HookPoint{PreModel, PreToolUse, Stop} {
		if err := p.Register(Hook{Name: string(point), Point: point, On: func(State) *Result { return &Result{Decision: &Decision{Action: Reject, Reason: "denied"}} }}); err != nil {
			t.Fatal(err)
		}
	}
	p.BeforeModel()
	p.BeforeTool(ToolRequest{Name: "read"})
	p.BeforeStop(TurnResult{})
	got := p.Inspect()
	if got.Counters.Evaluations != 3 || got.Counters.Rejections != 3 || got.Counters.BlockTools != 1 || got.Counters.BlockFinals != 1 {
		t.Fatalf("misclassified counters: %+v", got.Counters)
	}
	if got.LastEvaluationID != 3 || got.Recent[0].EvaluationID != 1 || got.Recent[2].EvaluationID != 3 {
		t.Fatalf("uncorrelated events: %+v", got.Recent)
	}
	p.ResetRun()
	if reset := p.Inspect(); reset.Counters.Evaluations != 0 || len(reset.Recent) != 0 || reset.LastEvaluationID != 0 || reset.RunID == got.RunID {
		t.Fatalf("run counters not reset: %+v", reset)
	}
}

func TestInspectConcurrentReadersAndEvaluations(t *testing.T) {
	p := New()
	p.ResetRun()
	p.Register(Hook{Name: "counter", Point: PreModel, On: func(s State) *Result { s.SetInt("count", s.Int("count")+1); return nil }})
	var workers sync.WaitGroup
	for i := 0; i < 4; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for j := 0; j < 20; j++ {
				p.Inspect()
			}
		}()
	}
	for i := 0; i < 20; i++ {
		p.BeforeModel()
	}
	workers.Wait()
	if got := p.Inspect(); got.Counters.Evaluations != 20 || len(got.Recent) != 20 {
		t.Fatalf("missing observations: %+v", got)
	}
}
