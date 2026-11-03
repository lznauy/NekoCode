package permission

import "testing"

func TestMultiFileToolRulesUseActualTargets(t *testing.T) {
	for _, tc := range []struct {
		name string
		args map[string]any
	}{
		{"image_gen", map[string]any{"output_dir": "/repo/private/out"}},
		{"image_understand", map[string]any{"path": "/repo/private/a.png"}},
		{"image_understand", map[string]any{"paths": []string{"/repo/public/a.png", "/repo/private/b.png"}}},
		{"diff", map[string]any{"old": "path:/repo/public/a", "new": "path: /repo/private/b", "path": "/repo/public/header"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := NewEngine(DefaultMatchers())
			e.SetRules([]Rule{{Tool: tc.name, Specifier: "/private/**", Effect: EffectDeny, Source: "project"}})
			d := e.Evaluate(tc.name, BuildCallInfo(tc.name, tc.args, "/repo", "/home/user"), EffectAllow)
			if d.Effect != EffectDeny {
				t.Fatalf("actual file target escaped scoped deny: %+v", d)
			}
		})
	}
}

func TestFileDisplayHeaderIsNotAuthority(t *testing.T) {
	e := NewEngine(DefaultMatchers())
	e.SetRules([]Rule{{Tool: "diff", Specifier: "/private/**", Effect: EffectDeny, Source: "project"}})
	for _, args := range []map[string]any{{"old": "text", "new": "other", "path": "/repo/private/header"}, {"old": "path:/repo/public/a", "new": "text", "path": "/repo/private/header"}} {
		if d := e.Evaluate("diff", BuildCallInfo("diff", args, "/repo", "/home/user"), EffectAllow); d.Effect != EffectAllow {
			t.Fatalf("display header treated as file authority: %+v", d)
		}
	}
}

func TestMultiPathAllowMustCoverEveryInput(t *testing.T) {
	for _, tool := range []string{"diff", "image_understand"} {
		for _, effect := range []Effect{EffectAllow, EffectAsk, EffectDeny} {
			e := NewEngine(DefaultMatchers())
			e.SetRules([]Rule{{Tool: tool, Specifier: "/public/**", Effect: effect, Source: "project"}})
			args := map[string]any{"paths": []string{"/repo/public/a", "/repo/private/b"}}
			if tool == "diff" {
				args = map[string]any{"old": "path:/repo/public/a", "new": "path:/repo/private/b"}
			}
			fallback := EffectAllow
			want := effect
			if effect == EffectAllow {
				fallback = EffectAsk
				want = EffectAsk
			}
			if d := e.Evaluate(tool, BuildCallInfo(tool, args, "/repo", "/home/user"), fallback); d.Effect != want {
				t.Errorf("tool=%s effect=%v: %+v", tool, effect, d)
			}
		}
	}
}

func TestImageGenerationDefaultOutputParticipatesInPathRules(t *testing.T) {
	e := NewEngine(DefaultMatchers())
	e.SetRules([]Rule{{Tool: "image_gen", Specifier: "~/.nekocode/**", Effect: EffectDeny, Source: "project"}})
	if d := e.Evaluate("image_gen", BuildCallInfo("image_gen", nil, "/repo", "/home/user"), EffectAsk); d.Effect != EffectDeny {
		t.Fatalf("default image directory skipped path deny: %+v", d)
	}
}
