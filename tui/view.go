// view.go — tea.View 视图布局组装。
package tui
import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

func (m *Model) View() tea.View {
	if !m.Ready {
		return tea.NewView("Loading...")
	}

	var parts []string

	if m.Messages.Len() == 0 {
		parts = append(parts, m.Splash.View())
	} else {
		parts = append(parts, m.Header.View())

		m.Scrollbar.Update(
			m.Messages.TotalContentHeight(),
			m.Messages.Height(),
			m.Messages.ScrollPercent(),
		)

		msgView := lipgloss.NewStyle().Width(m.Width - 1).Render(m.Messages.Render())
		barView := m.Scrollbar.View()
		row := msgView
		if barView != "" {
			row = lipgloss.JoinHorizontal(lipgloss.Top, msgView, barView)
		}
		parts = append(parts, row)
	}

	if m.state == stateConfirming {
		if bar := m.ConfirmBar.View(m.Width, m.Height); bar != "" {
			parts = append(parts, bar)
		}
	}

	parts = append(parts, "", m.Input.View())

	if sug := m.Suggestions.View(m.Width); sug != "" {
		parts = append(parts, sug)
	}

	view := lipgloss.JoinVertical(lipgloss.Left, parts...)

	v := tea.NewView(view)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion

	c := m.Input.Cursor()
	if c != nil {
		// Input is at parts[-1] (no suggestions) or parts[-2] (with suggestions).
		inputIdx := len(parts) - 1
		if m.Suggestions.Visible() {
			inputIdx = len(parts) - 2
		}
		inputY := 0
		for _, p := range parts[:inputIdx] {
			inputY += lipgloss.Height(p)
		}
		c.Position.Y += inputY
	}
	v.Cursor = c

	return v
}
