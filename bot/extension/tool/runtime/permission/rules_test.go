package permission

import (
	"context"
	"testing"
)

// stubMatcher matches when specifier == callInfo["match"].
type stubMatcher struct{}

func (stubMatcher) Match(spec string, info map[string]any) (bool, error) {
	v, _ := info["match"].(string)
	return spec == v, nil
}

func newTestEngine() *Engine {
	return NewEngine(map[string]SpecifierMatcher{
		"bash": stubMatcher{},
		"edit": stubMatcher{},
		"read": stubMatcher{},
	})
}

func TestParseRule(t *testing.T) {
	cases := []struct {
		in     string
		effect Effect
		want   Rule
	}{
		{"Bash(npm run *)", EffectAllow, Rule{Tool: "Bash", Specifier: "npm run *", Effect: EffectAllow, Source: "test"}},
		{"Read(./.env)", EffectDeny, Rule{Tool: "Read", Specifier: "./.env", Effect: EffectDeny, Source: "test"}},
		{"Bash", EffectAsk, Rule{Tool: "Bash", Effect: EffectAsk, Source: "test"}},
		{"mcp__github__*", EffectAllow, Rule{Tool: "mcp__github__*", Effect: EffectAllow, Source: "test"}},
	}
	for _, c := range cases {
		got, err := ParseRule(c.in, c.effect, "test")
		if err != nil {
			t.Fatalf("ParseRule(%q): %v", c.in, err)
		}
		if got != c.want {
			t.Errorf("ParseRule(%q) = %+v, want %+v", c.in, got, c.want)
		}
	}
}

func TestParseRuleErrors(t *testing.T) {
	bad := []string{"", "(", ")", "()", "(spec)", "Tool(spec", "Tool)spec)"}
	for _, s := range bad {
		if _, err := ParseRule(s, EffectAllow, "test"); err == nil {
			t.Errorf("expected error for %q", s)
		}
	}
}

func TestEvaluateDenyWins(t *testing.T) {
	e := newTestEngine()
	e.SetRules([]Rule{
		{Tool: "bash", Specifier: "rm", Effect: EffectAllow, Source: "test"},
		{Tool: "bash", Specifier: "rm", Effect: EffectDeny, Source: "test"},
	})
	d := e.EvaluateContext(context.Background(), "bash", map[string]any{"match": "rm"}, EffectAllow)
	if d.Effect != EffectDeny {
		t.Fatalf("deny must win, got %v", d.Effect)
	}
}

func TestEvaluateAskBeforeAllow(t *testing.T) {
	e := newTestEngine()
	e.SetRules([]Rule{
		{Tool: "bash", Specifier: "git push", Effect: EffectAllow, Source: "test"},
		{Tool: "bash", Specifier: "git push", Effect: EffectAsk, Source: "test"},
	})
	d := e.EvaluateContext(context.Background(), "bash", map[string]any{"match": "git push"}, EffectAllow)
	if d.Effect != EffectAsk {
		t.Fatalf("ask must beat allow, got %v", d.Effect)
	}
}

func TestEvaluateAllowMatches(t *testing.T) {
	e := newTestEngine()
	e.SetRules([]Rule{
		{Tool: "bash", Specifier: "npm run", Effect: EffectAllow, Source: "test"},
	})
	d := e.EvaluateContext(context.Background(), "bash", map[string]any{"match": "npm run"}, EffectDeny)
	if d.Effect != EffectAllow {
		t.Fatalf("allow rule should match, got %v", d.Effect)
	}
}

func TestEvaluateNoMatchReturnsDefault(t *testing.T) {
	e := newTestEngine()
	e.SetRules([]Rule{
		{Tool: "bash", Specifier: "ls", Effect: EffectAllow, Source: "test"},
	})
	d := e.EvaluateContext(context.Background(), "bash", map[string]any{"match": "rm"}, EffectAsk)
	if d.Effect != EffectAsk {
		t.Fatalf("no match → default, got %v", d.Effect)
	}
}

func TestEvaluateBareToolMatchesAll(t *testing.T) {
	e := newTestEngine()
	e.SetRules([]Rule{
		{Tool: "bash", Effect: EffectAllow, Source: "test"}, // bare, no specifier
		{Tool: "bash", Specifier: "rm", Effect: EffectDeny, Source: "test"},
	})
	// deny is more specific but bare-allow comes first; deny still wins by precedence.
	d := e.EvaluateContext(context.Background(), "bash", map[string]any{"match": "rm"}, EffectAllow)
	if d.Effect != EffectDeny {
		t.Fatalf("deny should win over bare allow, got %v", d.Effect)
	}
	// bare allow covers anything else
	d = e.EvaluateContext(context.Background(), "bash", map[string]any{"match": "anything"}, EffectDeny)
	if d.Effect != EffectAllow {
		t.Fatalf("bare allow should match unmatched calls, got %v", d.Effect)
	}
}

func TestEvaluateStarToolMatchesAnyTool(t *testing.T) {
	e := newTestEngine()
	e.SetRules([]Rule{
		{Tool: "*", Specifier: "dangerous", Effect: EffectDeny, Source: "test"},
	})
	d := e.EvaluateContext(context.Background(), "bash", map[string]any{"match": "dangerous"}, EffectAllow)
	if d.Effect != EffectDeny {
		t.Fatalf("* rule should match any tool, got %v", d.Effect)
	}
}

func TestEvaluateUnregisteredToolScopedRuleSkipped(t *testing.T) {
	e := newTestEngine() // no matcher for "web_fetch"
	e.SetRules([]Rule{
		{Tool: "web_fetch", Specifier: "domain:evil.com", Effect: EffectDeny, Source: "test"},
	})
	// No matcher → scoped rule can't be evaluated → treated as non-matching.
	d := e.EvaluateContext(context.Background(), "web_fetch", map[string]any{}, EffectAllow)
	if d.Effect != EffectAllow {
		t.Fatalf("unmatched scoped rule with no matcher should fall to default, got %v", d.Effect)
	}
}

func TestEvaluateDifferentToolsDontInterfere(t *testing.T) {
	e := newTestEngine()
	e.SetRules([]Rule{
		{Tool: "bash", Specifier: "rm", Effect: EffectDeny, Source: "test"},
		{Tool: "edit", Specifier: "/etc", Effect: EffectDeny, Source: "test"},
		{Tool: "edit", Specifier: "/src", Effect: EffectAllow, Source: "test"},
	})
	// bash rm deny shouldn't affect edit
	d := e.EvaluateContext(context.Background(), "edit", map[string]any{"match": "/src"}, EffectAsk)
	if d.Effect != EffectAllow {
		t.Fatalf("edit allow should match /src, got %v", d.Effect)
	}
}

func TestSandboxRulesMatchBashProfiles(t *testing.T) {
	rules, err := LoadSandboxRules(PermissionsDecl{Sandbox: map[string]SandboxProfile{
		"Bash(git status *)": {
			SandboxMode:   "read-only",
			Network:       true,
			WritableRoots: []string{"/cache"},
		},
	}})
	if err != nil {
		t.Fatalf("LoadSandboxRules: %v", err)
	}
	e := NewEngine(DefaultMatchers())
	e.SetSandboxRules(rules)

	profile, ok := e.SandboxFor("shell", BuildCallInfo("shell", map[string]any{
		"command": "git status --short",
	}, "/repo", "/home/user"))
	if !ok {
		t.Fatal("expected sandbox profile match")
	}
	if profile.SandboxMode != "read-only" || !profile.Network || len(profile.WritableRoots) != 1 || profile.WritableRoots[0] != "/cache" {
		t.Fatalf("unexpected profile: %+v", profile)
	}
}

func TestSandboxRulesPreferMostSpecificMatch(t *testing.T) {
	rules, err := LoadSandboxRules(PermissionsDecl{Sandbox: map[string]SandboxProfile{
		"Bash(*)":            {SandboxMode: "read-only"},
		"Bash(npm install*)": {SandboxMode: "workspace-write", Network: true},
	}})
	if err != nil {
		t.Fatalf("LoadSandboxRules: %v", err)
	}
	e := NewEngine(DefaultMatchers())
	e.SetSandboxRules(rules)

	profile, ok := e.SandboxFor("shell", BuildCallInfo("shell", map[string]any{
		"command": "npm install",
	}, "/repo", "/home/user"))
	if !ok {
		t.Fatal("expected sandbox profile match")
	}
	if profile.SandboxMode != "workspace-write" || !profile.Network {
		t.Fatalf("specific sandbox profile should win, got %+v", profile)
	}
}

func TestSandboxRulesRejectNonShellTools(t *testing.T) {
	_, err := LoadSandboxRules(PermissionsDecl{Sandbox: map[string]SandboxProfile{
		"Read(./foo)": {SandboxMode: "read-only"},
	}})
	if err == nil {
		t.Fatal("expected non-shell sandbox rule to be rejected")
	}
}

func TestSandboxRulesRejectInvalidMode(t *testing.T) {
	_, err := LoadSandboxRules(PermissionsDecl{Sandbox: map[string]SandboxProfile{
		"Bash(*)": {SandboxMode: "danger-full-access"},
	}})
	if err == nil {
		t.Fatal("expected invalid sandbox mode to be rejected")
	}
}

func TestSandboxRulesTrimMode(t *testing.T) {
	rules, err := LoadSandboxRules(PermissionsDecl{Sandbox: map[string]SandboxProfile{
		"Bash(*)": {SandboxMode: " read-only "},
	}})
	if err != nil {
		t.Fatalf("LoadSandboxRules: %v", err)
	}
	if len(rules) != 1 || rules[0].Profile.SandboxMode != "read-only" {
		t.Fatalf("sandbox mode should be normalized, got %+v", rules)
	}
}

// newBashEngine builds an engine with the real matchers so bash semantics
// (compound commands, subcommand coverage) are exercised end to end.
func newBashEngine(rules ...Rule) *Engine {
	e := NewEngine(DefaultMatchers())
	e.SetRules(rules)
	return e
}

func bashCall(cmd string) map[string]any {
	return map[string]any{"command": cmd}
}

func TestEvaluateBashCompoundCoveredByUnionOfAllows(t *testing.T) {
	e := newBashEngine(
		Rule{Tool: "bash", Specifier: "npm run *", Effect: EffectAllow, Source: "user"},
		Rule{Tool: "bash", Specifier: "npm test", Effect: EffectAllow, Source: "user"},
	)
	d := e.EvaluateContext(context.Background(), "bash", bashCall("npm run build && npm test"), EffectAsk)
	if d.Effect != EffectAllow {
		t.Fatalf("two narrow allows should jointly cover the compound call, got %v", d.Effect)
	}
	if d.Rule.Specifier != "npm run *" {
		t.Fatalf("deciding rule should be the first covering spec, got %+v", d.Rule)
	}
}

func TestEvaluateBashCompoundPartiallyCoveredFallsThrough(t *testing.T) {
	e := newBashEngine(
		Rule{Tool: "bash", Specifier: "npm run *", Effect: EffectAllow, Source: "user"},
	)
	d := e.EvaluateContext(context.Background(), "bash", bashCall("npm run build && rm -rf /tmp/x"), EffectAsk)
	if d.Effect != EffectAsk {
		t.Fatalf("uncovered subcommand must fall through to the default, got %v", d.Effect)
	}
}

func TestEvaluateBashUserAllowCoverageCountsBuiltinAllows(t *testing.T) {
	e := newBashEngine(
		Rule{Tool: "bash", Specifier: "cargo *", Effect: EffectAllow, Source: "user"},
		Rule{Tool: "bash", Specifier: "ls *", Effect: EffectAllow, Source: "builtin"},
	)
	d := e.EvaluateContext(context.Background(), "bash", bashCall("cargo build && ls -la"), EffectAsk)
	if d.Effect != EffectAllow {
		t.Fatalf("user + builtin allows should jointly cover the compound call, got %v", d.Effect)
	}
	if d.Rule.Specifier != "cargo *" {
		t.Fatalf("user rule should be the deciding rule, got %+v", d.Rule)
	}
}

func TestEvaluateBashBareAllowCoversCompound(t *testing.T) {
	bare := Rule{Tool: "bash", Effect: EffectAllow, Source: "user"}
	e := newBashEngine(
		Rule{Tool: "bash", Specifier: "npm run *", Effect: EffectAllow, Source: "user"},
		bare,
	)
	d := e.EvaluateContext(context.Background(), "bash", bashCall("npm run build && rm -rf /tmp/x"), EffectAsk)
	if d.Effect != EffectAllow {
		t.Fatalf("bare allow should cover the whole compound call, got %v", d.Effect)
	}
	if d.Rule.Specifier != "" {
		t.Fatalf("bare rule should be the deciding rule, got %+v", d.Rule)
	}
}

func TestEvaluateBashDenyFiresOnAnySubcommand(t *testing.T) {
	e := newBashEngine(
		Rule{Tool: "bash", Specifier: "rm *", Effect: EffectDeny, Source: "user"},
		Rule{Tool: "bash", Specifier: "ls *", Effect: EffectAllow, Source: "user"},
	)
	d := e.EvaluateContext(context.Background(), "bash", bashCall("ls -la && rm -rf /tmp/x"), EffectAllow)
	if d.Effect != EffectDeny {
		t.Fatalf("deny must fire on any matching subcommand, got %v", d.Effect)
	}
	// command substitution is a subcommand too
	d = e.EvaluateContext(context.Background(), "bash", bashCall("echo $(rm -rf /tmp/x)"), EffectAllow)
	if d.Effect != EffectDeny {
		t.Fatalf("deny must fire inside command substitution, got %v", d.Effect)
	}
}

func TestEvaluateBashAskFiresOnAnySubcommand(t *testing.T) {
	e := newBashEngine(
		Rule{Tool: "bash", Specifier: "git push *", Effect: EffectAsk, Source: "user"},
	)
	d := e.EvaluateContext(context.Background(), "bash", bashCall("git status && git push origin main"), EffectAllow)
	if d.Effect != EffectAsk {
		t.Fatalf("ask must fire on any matching subcommand, got %v", d.Effect)
	}
}

func TestEvaluateBashBuiltinAllowCoverage(t *testing.T) {
	e := newBashEngine(
		Rule{Tool: "bash", Specifier: "ls *", Effect: EffectAllow, Source: "builtin"},
	)
	d := e.EvaluateContext(context.Background(), "bash", bashCall("ls -la && ls /tmp"), EffectDeny)
	if d.Effect != EffectAllow {
		t.Fatalf("builtin allow covering every subcommand should allow, got %v", d.Effect)
	}
}

func TestEvaluateBashSingleCommandUsesPlainMatch(t *testing.T) {
	e := newBashEngine(
		Rule{Tool: "bash", Specifier: "npm run *", Effect: EffectAllow, Source: "user"},
	)
	d := e.EvaluateContext(context.Background(), "bash", bashCall("npm run build"), EffectAsk)
	if d.Effect != EffectAllow {
		t.Fatalf("single command matching an allow rule should allow, got %v", d.Effect)
	}
	if d.Rule.Specifier != "npm run *" {
		t.Fatalf("deciding rule should be the allow rule, got %+v", d.Rule)
	}
}

func TestEvaluateBashLiteralAllowIsExact(t *testing.T) {
	literal := `bash -c "$(cat task.txt)"`
	e := newBashEngine(Rule{Tool: "shell", Literal: literal, Effect: EffectAllow, Source: "remembered"})
	if d := e.EvaluateContext(context.Background(), "shell", bashCall(literal), EffectAsk); d.Effect != EffectAllow {
		t.Fatalf("exact literal should allow, got %+v", d)
	}
	if d := e.EvaluateContext(context.Background(), "shell", bashCall(literal+" extra"), EffectAsk); d.Effect != EffectAsk {
		t.Fatalf("literal must not match a longer command, got %+v", d)
	}
}

func TestEvaluateBashLiteralDoesNotBecomeBareCompoundAllow(t *testing.T) {
	e := newBashEngine(Rule{Tool: "shell", Literal: "git status", Effect: EffectAllow, Source: "remembered"})
	if d := e.EvaluateContext(context.Background(), "shell", bashCall("git status && rm -rf build"), EffectAsk); d.Effect != EffectAsk {
		t.Fatalf("literal rule must not cover a compound command, got %+v", d)
	}
}

func TestEvaluateBashBroadAllowCannotBypassWrappedDynamicCalls(t *testing.T) {
	e := newBashEngine(Rule{Tool: "shell", Specifier: "*", Effect: EffectAllow, Source: "project"})
	for _, command := range []string{
		`env -S "bash -c 'echo ok'"`,
		`env -u HOME bash -c 'echo ok'`,
		`timeout 5 bash -c 'echo ok'`,
		`nice -n 5 bash -c 'echo ok'`,
	} {
		if decision := e.EvaluateContext(context.Background(), "shell", bashCall(command), EffectAsk); decision.Effect != EffectAsk {
			t.Errorf("wrapped dynamic command %q = %v, want ask", command, decision.Effect)
		}
	}
}
