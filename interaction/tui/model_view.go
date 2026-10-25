// view.go — tea.View 视图布局组装。
package tui

import (
	"strings"
	"time"

	"nekocode/interaction/tui/styles"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func (m *Model) View() tea.View {
	if !m.Ready {
		return tea.NewView("Loading...")
	}

	overlay := m.tasks != nil && m.tasks.open && m.state != stateConfirming && m.state != stateQuestioning
	if overlay && m.tasks.background != "" && m.tasks.backgroundWidth == m.Width && m.tasks.backgroundHeight == m.Height {
		m.Messages.SetRenderingSuspended(true)
		return m.taskOverlayTeaView(m.tasks.background)
	}
	// Refresh the background once on opening/resize, and render the live main
	// view whenever approval or question controls temporarily own focus.
	m.Messages.SetRenderingSuspended(false)
	if !overlay && m.tasks != nil {
		m.tasks.background = ""
	}

	var parts []string

	if m.Messages.Len() == 0 {
		parts = append(parts, m.Splash.View())
	} else {
		// Horizontal scrollbar sits on top of the header when content overflows.
		totalHeight, viewportHeight, percent := m.Messages.ScrollbarMetrics()
		m.Scrollbar.Update(totalHeight, viewportHeight, percent)
		if hbar := m.Scrollbar.ViewHorizontal(m.Width); hbar != "" {
			parts = append(parts, hbar)
		}
		parts = append(parts, m.sessionHeaderView(time.Now()))

		msgWidth := m.Width
		if m.Messages.Width() != msgWidth {
			m.Messages.SetSize(msgWidth, m.Messages.Height())
		}

		parts = append(parts, lipgloss.NewStyle().Width(msgWidth).Render(m.Messages.Render()))
	}

	if m.state == stateConfirming {
		if bar := m.ConfirmBar.View(m.Width, m.Height); bar != "" {
			parts = append(parts, bar)
		}
	}
	questionIdx := -1
	if m.state == stateQuestioning {
		if bar := m.QuestionBar.View(m.Width, m.Height); bar != "" {
			questionIdx = len(parts)
			parts = append(parts, bar)
		}
	}

	parts = append(parts, "", m.Input.View())

	if sug := m.Suggestions.View(m.Width); sug != "" {
		parts = append(parts, sug)
	}

	view := lipgloss.JoinVertical(lipgloss.Left, parts...)

	if overlay {
		m.tasks.renderedView = ""
		m.tasks.background = view
		m.tasks.backgroundWidth, m.tasks.backgroundHeight = m.Width, m.Height
		m.Messages.SetRenderingSuspended(true)
		return m.taskOverlayTeaView(view)
	}

	v := tea.NewView(view)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion

	var c *tea.Cursor
	if m.state == stateQuestioning {
		// The question bar owns keyboard focus while it is open: show the
		// terminal cursor on its custom input row when active, and hide the
		// main input cursor otherwise so focus is not misleading.
		if qc := m.QuestionBar.Cursor(m.Width, m.Height); qc != nil && questionIdx >= 0 {
			for _, p := range parts[:questionIdx] {
				qc.Y += lipgloss.Height(p)
			}
			c = qc
		}
	} else {
		c = m.Input.Cursor()
		if c != nil {
			// Input is at parts[-1] (no suggestions) or parts[-2] (with suggestions).
			inputIdx := len(parts) - 1
			if m.Suggestions.Visible() {
				inputIdx = len(parts) - 2
			}
			for _, p := range parts[:inputIdx] {
				c.Y += lipgloss.Height(p)
			}
		}
	}
	v.Cursor = c

	return v
}

func (m *Model) taskOverlayTeaView(background string) tea.View {
	content := m.tasks.renderedView
	if !m.tasks.reuseView || content == "" {
		content = m.taskOverlayView(background)
		m.tasks.renderedView = content
	}
	v := tea.NewView(content)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

const sessionLoadHintDelay = 200 * time.Millisecond

func (m *Model) sessionHeaderView(now time.Time) string {
	header := m.Header.View()
	if !m.sessionLoading || m.sessionLoadStarted.IsZero() || now.Sub(m.sessionLoadStarted) < sessionLoadHintDelay {
		if hint := m.Messages.HistoryWindowHint(); hint != "" {
			firstLine, _, _ := strings.Cut(header, "\n")
			return firstLine + "\n" + styles.MutedStyle.Render(ansi.Truncate(hint, max(1, m.Width), "…")) + "\n"
		}
		return header
	}
	// Reuse the header's separator row so showing the hint never resizes or
	// scrolls the message viewport. Existing spinner ticks repaint this row.
	firstLine, _, _ := strings.Cut(header, "\n")
	hint := ansi.Truncate(m.Spinner.View()+" "+m.sessionLoadLabel, max(1, m.Width), "…")
	return firstLine + "\n" + styles.MutedStyle.Render(hint) + "\n"
}
