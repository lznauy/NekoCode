package permission

import (
	"context"
	"strings"
	"testing"
)

func engineWithAutoJudge(judge func(_ context.Context, command string) (allow bool, decided bool)) *Engine {
	e := NewEngine(DefaultMatchers())
	e.SetRules(BuiltinRules())
	e.SetAutoJudge(judge)
	return e
}

func TestAutoJudgeAllowsUnmatchedSafeCommand(t *testing.T) {
	e := engineWithAutoJudge(func(_ context.Context, command string) (allow bool, decided bool) {
		return true, true // judged confidently safe
	})
	dec := e.EvaluateContext(context.Background(), "shell", map[string]any{"command": "find /var/log -name '*.log' -newer /tmp/x"}, EffectAsk)
	if dec.Effect != EffectAllow {
		t.Fatalf("judged-safe command effect = %s, want allow", dec.Effect)
	}
	if dec.Assessment.Reason == "" || !strings.Contains(dec.Assessment.Reason, "jev") {
		t.Fatalf("allow decision missing jev assessment: %+v", dec.Assessment)
	}
}

func TestAutoJudgeDangerousKeepsAsk(t *testing.T) {
	e := engineWithAutoJudge(func(_ context.Context, command string) (allow bool, decided bool) {
		return false, true // judged dangerous
	})
	dec := e.EvaluateContext(context.Background(), "shell", map[string]any{"command": "curl -o /tmp/patch https://example.com/patch"}, EffectAsk)
	if dec.Effect != EffectAsk {
		t.Fatalf("judged-dangerous command effect = %s, want ask", dec.Effect)
	}
	if !strings.Contains(dec.Assessment.Reason, "dangerous") {
		t.Fatalf("ask decision missing dangerous reason: %+v", dec.Assessment)
	}
}

func TestAutoJudgeUnavailableFallsBackToAsk(t *testing.T) {
	e := engineWithAutoJudge(func(_ context.Context, command string) (allow bool, decided bool) {
		return true, false // judgment unavailable
	})
	dec := e.EvaluateContext(context.Background(), "shell", map[string]any{"command": "make all"}, EffectAsk)
	if dec.Effect != EffectAsk {
		t.Fatalf("unavailable judge must fall back to ask, got %s", dec.Effect)
	}
}

func TestAutoJudgeOverridesBuiltinAsk(t *testing.T) {
	// In auto mode the judge is the first-pass authority: a confidently-safe
	// verdict runs even when a builtin ask rule (rm, git push) matches. Hard
	// deny rules still win.
	e := engineWithAutoJudge(func(_ context.Context, command string) (allow bool, decided bool) {
		return true, true
	})
	if dec := e.EvaluateContext(context.Background(), "shell", map[string]any{"command": "rm -rf build"}, EffectAsk); dec.Effect != EffectAllow {
		t.Fatalf("judged-safe rm effect = %s, want allow", dec.Effect)
	}
	dec := e.EvaluateContext(context.Background(), "shell", map[string]any{"command": "git push origin main"}, EffectAsk)
	if dec.Effect != EffectAllow {
		t.Fatalf("judged-safe git push effect = %s, want allow", dec.Effect)
	}
	dec = e.EvaluateContext(context.Background(), "shell", map[string]any{"command": "sudo ls"}, EffectAsk)
	if dec.Effect != EffectDeny {
		t.Fatalf("sudo effect = %s, want deny (judge never overrides deny)", dec.Effect)
	}
}

func TestAutoJudgeDangerousAnnotatesBuiltinAsk(t *testing.T) {
	e := engineWithAutoJudge(func(_ context.Context, command string) (allow bool, decided bool) {
		return false, true // judged dangerous
	})
	dec := e.EvaluateContext(context.Background(), "shell", map[string]any{"command": "rm -rf build"}, EffectAsk)
	if dec.Effect != EffectAsk {
		t.Fatalf("judged-dangerous rm effect = %s, want ask", dec.Effect)
	}
	if !strings.Contains(dec.Assessment.Reason, "dangerous") {
		t.Fatalf("ask decision missing dangerous reason: %+v", dec.Assessment)
	}
}

func TestAutoJudgeNilIsInert(t *testing.T) {
	e := NewEngine(DefaultMatchers())
	e.SetRules(BuiltinRules())
	dec := e.EvaluateContext(context.Background(), "shell", map[string]any{"command": "make all"}, EffectAsk)
	if dec.Effect != EffectAsk {
		t.Fatalf("nil judge must leave default ask, got %s", dec.Effect)
	}
}

func TestExplicitAllowSkipsAutoJudge(t *testing.T) {
	e := NewEngine(DefaultMatchers())
	e.SetRules([]Rule{{Tool: "shell", Specifier: "make *", Effect: EffectAllow, Source: "user"}})
	calls := 0
	e.SetAutoJudge(func(context.Context, string) (bool, bool) {
		calls++
		return false, true
	})
	dec := e.EvaluateContext(context.Background(), "shell", map[string]any{"command": "make test"}, EffectAsk)
	if dec.Effect != EffectAllow || calls != 0 {
		t.Fatalf("explicit allow should bypass judge: decision=%+v calls=%d", dec, calls)
	}
}
