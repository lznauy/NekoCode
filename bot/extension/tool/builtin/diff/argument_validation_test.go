package diff

import (
	"context"
	"math"
	"strings"
	"testing"
)

func TestDiffRequiresExplicitTextInputs(t *testing.T) {
	for _, args := range []map[string]any{{}, {"old": "text"}, {"new": "text"}, {"old": 42, "new": ""}, {"old": "", "new": nil}} {
		out, err := NewTool().Execute(context.Background(), args)
		if err == nil || out.Text != "" {
			t.Fatalf("invalid diff reported a comparison: %+v %q %v", args, out.Text, err)
		}
	}
	for _, args := range []map[string]any{{"old": "", "new": ""}, {"old": "old", "new": ""}} {
		if _, err := NewTool().Execute(context.Background(), args); err != nil {
			t.Fatal(err)
		}
	}
}
func TestDiffRejectsInvalidContext(t *testing.T) {
	for _, v := range []any{1.9, "3", nil, math.Inf(1)} {
		if _, err := NewTool().Execute(context.Background(), map[string]any{"old": "a", "new": "b", "context": v}); err == nil {
			t.Fatalf("invalid context accepted: %v", v)
		}
	}
}

func TestDiffLargeContextIncludesChanges(t *testing.T) {
	out, err := NewTool().Execute(context.Background(), map[string]any{"old": "one\ntwo", "new": "one\nchanged", "context": int(^uint(0) >> 1)})
	if err != nil || !strings.Contains(out.Text, "-2:two") || !strings.Contains(out.Text, "+2:changed") {
		t.Fatalf("large context lost changes: %q %v", out.Text, err)
	}
}
