package permission

import (
	"context"
	"testing"
)

func TestJevCannotOverrideExplicitUserAsk(t *testing.T) {
	e := NewEngine(DefaultMatchers())
	e.SetRules([]Rule{{Tool: "shell", Specifier: "git push *", Effect: EffectAsk, Source: "user"}})
	e.SetAutoJudge(func(context.Context, string) (bool, bool) { return true, true })
	d := e.EvaluateContext(context.Background(), "shell", map[string]any{"command": "git push origin main"}, EffectAsk)
	if d.Effect != EffectAsk {
		t.Fatalf("explicit user ask bypassed: effect=%s", d.Effect)
	}
}
