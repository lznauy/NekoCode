package policy

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"nekocode/bot/policy/contract"
)

func TestLifecycleSeparatesRunAndSession(t *testing.T) {
	p := New()
	p.SetSessionID("a")
	var starts, ends, runs int
	p.Register(Hook{Name: "session", Point: SessionStart, On: func(s State) *Result { starts++; return nil }})
	p.Register(Hook{Name: "end", Point: SessionEnd, On: func(s State) *Result { ends++; return nil }})
	p.Register(Hook{Name: "run", Point: RunStart, On: func(s State) *Result { runs++; return nil }})
	for i := 0; i < 2; i++ {
		p.ResetRun()
		p.StartRun(context.Background(), "hello")
		p.EndRun(context.Background())
	}
	if starts != 1 || ends != 0 || runs != 2 {
		t.Fatalf("start/end/run = %d/%d/%d", starts, ends, runs)
	}
	p.SetSessionID("b")
	p.ResetRun()
	p.StartRun(context.Background(), "next")
	p.EndSession(context.Background())
	p.EndSession(context.Background())
	if starts != 2 || ends != 2 {
		t.Fatalf("start/end = %d/%d", starts, ends)
	}
}

func TestInvalidResultDoesNotCommitState(t *testing.T) {
	e := newHookEngine()
	e.register(Hook{Name: "invalid", Point: PreToolUse, Guard: true, On: func(s State) *Result {
		s.SetInt("direct", 1)
		return &Result{Decision: &Decision{Action: Reject}, BlockTool: &BlockTool{}, StatePatch: &contract.StatePatch{Ints: map[string]int64{"patch": 1}}}
	}})
	out := Resolve(e.evaluate(PreToolUse, Facts{}))
	if out.Action != Reject || len(out.Hints) != 1 {
		t.Fatalf("outcome=%+v", out)
	}
	if e.state["invalid"].ints["direct"] != 0 || e.state["invalid"].ints["patch"] != 0 {
		t.Fatal("invalid state committed")
	}
}

func TestPanicAndTimeoutFollowFailureMode(t *testing.T) {
	for _, guard := range []bool{false, true} {
		e := newHookEngine()
		e.register(Hook{Name: "panic", Point: PreToolUse, Guard: guard, On: func(State) *Result { panic("boom") }})
		out := Resolve(e.evaluate(PreToolUse, Facts{}))
		if (out.Action == Reject) != guard || len(out.Hints) != 1 {
			t.Fatalf("panic outcome=%+v guard=%v", out, guard)
		}
	}
	e := newHookEngine()
	e.register(Hook{Name: "slow", Point: PreToolUse, Guard: true, Timeout: time.Millisecond, Run: func(ctx context.Context, s State) (*Result, error) { <-ctx.Done(); return nil, ctx.Err() }})
	if out := Resolve(e.evaluate(PreToolUse, Facts{})); out.Action != Reject {
		t.Fatalf("timeout outcome=%+v", out)
	}
}

func TestCanceledEvaluationDoesNotWaitForBusyEngine(t *testing.T) {
	e := newHookEngine()
	entered, release := make(chan struct{}), make(chan struct{})
	e.register(Hook{Name: "busy", Point: PreModel, On: func(State) *Result { close(entered); <-release; return nil }})
	done := make(chan struct{})
	go func() { defer close(done); e.evaluate(PreModel, Facts{}) }()
	<-entered
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	out := Resolve(e.evaluate(PreModel, Facts{}, ctx))
	close(release)
	<-done
	if out.StopReason != StopInterrupted || time.Since(start) > time.Second {
		t.Fatalf("cancellation outcome=%+v", out)
	}
}

func TestResolveRetainsHintsAndCombinesRejections(t *testing.T) {
	out := Resolve([]Result{
		{Hint: &Hint{Content: "first"}},
		{Decision: &Decision{Action: Reject, Reason: "one"}, Hints: []Hint{{Content: "second"}}},
		{BlockFinal: &BlockFinal{Reason: "two"}},
	})
	if out.Action != Reject || out.Reason != "one; two" || len(out.Hints) != 2 {
		t.Fatalf("outcome=%+v", out)
	}
	terminal := Resolve([]Result{{Decision: &Decision{Action: Reject, Reason: "rejected"}}, {Decision: &Decision{Action: Terminate, Reason: "stop"}}})
	if terminal.Action != Terminate || !strings.Contains(terminal.Reason, "rejected") {
		t.Fatalf("terminal=%+v", terminal)
	}
}

func TestObserverErrorDoesNotConsumeOnce(t *testing.T) {
	e := newHookEngine()
	attempt := 0
	e.register(Hook{Name: "once", Point: SessionStart, Run: func(context.Context, State) (*Result, error) {
		attempt++
		if attempt == 1 {
			return nil, errors.New("retry")
		}
		return nil, nil
	}})
	e.evaluate(SessionStart, Facts{})
	e.evaluate(SessionStart, Facts{})
	e.evaluate(SessionStart, Facts{})
	if attempt != 2 {
		t.Fatalf("attempts=%d", attempt)
	}
}

func TestRegisterRejectsInvalidHooks(t *testing.T) {
	p := New()
	for _, h := range []Hook{{Name: "x", Point: PreModel}, {Name: "x", Point: "typo", On: func(State) *Result { return nil }}, {Name: "x", Point: PreModel, Timeout: -time.Second, On: func(State) *Result { return nil }}} {
		if err := p.Register(h); err == nil {
			t.Fatalf("invalid hook accepted: %+v", h)
		}
	}
}

func TestHookStateLimitsRollBackEntireUpdate(t *testing.T) {
	for _, kind := range []string{"entries", "bytes"} {
		t.Run(kind, func(t *testing.T) {
			e := newHookEngine()
			e.register(Hook{Name: "quota", Point: PreToolUse, SessionScoped: true, Guard: true, On: func(s State) *Result {
				s.SetInt("existing", 99)
				if kind == "entries" {
					s.SetInt("extra", 1)
				} else {
					s.SetString("huge", strings.Repeat("x", 64*1024))
				}
				return nil
			}})
			memory := hookMemory{ints: map[string]int64{"existing": 1}}
			if kind == "entries" {
				for i := 0; i < 255; i++ {
					memory.ints[fmt.Sprint(i)] = 1
				}
			}
			e.state["quota"] = memory
			out := Resolve(e.evaluate(PreToolUse, Facts{}))
			if out.Action != Reject || len(out.Hints) != 1 {
				t.Fatalf("quota not enforced: %+v", out)
			}
			if e.state["quota"].ints["existing"] != 1 || e.state["quota"].ints["extra"] != 0 || e.state["quota"].strings["huge"] != "" {
				t.Fatal("partial state update committed")
			}
		})
	}
}

func TestSessionChangeDiscardsInFlightStateAndDecision(t *testing.T) {
	e := newHookEngine()
	e.setSessionID("before")
	entered, release := make(chan struct{}), make(chan struct{})
	e.register(Hook{Name: "session", Point: PreModel, SessionScoped: true, On: func(s State) *Result {
		close(entered)
		<-release
		s.SetInt("old", 1)
		return &Result{Hint: &Hint{Content: "stale"}}
	}})
	done := make(chan []Result, 1)
	go func() { done <- e.evaluate(PreModel, Facts{}) }()
	<-entered
	e.setSessionID("after")
	close(release)
	out := Resolve(<-done)
	if out.Action != Terminate || out.StopReason != StopPolicyBlocked || len(out.Hints) != 0 {
		t.Fatalf("stale decision escaped: %+v", out)
	}
	if len(e.state) != 0 {
		t.Fatal("old session state committed")
	}
	if len(e.audit) != 1 || e.audit[0].Action != "discarded" || e.audit[0].StateCommitted {
		t.Fatalf("discard not observed: %+v", e.audit)
	}
}

func TestNoopHooksAreObservableAndAuditIsBounded(t *testing.T) {
	e := newHookEngine()
	e.register(Hook{Name: "noop", Point: PreModel, On: func(s State) *Result { s.SetInt("count", s.Int("count")+1); return nil }})
	e.reset()
	for i := 0; i < maxHookAuditEvents+2; i++ {
		if got := e.evaluate(PreModel, Facts{}); len(got) != 0 {
			t.Fatal("no-op emitted a decision")
		}
	}
	if len(e.audit) != maxHookAuditEvents {
		t.Fatalf("audit length=%d", len(e.audit))
	}
	last := e.audit[len(e.audit)-1]
	if last.Action != "continue" || !last.StateCommitted || last.StateEntries != 1 || last.StateBytes == 0 || last.RunID == "" || last.ActorID != "main" {
		t.Fatalf("incomplete audit: %+v", last)
	}
}
