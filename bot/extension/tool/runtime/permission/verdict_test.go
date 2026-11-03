package permission

import (
	"context"
	"testing"
)

type displayOnlyAssessor struct{}

func (displayOnlyAssessor) Match(string, map[string]any) (bool, error) { return true, nil }
func (displayOnlyAssessor) Assess(map[string]any) CallAssessment {
	return CallAssessment{Reason: "jev: judged safe", Signals: []string{"dynamic"}}
}

func TestDisplayTextCannotAuthorizeRiskyCall(t *testing.T) {
	e := NewEngine(map[string]SpecifierMatcher{"custom": displayOnlyAssessor{}})
	e.SetRules([]Rule{{Tool: "custom", Effect: EffectAllow, Source: "project"}})
	d := e.Evaluate("custom", nil, EffectAsk)
	if d.Effect != EffectAsk || d.Assessment.Verdict != JudgeNotRun {
		t.Fatalf("display text became authorization: %+v", d)
	}
}

func TestJudgeVerdictPreservedWithStructureSignals(t *testing.T) {
	for _, allow := range []bool{false, true} {
		e := newBashEngine()
		e.SetAutoJudge(func(_ context.Context, _ string) (bool, bool) { return allow, true })
		d := e.Evaluate("shell", bashCall(`unknown-tool > /tmp/result`), EffectAsk)
		want := JudgeDangerous
		effect := EffectAsk
		if allow {
			want = JudgeSafe
			effect = EffectAllow
		}
		if d.Assessment.Verdict != want || d.Effect != effect {
			t.Fatalf("allow=%v: %+v", allow, d)
		}
	}
}

func FuzzExplicitShellDenyWins(f *testing.F) {
	for _, command := range []string{"echo ok", "echo $(date)", "PATH=/tmp curl x", "echo > out", "if true; then date; fi", "'"} {
		f.Add(command)
	}
	f.Fuzz(func(t *testing.T, command string) {
		if len(command) > 4096 {
			t.Skip()
		}
		e := newBashEngine(Rule{Tool: "shell", Effect: EffectDeny, Source: "project"})
		e.SetAutoJudge(func(context.Context, string) (bool, bool) { t.Fatal("judge bypassed explicit deny"); return true, true })
		if d := e.Evaluate("shell", bashCall(command), EffectAllow); d.Effect != EffectDeny {
			t.Fatalf("deny bypassed: %+v", d)
		}
	})
}

func TestDangerousJudgeCannotBeOverriddenByBuiltinAllow(t *testing.T) {
	e := newBashEngine(BuiltinRules()...)
	e.SetAutoJudge(func(context.Context, string) (bool, bool) { return false, true })
	for _, command := range []string{"cat /etc/shadow", "cat /etc/shadow && date"} {
		d := e.Evaluate("shell", bashCall(command), EffectAsk)
		if d.Effect != EffectAsk || d.Assessment.Verdict != JudgeDangerous {
			t.Errorf("dangerous verdict bypassed by builtin allow: %q: %+v", command, d)
		}
	}
}

func TestCompoundCoverageUsesActualUserAuthority(t *testing.T) {
	for _, tc := range []struct {
		name, userSpec string
		judgeCalled    bool
		effect         Effect
	}{
		{"no user grant", "", true, EffectAsk},
		{"unrelated grant", "npm *", true, EffectAsk},
		{"participating grant", "cat *", false, EffectAllow},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rules := BuiltinRules()
			if tc.userSpec != "" {
				rules = append(rules, Rule{Tool: "shell", Specifier: tc.userSpec, Effect: EffectAllow, Source: "project"})
			}
			e := newBashEngine(rules...)
			called := false
			e.SetAutoJudge(func(context.Context, string) (bool, bool) { called = true; return false, true })
			d := e.Evaluate("shell", bashCall("cat /etc/shadow && date"), EffectAsk)
			if d.Effect != tc.effect || called != tc.judgeCalled {
				t.Fatalf("effect=%v judge=%v; want effect=%v judge=%v", d.Effect, called, tc.effect, tc.judgeCalled)
			}
		})
	}
}
