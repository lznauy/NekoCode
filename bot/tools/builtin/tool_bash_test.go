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
}
