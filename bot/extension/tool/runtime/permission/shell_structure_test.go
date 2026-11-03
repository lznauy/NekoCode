package permission

import (
	"slices"
	"testing"
)

func TestClassifyShellStructureDynamic(t *testing.T) {
	tests := []struct {
		command string
		want    ShellStructure
	}{
		{`echo "$(cat task.txt)"`, ShellCommandSubstitution},
		{`diff <(sort a) <(sort b)`, ShellProcessSubstitution},
		{`$COMMAND --flag`, ShellDynamicCommand},
		{`env "$COMMAND" --flag`, ShellDynamicCommand},
		{`command "$COMMAND" --flag`, ShellDynamicCommand},
		{`eval "$COMMAND"`, ShellEval},
		{`builtin eval "$COMMAND"`, ShellEval},
		{`source ./script.sh`, ShellSource},
		{`. ./script.sh`, ShellSource},
		{`bash -c 'echo ok'`, ShellCommandString},
		{`env FOO=bar bash -lc 'echo ok'`, ShellCommandString},
		{`env -- FOO=bar bash -lc 'echo ok'`, ShellCommandString},
		{`env -- FOO=bar echo ok`, ShellEnvironment},
		{`env -u HOME bash -c 'echo ok'`, ShellCommandString},
		{`env -S "bash -c 'echo ok'"`, ShellDynamicCommand},
		{`timeout 5 bash -c 'echo ok'`, ShellCommandString},
		{`nice -n 5 bash -c 'echo ok'`, ShellCommandString},
		{`time -p bash -c 'echo ok'`, ShellCommandString},
		{`stdbuf -oL bash -c 'echo ok'`, ShellCommandString},
		{`/usr/local/bin/bash -c 'echo ok'`, ShellCommandString},
		{`busybox sh -c 'echo ok'`, ShellCommandString},
		{"bash <<'EOF'\necho ok\nEOF", ShellHeredocCode},
		{`bash <<< 'echo ok'`, ShellHeredocCode},
		{`echo "unterminated`, ShellUnparseable},
	}
	for _, test := range tests {
		t.Run(test.command, func(t *testing.T) {
			report := ClassifyShellStructure(test.command)
			if !report.Dynamic() || !slices.Contains(report.Structures, test.want) {
				t.Fatalf("ClassifyShellStructure(%q) = %#v, want %q", test.command, report, test.want)
			}
		})
	}
}

func TestClassifyShellStructureStatic(t *testing.T) {
	for _, command := range []string{
		`git status`,
		`'git' status`,
		`cat <<'EOF'
plain data
EOF`,
		`printf '%s\n' '$HOME'`,
		`bash script.sh`,
		`bash --norc script.sh`,
	} {
		if report := ClassifyShellStructure(command); report.Dynamic() {
			t.Fatalf("ClassifyShellStructure(%q) = %#v, want static", command, report)
		}
	}
}
