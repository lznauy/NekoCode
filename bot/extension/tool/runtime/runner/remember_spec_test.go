package runner

import (
	"reflect"
	"testing"

	"nekocode/bot/extension/tool/runtime/permission"
)

func TestBashRememberRulesKeepsDynamicStatementLiteral(t *testing.T) {
	cmd := `echo "喵~ bash 命令测试成功！当前工作目录: $(pwd)" && date`
	got := permission.ShellRulesToRemember("shell", cmd)
	want := []permission.Rule{
		{Tool: "shell", Literal: `echo "喵~ bash 命令测试成功！当前工作目录: $(pwd)"`, Effect: permission.EffectAllow, Lifetime: permission.LifetimeSession},
		{Tool: "shell", Specifier: "date", Effect: permission.EffectAllow},
	}
	if !reflect.DeepEqual(got, want) {
		// A dynamic statement is one exact unit: no reusable "echo *" prefix.
		t.Fatalf("ShellRulesToRemember() = %#v, want %#v", got, want)
	}
}

func TestBashRememberRulesBroadensOnlyStaticCommands(t *testing.T) {
	got := permission.ShellRulesToRemember("shell", `echo hello && date`)
	wantSpecs := []string{"echo *", "date"}
	var gotSpecs []string
	for _, rule := range got {
		gotSpecs = append(gotSpecs, rule.Specifier)
	}
	if !reflect.DeepEqual(gotSpecs, wantSpecs) {
		t.Fatalf("bashRememberRules() specs = %#v, want %#v", gotSpecs, wantSpecs)
	}
}

func TestPathRememberSpecKeepsWorkspaceRootFileExact(t *testing.T) {
	if got := pathRememberSpec("/repo/go.mod", "/repo", "/home/user"); got != "/go.mod" {
		t.Fatalf("root workspace file should be remembered exactly, got %q", got)
	}
}
