package message

import (
	"fmt"
	"strings"

	"nekocode/interaction/tui/styles"
	"nekocode/protocol"
	"nekocode/util/text"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// TelemetryMessageItem renders a standalone turn footer, used when a failed
// run has no assistant message to attach the metrics to.
type TelemetryMessageItem struct {
	telemetry protocol.Metrics
	sty       *styles.Styles
}

func NewTelemetryMessageItem(sty *styles.Styles, telemetry protocol.Metrics) *TelemetryMessageItem {
	return &TelemetryMessageItem{telemetry: telemetry, sty: sty}
}

func (m *TelemetryMessageItem) Render(width int) string {
	return renderTelemetryFooter(m.telemetry, fullMessageWidth(width), m.sty)
}

func (m *TelemetryMessageItem) Height(width int) int {
	return lipgloss.Height(m.Render(width))
}

// Reduce indentation before reducing content width on very small terminals.
func renderTelemetryFooter(telemetry protocol.Metrics, width int, sty *styles.Styles) string {
	width = max(width, 1)
	padding := min(2, max(width-2, 0))
	return indentLines(renderTelemetryLine(telemetry, width-padding, sty), strings.Repeat(" ", padding))
}

func renderTelemetryLine(telemetry protocol.Metrics, width int, sty *styles.Styles) string {
	width = max(width, 1)
	full := renderFullTelemetry(telemetry, sty)
	// A Chinese glyph occupies two cells; one cell can only show a hint.
	if width == 1 {
		return sty.Subtle.Render("…")
	}
	if lipgloss.Width(full) <= width {
		return full
	}
	// Keep each metric together where possible, and wrap rather than discard
	// token counts when the terminal is narrow.
	separator := sty.Subtle.Render(" · ")
	parts := strings.Split(strings.TrimPrefix(full, sty.Subtle.Render("↳ ")), separator)
	lines := []string{}
	line := sty.Subtle.Render("↳ ")
	for _, part := range parts {
		candidate := line + part
		if lipgloss.Width(candidate) > width && lipgloss.Width(line) > 2 {
			lines = append(lines, strings.TrimSuffix(line, separator))
			line = ""
		}
		if lipgloss.Width(line+part) > width {
			wrapped := strings.Split(ansi.Hardwrap(line+part, width, true), "\n")
			lines = append(lines, wrapped[:len(wrapped)-1]...)
			line = wrapped[len(wrapped)-1] + separator
		} else {
			line += part + separator
		}
	}
	lines = append(lines, strings.TrimSuffix(line, separator))
	return strings.Join(lines, "\n")
}

func renderFullTelemetry(t protocol.Metrics, sty *styles.Styles) string {
	total := t.TurnTotal
	if total <= 0 {
		total = max(0, t.TurnInput) + max(0, t.TurnOutput)
	}
	parts := make([]string, 0, 9)
	if t.Duration != "" {
		parts = append(parts, sty.Subtle.Render(t.Duration))
	}
	parts = append(parts,
		telemetryMetric("总计", text.FormatTokens(total)+" tok", sty.Muted, sty),
		telemetryMetric("输入", text.FormatTokens(t.TurnInput), sty.Muted, sty),
	)
	if t.TurnCacheReported {
		parts = append(parts,
			telemetryMetric("缓存", text.FormatTokens(t.TurnCached), sty.Teal, sty),
			telemetryMetric("未缓存", text.FormatTokens(t.TurnNew), sty.Yellow, sty),
		)
	} else {
		parts = append(parts, telemetryMetric("缓存", "—", sty.Subtle, sty))
		parts = append(parts, telemetryMetric("未缓存", "—", sty.Subtle, sty))
	}
	parts = append(parts, telemetryMetric("输出", text.FormatTokens(t.TurnOutput), sty.Blue, sty))
	if t.TurnReasoning > 0 {
		parts = append(parts, telemetryMetric("推理", text.FormatTokens(t.TurnReasoning), sty.Primary, sty))
	}
	parts = append(parts, telemetryMetric("本次命中", turnHitRatio(t), sty.Teal, sty))
	return sty.Subtle.Render("↳ ") + strings.Join(parts, sty.Subtle.Render(" · "))
}

func turnHitRatio(t protocol.Metrics) string {
	if !t.TurnCacheReported || t.TurnCached+t.TurnNew <= 0 {
		return "—"
	}
	return fmt.Sprintf("%.2f%%", float64(t.TurnCached)*100/float64(t.TurnCached+t.TurnNew))
}

func telemetryMetric(label, value string, valueStyle lipgloss.Style, sty *styles.Styles) string {
	return sty.Subtle.Render(label+" ") + valueStyle.Render(value)
}
