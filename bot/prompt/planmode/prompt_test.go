package planmode

import (
	"strings"
	"testing"
)

func TestPromptIncludesTaskAndBlocksWrites(t *testing.T) {
	got := Prompt("inspect only")
	if !strings.Contains(got, "inspect only") {
		t.Fatalf("missing task: %q", got)
	}
	if !strings.Contains(got, "BLOCKED: write, edit, bash") {
		t.Fatalf("missing blocked tool rule: %q", got)
	}
}
