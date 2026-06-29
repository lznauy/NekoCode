package contextguard

import (
	"strings"
	"testing"

	"nekocode/bot/llm/types"
)

func TestApplyToolResultGuardrailInjectsWarning(t *testing.T) {
	msgs := make([]types.Message, 0, 3)
	for range 3 {
		msgs = append(msgs, types.Message{Role: "tool", Content: "x"})
	}
	last := 0
	got := ApplyToolResultGuardrailWithLimits(msgs, &last, 2, 1)
	if len(got) != 4 {
		t.Fatalf("expected warning appended, got %d messages", len(got))
	}
	if last != 3 {
		t.Fatalf("last warned = %d", last)
	}
	if got[3].Role != "user" || !strings.Contains(got[3].Content, "3 tool results accumulated") {
		t.Fatalf("unexpected warning: %+v", got[3])
	}
}

func TestApplyToolResultGuardrailHonorsInterval(t *testing.T) {
	msgs := []types.Message{{Role: "tool"}, {Role: "tool"}, {Role: "tool"}}
	last := 3
	got := ApplyToolResultGuardrailWithLimits(msgs, &last, 2, 10)
	if len(got) != len(msgs) {
		t.Fatalf("warning should not be appended")
	}
}
