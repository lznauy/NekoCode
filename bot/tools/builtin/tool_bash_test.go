package builtin

import (
	"context"
	"testing"

	"nekocode/common"
)

func TestBashTool(t *testing.T) {
	b := &BashTool{}

	out, err := b.Execute(context.Background(), map[string]any{"command": "echo hello"})
	if err != nil {
		t.Fatalf("bash: %v", err)
	}
	if out != "hello\n" {
		t.Errorf("output = %q, want %q", out, "hello\n")
	}

	_, err = b.Execute(context.Background(), nil)
	if err == nil {
		t.Error("expected error for missing command")
	}

	if b.DangerLevel(map[string]any{"command": "rm -rf /"}) != common.LevelDestructive {
		t.Error("rm -rf should be destructive")
	}
	if b.DangerLevel(map[string]any{"command": "ls"}) != common.LevelSafe {
		t.Error("ls should be safe")
	}

	// cat > file: writes via redirection, must not be LevelSafe.
	if b.DangerLevel(map[string]any{"command": "cat > /tmp/test_edit.go << 'EOF'"}) != common.LevelWrite {
		t.Error("cat > file should be LevelWrite (writes via redirection)")
	}

	// cat heredoc with Go source code: must not be LevelForbidden.
	cmd := "cat > /tmp/test_edit.go << 'EOF'\npackage main\n\nfunc main() {}\nEOF"
	if b.DangerLevel(map[string]any{"command": cmd}) == common.LevelForbidden {
		t.Error("cat heredoc with Go code should not be LevelForbidden (heredoc body stripped before matching)")
	}

	// stripHeredocBodies unit tests
	if got := stripHeredocBodies("cat > /tmp/x << 'EOF'\ncode\nEOF"); got != "cat > /tmp/x " {
		t.Errorf("stripHeredocBodies = %q, want %q", got, "cat > /tmp/x ")
	}
	if got := stripHeredocBodies("ls -la"); got != "ls -la" {
		t.Errorf("stripHeredocBodies = %q, want %q", got, "ls -la")
	}

	// hasWriteRedirection unit tests
	if !hasWriteRedirection("cat > /tmp/x") {
		t.Error("cat > /tmp/x should have write redirection")
	}
	if hasWriteRedirection("cat /tmp/x") {
		t.Error("cat /tmp/x should not have write redirection")
	}
	if hasWriteRedirection("echo > /dev/null") {
		t.Error("echo > /dev/null should not have write redirection")
	}
	if !hasWriteRedirection("cat >> /tmp/x") {
		t.Error("cat >> /tmp/x (append) should have write redirection")
	}
	if hasWriteRedirection("echo hello") {
		t.Error("echo hello should not have write redirection")
	}
	// Bare > inside a quoted string must NOT trigger write redirection.
	if hasWriteRedirection(`echo "a>b"`) {
		t.Error(`echo "a>b" should not have write redirection (bare > inside quoted string)`)
	}
	// Bare > inside a heredoc delimiter area should NOT trigger (strip happens first).
	if hasWriteRedirection(`sort file`) {
		t.Error("sort file should not have write redirection")
	}

	// " > " inside a quoted string must NOT trigger write redirection.
	if hasWriteRedirection(`echo "foo > bar"`) {
		t.Error(`echo "foo > bar" should not have write redirection ( > inside double quotes)`)
	}
	if hasWriteRedirection(`echo 'foo > bar'`) {
		t.Error(`echo 'foo > bar' should not have write redirection ( > inside single quotes)`)
	}
	// Leading redirect without space: ">file".
	if !hasWriteRedirection(">file") {
		t.Error(">file should have write redirection (no space after >)")
	}
	// << inside quoted strings should not truncate stripHeredocBodies incorrectly.
	if got := stripHeredocBodies(`echo "a<<b"`); got != `echo "a<<b"` {
		t.Errorf(`stripHeredocBodies for echo "a<<b" = %q, want %q`, got, `echo "a<<b"`)
	}

	// Compact redirect variants (no space between operator and path).
	if !hasWriteRedirection(`cmd2>/tmp/log`) {
		t.Error(`cmd2>/tmp/log should have write redirection (compact 2>)`)
	}
	if !hasWriteRedirection(`cmd1>/tmp/log`) {
		t.Error(`cmd1>/tmp/log should have write redirection (compact 1>)`)
	}
	if !hasWriteRedirection(`cmd&>/tmp/log`) {
		t.Error(`cmd&>/tmp/log should have write redirection (compact &>)`)
	}
	// Compact redirect with quoted target should not trigger.
	if hasWriteRedirection(`cmd2>/dev/null`) {
		t.Error(`cmd2>/dev/null should not have write redirection`)
	}

}
