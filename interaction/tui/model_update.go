// update.go — tea.Update 主循环消息分发。
package tui

import (
	"strings"

	"nekocode/interaction/tui/components"
	controlruntime "nekocode/runtime"
	"nekocode/util/runtime"

	"charm.land/bubbles/v2/cursor"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
)

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	defer func() {
		if r := recover(); r != nil {
			runtime.WritePanicLog(r)
		}
	}()

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.Width = max(msg.Width, 10)
		m.Height = max(msg.Height, 10)
		m.Ready = true

		m.Header.SetWidth(msg.Width)
		m.Input.SetWidth(msg.Width)
		m.Splash.SetSize(msg.Width, msg.Height)

		m.resizeMessages()
		return m, nil

	case spinner.TickMsg:
		return m, m.handleSpinnerTick(msg)

	case doneMsg:
		return m, m.handleDone(msg)

	case runtimeEventMsg:
		return m, tea.Batch(m.handleRuntimeEvent(msg.event), listenRuntimeEvent(m.runtimeEvents))

	case workspaceChangesMsg:
		m.Header.SetWorkspace(controlruntime.WorkspaceChanges(msg))
		return m, nil

	case tea.KeyPressMsg:
		if m.state == stateConfirming {
			return m.handleConfirmKey(msg)
		}
		if m.state == stateQuestioning {
			return m.handleQuestionKey(msg)
		}
		return m, m.handleKeyPress(msg)

	case components.TickMsg:
		if m.Messages.Len() == 0 {
			m.Splash.Blink()
			return m, components.BlinkTick()
		}
		return m, nil

	case cursor.BlinkMsg:
		input, cmd := m.Input.Update(msg)
		m.Input = input
		return m, cmd

	case tea.PasteMsg:
		if m.state == stateQuestioning {
			text := strings.NewReplacer("\r\n", " ", "\r", " ", "\n", " ").Replace(msg.Content)
			m.QuestionBar.Type(text)
			return m, nil
		}
		input, cmd := m.Input.Update(msg)
		m.Input = input
		m.refreshSuggestions()
		return m, cmd

	case tea.MouseMsg:
		m.Messages.Update(msg)
		m.Input.SetFollow(m.Messages.Follow)
	}

	return m, nil
}
