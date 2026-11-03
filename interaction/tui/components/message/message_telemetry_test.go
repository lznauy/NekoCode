package message

import (
	"strings"
	"testing"

	"nekocode/interaction/tui/styles"
	"nekocode/protocol"

	"github.com/charmbracelet/x/ansi"
)

func TestTelemetryRendersReadableTurnVocabulary(t *testing.T) {
	sty := styles.DefaultStyles()
	item := NewTelemetryMessageItem(&sty, protocol.Metrics{
		Duration: "16.3s", TurnTotal: 22_597, TurnInput: 21_865, TurnCached: 21_200, TurnNew: 665, TurnOutput: 732, TurnReasoning: 283, TurnCacheReported: true,
	})
	clean := ansi.Strip(item.Render(180))
	want := "↳ 16.3s · 总计 22.6k tok · 输入 21.9k · 缓存 21.2k · 未缓存 665 · 输出 732 · 推理 283 · 本次命中 96.96%"
	if !strings.Contains(clean, want) {
		t.Fatalf("full telemetry = %q, want %q", clean, want)
	}
}

func TestTelemetryWrapsWithoutDroppingMetrics(t *testing.T) {
	sty := styles.DefaultStyles()
	item := NewTelemetryMessageItem(&sty, protocol.Metrics{
		Duration: "16.3s", TurnInput: 14_575, TurnCached: 14_500, TurnNew: 75, TurnOutput: 12, TurnReasoning: 3, TurnCacheReported: true,
	})
	for _, width := range []int{20, 36, 60, 80, 180} {
		clean := ansi.Strip(item.Render(width))
		for _, want := range []string{"16.3s", "总计", "输入 14.6k", "缓存 14.5k", "未缓存 75", "输出 12", "推理 3", "本次命中 99.49%"} {
			if !strings.Contains(clean, want) {
				t.Fatalf("width %d missing %q: %q", width, want, clean)
			}
		}
		for _, line := range strings.Split(clean, "\n") {
			if ansi.StringWidth(line) > width {
				t.Fatalf("width %d overflow: %q", width, line)
			}
		}
		if item.Height(width) != len(strings.Split(clean, "\n")) {
			t.Fatalf("width %d: incorrect telemetry height", width)
		}
	}
}

func TestTelemetryMarksUnknownCacheUsage(t *testing.T) {
	sty := styles.DefaultStyles()
	item := NewTelemetryMessageItem(&sty, protocol.Metrics{TurnInput: 1000, TurnOutput: 12})
	clean := ansi.Strip(item.Render(80))
	if !strings.Contains(clean, "缓存 —") || !strings.Contains(clean, "未缓存 —") || !strings.Contains(clean, "本次命中 —") {
		t.Fatalf("unknown cache usage looked like a real miss: %q", clean)
	}
	if strings.Contains(clean, "推理") {
		t.Fatalf("zero reasoning tokens should be omitted: %q", clean)
	}
}

func TestTelemetryFitsExtremeWidths(t *testing.T) {
	sty := styles.DefaultStyles()
	item := NewTelemetryMessageItem(&sty, protocol.Metrics{TurnInput: 1000, TurnOutput: 12})
	for width := 1; width <= 24; width++ {
		rendered := item.Render(width)
		for _, line := range strings.Split(rendered, "\n") {
			if ansi.StringWidth(line) > width {
				t.Errorf("width %d overflow: %q", width, ansi.Strip(line))
				break
			}
		}
	}
}

func TestAssistantTelemetryWrapKeepsMetricsAndHeightOnResize(t *testing.T) {
	sty := styles.DefaultStyles()
	item := NewAssistantMessageItem(&sty, "answer")
	item.SetTelemetry(protocol.Metrics{TurnInput: 1000, TurnCached: 900, TurnNew: 100, TurnOutput: 12, TurnCacheReported: true})
	for _, width := range []int{80, 20, 36, 180, 20} {
		rendered := item.Render(width)
		clean := ansi.Strip(rendered)
		for _, want := range []string{"输入 1.0k", "缓存 900", "未缓存 100", "输出 12", "本次命中 90.00%"} {
			if !strings.Contains(clean, want) {
				t.Fatalf("width %d missing %q: %q", width, want, clean)
			}
		}
		for _, line := range strings.Split(clean, "\n") {
			if ansi.StringWidth(line) > width {
				t.Fatalf("width %d overflow: %q", width, line)
			}
		}
		if item.Height(width) != len(strings.Split(clean, "\n")) {
			t.Fatalf("width %d: wrong assistant height", width)
		}
	}
}

func TestTelemetryPreservesAllTextAcrossWidths(t *testing.T) {
	sty := styles.DefaultStyles()
	metrics := protocol.Metrics{Duration: "16.3s", TurnInput: 21865, TurnOutput: 732, TurnCached: 21200, TurnNew: 665, TurnReasoning: 283, TurnCacheReported: true}
	normalize := func(value string) string {
		return strings.Map(func(r rune) rune {
			switch r {
			case ' ', '\n', '\t', '·':
				return -1
			}
			return r
		}, ansi.Strip(value))
	}
	expected := normalize(renderFullTelemetry(metrics, &sty))
	for width := 2; width <= 200; width++ {
		rendered := renderTelemetryLine(metrics, width, &sty)
		if got := normalize(rendered); got != expected {
			t.Fatalf("width %d lost or duplicated text: %q, want %q", width, got, expected)
		}
		for _, line := range strings.Split(rendered, "\n") {
			if ansi.StringWidth(line) > width {
				t.Fatalf("width %d overflowed", width)
			}
		}
	}
}
