package command

import (
	"testing"

	"nekocode/bot/tools"
)

func TestEstimateToolDefTokens(t *testing.T) {
	descs := []tools.Descriptor{
		{Name: "read", Description: "read files", Parameters: []tools.Parameter{
			{Name: "path", Type: "string", Description: "file path"},
		}},
	}
	n := EstimateToolDefTokens(descs)
	if n <= 0 {
		t.Errorf("expected positive token count, got %d", n)
	}
}

func TestSkillState(t *testing.T) {
	st := &SkillState{MsgStart: -1}
	if ClearSkillContext(nil, st); st.MsgStart != -1 {
		t.Error("should be no-op when MsgStart is -1")
	}
}

func TestDeps(t *testing.T) {
	d := Deps{
		ContextWindow: 100000,
		GetConfigFn:   func() (string, string) { return "anthropic", "claude-sonnet-4-6" },
	}
	if d.ContextWindow != 100000 {
		t.Error("bad ContextWindow")
	}
	p, m := d.GetConfigFn()
	if p != "anthropic" || m != "claude-sonnet-4-6" {
		t.Errorf("bad GetConfigFn: %s/%s", p, m)
	}
}
