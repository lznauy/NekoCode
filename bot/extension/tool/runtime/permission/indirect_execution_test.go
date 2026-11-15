package permission

import (
	"context"
	"testing"
)

func TestIndirectExecutionRetainsHardDeny(t *testing.T) {
	e := NewEngine(DefaultMatchers())
	e.SetRules(append(append([]Rule{}, builtinRules...), Rule{Tool: "shell", Specifier: "*", Effect: EffectAllow, Source: "user"}))
	for _, command := range []string{
		`ls | xargs sudo -i`, `xargs -0 -n 1 /usr/bin/sudo -i`, `xargs -I{} sudo -u root {}`,
		`xargs --max-procs=2 --arg-file input sudo -i`, `xargs -0rn1 /bin/dd if=x of=y`,
		`xargs -i sudo -i`, `xargs --replace sudo -i`, `xargs --eof sudo -i`, `xargs -o sudo -i`, `xargs --open-tty sudo -i`, `xargs --max-proc=1 sudo -i`, `xargs --rep sudo -i`, `xargs --arg-f input sudo -i`, `xargs -- sudo -i`, `env X=1 xargs sudo -i`,
		`find . -exec sudo -i \;`, `find . -execdir /usr/bin/sudo -i {} +`,
		`find . -ok /bin/dd if=x of=y \;`, `find . -okdir /usr/bin/ssh host \;`,
		`find . -exec xargs sudo -i \;`, `xargs find . -exec sudo -i \;`,
	} {
		t.Run(command, func(t *testing.T) {
			d := e.Evaluate("shell", CallInfoForBash(command), EffectAsk)
			if d.Effect != EffectDeny {
				t.Fatalf("effect=%s rule=%q signals=%v", d.Effect, d.Rule.Specifier, d.Assessment.Signals)
			}
		})
	}
}

func TestIndirectExecutionCannotReusePrefixGrant(t *testing.T) {
	for _, command := range []string{`ls | xargs echo`, `find . -exec echo {} \;`, `find . -execdir echo {} +`} {
		e := NewEngine(DefaultMatchers())
		e.SetRules(append(append([]Rule{}, builtinRules...), Rule{Tool: "shell", Specifier: "*", Effect: EffectAllow, Source: "user"}))
		d := e.Evaluate("shell", CallInfoForBash(command), EffectAsk)
		if d.Effect != EffectAsk || len(d.Assessment.Signals) == 0 {
			t.Errorf("%q: effect=%s signals=%v", command, d.Effect, d.Assessment.Signals)
		}
		grants := e.PendingShellRules("shell", command)
		if len(grants) != 1 || grants[0].Literal != command || grants[0].Lifetime != LifetimeSession {
			t.Errorf("%q got reusable grants: %+v", command, grants)
		}
	}
}

func TestFindDataIsNotAnExecutedCommand(t *testing.T) {
	for _, command := range []string{`find . -name sudo -print`, `find . -name '-exec' -print`, `echo 'xargs sudo -i'`, `find . -path "$PATTERN" -print`} {
		e := NewEngine(DefaultMatchers())
		e.SetRules(builtinRules)
		d := e.Evaluate("shell", CallInfoForBash(command), EffectAsk)
		if d.Effect == EffectDeny || len(d.Assessment.Signals) != 0 {
			t.Errorf("%q falsely classified: %+v", command, d)
		}
	}
	if BashRuleMatches("sudo *", CallInfoForBash(`xargs sudo -i`), MatchAllSubcommands) {
		t.Fatal("indirect command projection broadened allow")
	}
}

func TestUnsupportedIndirectExecutionCannotBeApproved(t *testing.T) {
	for _, command := range []string{
		`xargs "$PROGRAM"`, `xargs -I{} '{}'`, `printf sudo | xargs -I CMD CMD -i`, `printf sudo | xargs --replace=CMD CMD -i`, `xargs -I{} echo {}`, `xargs --rep echo`, `xargs --unknown echo`,
		`find . $OPTIONS`, `find . -name $PATTERN`, `find . -path "$@"`,
		`find . -unknown -exec echo {} \;`, `find . -exec xargs --rep echo \;`,
	} {
		t.Run(command, func(t *testing.T) {
			e := NewEngine(DefaultMatchers())
			e.SetRules([]Rule{{Tool: "shell", Specifier: "*", Effect: EffectAllow, Source: "user"}, {Tool: "shell", Literal: command, Effect: EffectAllow, Source: "session"}})
			e.SetAutoJudge(func(_ context.Context, _ string) (bool, bool) {
				t.Fatal("unsupported indirect command reached auto judge")
				return true, true
			})
			if d := e.Evaluate("shell", CallInfoForBash(command), EffectAllow); d.Effect != EffectDeny {
				t.Fatalf("unsupported syntax bypassed hard boundary: %+v", d)
			}
			if _, denied := e.DenyRule("shell", CallInfoForBash(command)); !denied {
				t.Fatal("preview and execution boundaries disagree")
			}
		})
	}
}
