package hooks

import (
	"strings"
	"testing"
)

func TestFormatHints(t *testing.T) {
	hints := []Hint{
		{Type: "quota", Severity: "warning", Content: "one"},
		{Type: "verification", Severity: "critical", Content: "two"},
	}
	s := FormatHints(hints)
	if !strings.Contains(s, `<hints>`) {
		t.Error("missing hints wrapper")
	}
	if !strings.Contains(s, "type=\"quota\"") {
		t.Error("missing quota hint")
	}
	if !strings.Contains(s, "type=\"verification\"") {
		t.Error("missing verification hint")
	}
}

func TestFormatHintsDefaultsSeverity(t *testing.T) {
	out := FormatHints([]Hint{{Type: "notice", Content: "hello"}})
	if !strings.Contains(out, `severity="info"`) {
		t.Fatalf("formatted hint = %q, want default info severity", out)
	}
}
