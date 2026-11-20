package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"nekocode/interaction/tui/styles"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

type sessionRenamer interface{ RenameSession(string, string) error }

type sessionNameEditor struct {
	id, menu, err string
	input         textinput.Model
}

func sessionMenuInput(value string) bool {
	value = strings.TrimSpace(value)
	return value == "/sessions"
}

func (m *Model) sessionNameFocused() bool {
	return m.sessionName != nil && (m.state == stateReady || m.state == stateProcessing)
}

func (m *Model) requestSessionRename() tea.Cmd {
	if m.sessionMenu == "" || !m.Suggestions.IsMenu() || m.sessionLoading {
		return nil
	}
	if _, ok := m.Runtime.(sessionRenamer); !ok {
		return nil
	}
	selected, ok := m.Suggestions.Selected()
	if !ok || selected.Key == "" {
		return nil
	}
	input := textinput.New()
	input.Prompt = "tag › "
	sty := styles.DefaultStyles()
	inputStyles := input.Styles()
	inputStyles.Focused = textinput.StyleState{Text: sty.Base, Prompt: sty.Primary.Bold(true), Placeholder: sty.Subtle, Suggestion: sty.Muted}
	inputStyles.Blurred = textinput.StyleState{Text: sty.Muted, Prompt: sty.Subtle, Placeholder: sty.Subtle, Suggestion: sty.Subtle}
	inputStyles.Cursor.Color = lipgloss.Color(styles.Primary)
	input.SetStyles(inputStyles)
	input.Placeholder = "为选中的会话添加标签"
	input.CharLimit = 80
	input.SetValue(selected.Tag)
	// An outstanding paste belongs to the draft that had focus before editing.
	m.clipboardGeneration++
	m.sessionName = &sessionNameEditor{id: selected.Key, menu: m.sessionMenu, input: input}
	m.sessionName.resize(m.Width)
	m.resizeMessages()
	return m.sessionName.input.Focus()
}

func (m *Model) handleSessionNameKey(msg tea.KeyPressMsg) tea.Cmd {
	editor := m.sessionName
	switch msg.String() {
	case "esc", "ctrl+c":
		m.closeSessionNameEditor()
		return nil
	case "enter":
		if err := m.Runtime.(sessionRenamer).RenameSession(editor.id, editor.input.Value()); err != nil {
			editor.err = err.Error()
			return nil
		}
		m.closeSessionNameEditor()
		return nil
	}
	return editor.updateInput(msg)
}

func (e *sessionNameEditor) updateInput(msg tea.Msg) tea.Cmd {
	before := e.input.Value()
	var cmd tea.Cmd
	e.input, cmd = e.input.Update(msg)
	if e.input.Value() != before {
		e.err = ""
	}
	return cmd
}

func (m *Model) closeSessionNameEditor() {
	editor := m.sessionName
	m.sessionName = nil
	m.reopenSessionMenu(editor.menu, editor.id)
}

func (m *Model) refreshOpenSessionMenu() {
	if m.sessionName != nil || m.sessionMenu == "" || !m.Suggestions.IsMenu() {
		return
	}
	selected, _ := m.Suggestions.Selected()
	m.reopenSessionMenu(m.sessionMenu, selected.Key)
}

func (m *Model) reopenSessionMenu(menu, selectedID string) {
	if !m.openCommandMenu(menu) {
		m.Suggestions.Hide()
		m.sessionMenu = ""
		m.resizeMessages()
		return
	}
	first, ok := m.Suggestions.Selected()
	if !ok || first.Key == selectedID {
		return
	}
	// Keep the selected session by ID, even when display names match.
	for {
		m.Suggestions.Cycle(1)
		selected, _ := m.Suggestions.Selected()
		if selected.Key == selectedID || selected.Key == first.Key {
			return
		}
	}
}

const sessionNameHeight = 2

func (e *sessionNameEditor) resize(width int) {
	e.input.SetWidth(max(1, width-3-ansi.StringWidth(e.input.Prompt)))
}

func (e *sessionNameEditor) view(width int) string {
	sty := styles.DefaultStyles()
	hint := "Enter 保存 · Esc 取消 · 留空清除"
	if ansi.StringWidth(hint)+2 > width {
		hint = "Enter 保存 · Esc 取消"
	}
	if ansi.StringWidth(hint)+2 > width {
		hint = "Enter / Esc"
	}
	hint = sty.Subtle.Render(hint)
	if e.err != "" {
		hint = sty.Red.Render(ansi.Strip(strings.Join(strings.Fields(e.err), " ")))
	}
	return ansi.Truncate(sty.Primary.Render(styles.HeavyVert)+" "+e.input.View(), max(1, width), "…") + "\n" + ansi.Truncate("  "+hint, max(1, width), "…")
}
