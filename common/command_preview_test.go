package common

import (
	"strings"
	"testing"
)

func TestFormatCommandPreviewShortCommand(t *testing.T) {
	cmd := "go test ./bot/..."
	if got := FormatCommandPreview(cmd, 80); got != cmd {
		t.Fatalf("preview = %q, want %q", got, cmd)
	}
}

func TestFormatCommandPreviewLongCommandKeepsHeadAndTail(t *testing.T) {
	cmd := "python3 -c " + strings.Repeat("print('x');", 20) + " --important-tail"
	got := FormatCommandPreview(cmd, 80)
	if len([]rune(got)) > 80 {
		t.Fatalf("preview length = %d, want <= 80: %q", len([]rune(got)), got)
	}
	if !strings.HasPrefix(got, "python3 -c ") {
		t.Fatalf("preview lost command head: %q", got)
	}
	if !strings.Contains(got, "…") {
		t.Fatalf("preview missing ellipsis: %q", got)
	}
	if !strings.HasSuffix(got, "--important-tail") {
		t.Fatalf("preview lost command tail: %q", got)
	}
}

func TestFormatCommandPreviewMultilineCommandShowsShape(t *testing.T) {
	cmd := "python3 <<'PY'\nprint('hello')\nprint('done')\nPY"
	got := FormatCommandPreview(cmd, 96)
	if !strings.Contains(got, "python3 <<'PY'") {
		t.Fatalf("preview lost first line: %q", got)
	}
	if !strings.Contains(got, "PY") {
		t.Fatalf("preview lost last line: %q", got)
	}
	if !strings.Contains(got, "+3 lines") {
		t.Fatalf("preview missing extra line count: %q", got)
	}
	if strings.Contains(got, "\n") {
		t.Fatalf("preview should be single line: %q", got)
	}
}
