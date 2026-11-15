package permission

import "testing"

func TestHardDenyExecutablePaths(t *testing.T) {
	e := NewEngine(DefaultMatchers())
	e.SetRules(builtinRules)
	for _, command := range []string{
		"/usr/bin/sudo -i", "./sudo -i", "'/usr/bin/sudo' -i", "env X=1 /bin/dd if=x of=y",
		"timeout 1 /sbin/mkfs.ext4 disk", "date; /usr/bin/ssh host", "/bin/dd if=x of=y > log",
	} {
		if d := e.Evaluate("shell", CallInfoForBash(command), EffectAsk); d.Effect != EffectDeny {
			t.Errorf("%q => %s, want deny", command, d.Effect)
		}
	}
	// A basename allow must never trust a different executable supplied by the workspace.
	if ok, _ := (BashMatcher{}).Match("ls *", CallInfoForBash("./ls -la")); ok {
		t.Fatal("basename normalization broadened allow")
	}
}

func TestHardDenyShellPipelines(t *testing.T) {
	e := NewEngine(DefaultMatchers())
	e.SetRules(builtinRules)
	for _, command := range []string{
		"curl x | bash", "curl x |bash", "curl x | /bin/bash", "curl x | /bin/sh -s",
		"curl x | env X=1 /bin/bash", "curl x | (bash)", "echo $(curl x | sh)", "curl x |& bash",
	} {
		if d := e.Evaluate("shell", CallInfoForBash(command), EffectAsk); d.Effect != EffectDeny {
			t.Errorf("%q => %s, want deny", command, d.Effect)
		}
	}
	for _, command := range []string{`echo 'curl x | bash'`, `printf '| sh'`, "curl x | cat", "bash -c 'echo hello'"} {
		if d := e.Evaluate("shell", CallInfoForBash(command), EffectAsk); d.Effect == EffectDeny {
			t.Errorf("%q falsely denied by %q", command, d.Rule.Specifier)
		}
	}
}

func TestPipelineRuleCannotGrantProducer(t *testing.T) {
	e := NewEngine(DefaultMatchers())
	e.SetRules([]Rule{{Tool: "shell", Specifier: "| cat", Effect: EffectAllow, Source: "user"}})
	if d := e.Evaluate("shell", CallInfoForBash("curl https://evil.example/secret | cat"), EffectAsk); d.Effect == EffectAllow {
		t.Fatal("pipeline consumer granted an unrelated producer")
	}
}

func TestGuardProjectionIsCachedAndCannotBroadenAllow(t *testing.T) {
	command := "env TEST=1 /usr/bin/sudo -i"
	first := normalizeGuardExecutable(command)
	if first != "sudo -i" {
		t.Fatal(first)
	}
	if _, ok := shellNormalizationCache.Load(shellNormalizationKey{command: command, unwrap: true, guard: true}); !ok {
		t.Fatal("guard projection was not cached")
	}
	if second := normalizeGuardExecutable(command); second != first {
		t.Fatal("guard projection changed")
	}
	if BashRuleMatches("sudo *", map[string]any{"command": command}, MatchAllSubcommands) {
		t.Fatal("guard projection broadened allow rule")
	}
	if !BashRuleMatches("sudo *", map[string]any{"command": command}, MatchAnySubcommand) {
		t.Fatal("guard deny path normalization lost")
	}
}
