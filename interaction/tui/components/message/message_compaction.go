package message

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"charm.land/lipgloss/v2"

	"nekocode/interaction/tui/styles"
	"nekocode/protocol"
)

const maxStreamLines = 8

// CompactionItem remains in the display transcript after the run settles.
type CompactionItem struct {
	Event protocol.CompactionEvent
	sty   *styles.Styles
}

func NewCompactionItem(sty *styles.Styles, event protocol.CompactionEvent) *CompactionItem {
	m := &CompactionItem{sty: sty}
	m.Update(event)
	return m
}

func (m *CompactionItem) Running() bool {
	return m.Event.Status == protocol.CompactionStarted || m.Event.Status == protocol.CompactionDelta
}

func (m *CompactionItem) Update(event protocol.CompactionEvent) {
	if m.Event.Status == protocol.CompactionCompleted || m.Event.Status == protocol.CompactionFailed {
		return
	}
	if event.Status == protocol.CompactionDelta {
		event.Summary = m.Event.Summary + event.Delta
	}
	m.Event = event
}

func (m *CompactionItem) Render(width int) string {
	w := max(1, width-4)
	label := "Compacting context"
	nameStyle := m.sty.Blue.Bold(true)
	if m.Event.Status == protocol.CompactionCompleted {
		label = "Compacted"
	}
	if m.Event.Status == protocol.CompactionFailed {
		label = "Compaction incomplete"
		nameStyle = m.sty.Red.Bold(true)
	}
	meta := fmt.Sprintf("%s · %d messages · ~%d tokens", m.Event.Trigger, m.Event.BeforeMessages, m.Event.BeforeTokens)
	if m.Event.Status == protocol.CompactionCompleted {
		meta = fmt.Sprintf("%s · %d → %d messages · ~%d → ~%d tokens · %.1fs", m.Event.Trigger, m.Event.BeforeMessages, m.Event.AfterMessages, m.Event.BeforeTokens, m.Event.AfterTokens, float64(m.Event.ElapsedMs)/1000)
	}
	wrap := func(s string) string { return ansi.Hardwrap(ansi.Strip(s), max(1, w-7), true) }

	// Head line renders like a tool call: bullet + bold name + inline meta.
	// Falls back to two lines when the terminal is too narrow to fit.
	bullet, bulletStyle := styles.BulletForBlock("", -1, m.sty.Teal)
	head := "  " + bulletStyle.Render(bullet) + " " + nameStyle.Render(label)
	var lines []string
	if inline := head + " " + m.sty.Muted.Render(meta); lipgloss.Width(inline) <= w {
		lines = []string{inline}
	} else {
		lines = []string{head, m.sty.Muted.Render(ansi.Hardwrap(ansi.Strip("  "+meta), w, true))}
	}

	// While running, stream the summary tail like tool output. On failure,
	// keep the error (and any partial summary) visible.
	if m.Running() || m.Event.Status == protocol.CompactionFailed || m.Event.Warning != "" {
		summary := strings.TrimSpace(m.Event.Summary)
		summary = strings.TrimPrefix(summary, "<summary>")
		summary = strings.TrimSuffix(summary, "</summary>")
		body := summary
		if m.Event.Status == protocol.CompactionFailed && m.Event.Error != "" {
			if body == "" {
				body = m.Event.Error
			} else {
				body = m.Event.Error + "\n" + body
			}
		}
		if m.Event.Warning != "" {
			if body == "" {
				body = m.Event.Warning
			} else {
				body = m.Event.Warning + "\n" + body
			}
		}
		if body != "" {
			body = wrap(body)
			if m.Running() {
				parts := strings.Split(body, "\n")
				if len(parts) > maxStreamLines {
					parts = parts[len(parts)-maxStreamLines:]
				}
				body = strings.Join(parts, "\n")
			}
			lines = append(lines, m.renderStreamBody(body))
		}
	}
	return strings.Join(lines, "\n")
}

// renderStreamBody prefixes the first line with a tool-output corner and
// aligns continuation lines under it, mirroring block tool output.
func (m *CompactionItem) renderStreamBody(rendered string) string {
	corner := "└"
	if styles.Vertical == "|" {
		corner = "`"
	}
	var out strings.Builder
	first := true
	for line := range strings.SplitSeq(rendered, "\n") {
		if first {
			out.WriteString("    ")
			out.WriteString(m.sty.Border.Render(corner))
			out.WriteString("  ")
			first = false
		} else {
			out.WriteString("       ")
		}
		out.WriteString(m.sty.Muted.Render(line))
		out.WriteByte(10)
	}
	return strings.TrimRight(out.String(), "\n")
}

func (m *CompactionItem) Height(width int) int { return strings.Count(m.Render(width), "\n") + 1 }
