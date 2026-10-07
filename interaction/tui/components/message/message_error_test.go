package message

import (
	"strings"
	"testing"

	"nekocode/interaction/tui/styles"

	"github.com/charmbracelet/x/ansi"
)

func TestErrorMessageRendersAsDotItem(t *testing.T) {
	sty := styles.DefaultStyles()
	item := NewErrorMessageItem(&sty, "auto compact failed: 待摘要上下文超过摘要模型窗口")
	out := item.Render(100)

	if strings.Contains(out, "▐") {
		t.Fatalf("error message still uses the retired left bar: %q", out)
	}
	stripped := ansi.Strip(out)
	if !strings.HasPrefix(stripped, "  • ") {
		t.Fatalf("error message missing bullet prefix: %q", stripped)
	}
	if !strings.Contains(stripped, "auto compact failed") {
		t.Fatalf("error content missing: %q", stripped)
	}
	// Continuation lines keep the shared 4-space indent.
	for _, line := range strings.Split(stripped, "\n")[1:] {
		if strings.TrimSpace(line) != "" && !strings.HasPrefix(line, "    ") {
			t.Fatalf("continuation line lost its indent: %q", line)
		}
	}
	if item.Height(100) < 1 {
		t.Fatalf("height = %d", item.Height(100))
	}
}
