package components

import (
	"strings"
	"testing"

	"nekocode/protocol"

	"github.com/charmbracelet/x/ansi"
)

func TestHeaderFitsVeryNarrowTerminal(t *testing.T) {
	for _, width := range []int{1, 6, 10, 20} {
		for _, line := range strings.Split(NewHeader(width, "dev").View(), "\n") {
			if ansi.StringWidth(line) > width {
				t.Fatalf("header exceeds %d columns: %q", width, line)
			}
		}
	}
}

func TestHeaderShowsOperationalStatusWithoutModelOrTotalTokens(t *testing.T) {
	h := NewHeader(180, "dev")
	h.SetContext(14500, 1_000_000, 800_000, 9940, 60)
	h.SetWorkspace(protocol.WorkspaceChanges{Added: 2263, Deleted: 882, Untracked: 5, Available: true})
	view := ansi.Strip(h.View())
	for _, want := range []string{"平均命中 99.40%", "上下文 14.5k (1%)", "距压缩 79%", "工作区 +2,263 −882 · 未跟踪 5"} {
		if !strings.Contains(view, want) {
			t.Fatalf("header missing %q:\n%s", want, view)
		}
	}
	for _, unwanted := range []string{"deepseek", "8.2k", "本次命中"} {
		if strings.Contains(view, unwanted) {
			t.Fatalf("header retained %q:\n%s", unwanted, view)
		}
	}
}

func TestHeaderUsesCompactLayoutAtNarrowWidth(t *testing.T) {
	h := NewHeader(80, "dev")
	h.SetContext(14500, 1_000_000, 800_000, 9940, 60)
	h.SetWorkspace(protocol.WorkspaceChanges{Added: 2263, Deleted: 882, Untracked: 5, Available: true})
	view := ansi.Strip(h.View())
	for _, line := range strings.Split(strings.TrimSuffix(view, "\n"), "\n") {
		if ansi.StringWidth(line) > 80 {
			t.Fatalf("header width = %d, want <= 80:\n%s", ansi.StringWidth(line), view)
		}
	}
	if !strings.Contains(view, "上下文") || !strings.Contains(view, "距压") {
		t.Fatalf("narrow header lost essential context status:\n%s", view)
	}
	if !strings.Contains(view, "git +2,263 −882 ?5") {
		t.Fatalf("narrow header lost workspace status:\n%s", view)
	}
}

func TestFormatCountUsesThousandsSeparators(t *testing.T) {
	for value, want := range map[int]string{0: "0", 8: "8", 976: "976", 2545: "2,545", 1234567: "1,234,567"} {
		if got := formatCount(value); got != want {
			t.Errorf("formatCount(%d) = %q, want %q", value, got, want)
		}
	}
}
