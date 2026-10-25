package policy

import (
	"strings"
	"testing"
	"time"
)

func TestHookStatePersistsAndIsPrivate(t *testing.T) {
	engine := newHookEngine()
	var firstSeen, secondSeen int64
	engine.register(Hook{
		Name:  "first",
		Point: PreModel,
		On: func(state State) *Result {
			firstSeen = state.Int("count")
			state.SetInt("count", firstSeen+1)
			return nil
		},
	})
	engine.register(Hook{
		Name:  "second",
		Point: PreModel,
		On: func(state State) *Result {
			secondSeen = state.Int("count")
			return nil
		},
	})

	engine.evaluate(PreModel, Facts{})
	engine.evaluate(PreModel, Facts{})
	if firstSeen != 1 {
		t.Fatalf("first hook count = %d, want 1 on second evaluation", firstSeen)
	}
	if secondSeen != 0 {
		t.Fatalf("second hook observed first hook state: %d", secondSeen)
	}
}

func TestHookReceivesImmutableFactsValue(t *testing.T) {
	engine := newHookEngine()
	facts := Facts{
		Tool: ToolFacts{Args: map[string]any{"path": "main.go"}},
	}
	engine.register(Hook{
		Name:  "capture",
		Point: PreModel,
		On: func(state State) *Result {
			got := state.Facts()
			got.Tool.Args["path"] = "changed.go"
			return nil
		},
	})

	engine.evaluate(PreModel, facts)
	if facts.Tool.Args["path"] != "main.go" {
		t.Fatalf("source args mutated: %+v", facts.Tool.Args)
	}
}

func TestHookAuditAndStats(t *testing.T) {
	engine := newHookEngine()
	engine.setSessionID("session-1")
	engine.register(Hook{
		Name:  "block",
		Point: PreToolUse,
		On: func(State) *Result {
			return &Result{BlockTool: &BlockTool{Tool: "edit", Reason: "read first"}}
		},
	})

	engine.evaluate(PreToolUse, Facts{Tool: ToolFacts{Name: "edit"}})
	stats := engine.summary()
	if !strings.Contains(stats, "block_tool=block") || !strings.Contains(stats, "block@pre_tool_use:block_tool") {
		t.Fatalf("stats = %q", stats)
	}
	if second := engine.summary(); second != stats {
		t.Fatalf("second summary = %q, want stable %q", second, stats)
	}
}

func TestUnregisterWhereRemovesHookAndState(t *testing.T) {
	engine := newHookEngine()
	engine.register(Hook{
		Name:  "drop",
		Point: PreModel,
		On: func(state State) *Result {
			state.SetInt("count", 1)
			return nil
		},
	})
	engine.evaluate(PreModel, Facts{})
	engine.unregisterPrefix("drop")

	if len(engine.hooks) != 0 {
		t.Fatalf("hooks = %+v, want empty", engine.hooks)
	}
	if _, ok := engine.state["drop"]; ok {
		t.Fatal("unregistered hook state was retained")
	}
}

func TestRegisterReplacesHookWithSameName(t *testing.T) {
	engine := newHookEngine()
	engine.register(Hook{Name: "guard", Point: PreModel, On: func(State) *Result { return nil }})
	engine.register(Hook{Name: "guard", Point: Stop, On: func(State) *Result { return nil }})

	hooks := engine.hooks
	if len(hooks) != 1 || hooks[0].Point != Stop {
		t.Fatalf("hooks = %+v, want replacement", hooks)
	}
}

func TestHookNestedFactsAreIsolated(t *testing.T) {
	e := newHookEngine()
	facts := Facts{Tool: ToolFacts{Args: map[string]any{"nested": map[string]any{"value": "original"}, "list": []any{"original"}}}}
	e.register(Hook{Name: "mutate", Point: PreModel, On: func(s State) *Result {
		s.Facts().Tool.Args["nested"].(map[string]any)["value"] = "changed"
		s.Facts().Tool.Args["list"].([]any)[0] = "changed"
		return nil
	}})
	e.register(Hook{Name: "observe", Point: PreModel, On: func(s State) *Result {
		if s.Facts().Tool.Args["nested"].(map[string]any)["value"] != "original" || s.Facts().Tool.Args["list"].([]any)[0] != "original" {
			t.Error("nested facts leaked between hooks")
		}
		return nil
	}})
	e.evaluate(PreModel, facts)
	if facts.Tool.Args["nested"].(map[string]any)["value"] != "original" {
		t.Error("source facts mutated")
	}
}

func TestReplacingInFlightHookDoesNotRestoreOldState(t *testing.T) {
	e := newHookEngine()
	entered, release := make(chan struct{}), make(chan struct{})
	e.register(Hook{Name: "replace", Point: PreModel, On: func(s State) *Result { close(entered); <-release; s.SetInt("old", 1); return nil }})
	done := make(chan struct{})
	go func() { defer close(done); e.evaluate(PreModel, Facts{}) }()
	<-entered
	e.register(Hook{Name: "replace", Point: PreModel, On: func(s State) *Result {
		if s.Int("old") != 0 {
			t.Error("old callback restored state")
		}
		return nil
	}})
	close(release)
	<-done
	e.evaluate(PreModel, Facts{})
}

func TestResolvePrioritizesStopOverEarlierHint(t *testing.T) {
	e := newHookEngine()
	e.register(Hook{Name: "hint", Point: Stop, On: func(State) *Result { return &Result{Hint: &Hint{Content: "revise"}} }})
	reason := StopFormatError
	e.register(Hook{Name: "stop", Point: Stop, On: func(State) *Result { return &Result{Stop: &reason} }})
	got := e.evaluate(Stop, Facts{})
	if len(got) != 2 || Resolve(got).Action != Terminate {
		t.Fatalf("stop must precede hint: %+v", got)
	}
}

func TestUnsupportedActionsProduceDiagnostic(t *testing.T) {
	for _, point := range []HookPoint{UserSubmit, PreModel, PreToolUse, PostToolUse, PostToolBatch, Stop} {
		t.Run(string(point), func(t *testing.T) {
			e := newHookEngine()
			e.register(Hook{Name: "invalid", Point: point, On: func(State) *Result {
				if point == UserSubmit || point == PreModel {
					return &Result{BlockTool: &BlockTool{Reason: "invalid"}}
				}
				if point == PreToolUse {
					return &Result{BlockFinal: &BlockFinal{Reason: "invalid"}}
				}
				return &Result{BlockTool: &BlockTool{Reason: "invalid"}}
			}})
			got := e.evaluate(point, Facts{})
			if len(got) != 1 || got[0].Hint == nil || got[0].Hint.Type != "hook_error" || got[0].Stop != nil || got[0].BlockTool != nil || got[0].BlockFinal != nil {
				t.Fatalf("invalid action must be diagnosed: %+v", got)
			}
		})
	}
}

func TestDescribeTriggerMayReadRegistry(t *testing.T) {
	e := newHookEngine()
	e.register(Hook{Name: "audit", Point: PreModel, On: func(State) *Result { return &Result{Hint: &Hint{Content: "test"}} }, DescribeTrigger: func(State) string { return e.summary() }})
	done := make(chan struct{})
	go func() { defer close(done); e.evaluate(PreModel, Facts{}) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("DescribeTrigger deadlocked on registry lock")
	}
}
