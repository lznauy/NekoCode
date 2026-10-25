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

	if s := m.tasks; s != nil {
		s.reuseView = false
		if event, ok := msg.(runtimeEventMsg); ok {
			switch event.event.Type {
			case controlruntime.EventAssistantDelta, controlruntime.EventAssistantReset,
				controlruntime.EventReasoningDelta, controlruntime.EventMetricsUpdated, controlruntime.EventPhaseChanged:
				// Main-only stream updates do not change the visible task panel.
				s.reuseView = true
			}
		}
	}

	if handled, cmd := m.handleTaskMessage(msg); handled {
		return m, cmd
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.terminalWidth = max(1, msg.Width)
		m.terminalHeight = max(1, msg.Height)
		m.Width = max(msg.Width, 10)
		m.Height = max(msg.Height, 10)
		m.Ready = true

		m.Header.SetWidth(msg.Width)
		m.Input.SetWidth(msg.Width)
		m.Splash.SetSize(msg.Width, msg.Height)

		m.resizeMessages()
		m.resizeTasks()
		return m, nil

	case spinner.TickMsg:
		return m, m.handleSpinnerTick(msg)

	case sessionLoadedMsg:
		return m, m.applySessionLoaded(msg)

	case doneMsg:
		return m, m.handleDone(msg)

	case clipboardImageMsg:
		return m, m.handleClipboardImage(msg)

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
		if m.tasks != nil && m.tasks.open {
			return m, m.handleTaskKey(msg)
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
		if m.tasks != nil && m.tasks.open {
			if m.tasks.searching {
				text := strings.Join(strings.Fields(msg.Content), " ")
				m.tasks.query = string([]rune(m.tasks.query + text)[:min(256, len([]rune(m.tasks.query+text)))])
				m.tasks.listOffset = 0
				return m, m.loadTasks()
			}
			return m, nil
		}
		if msg.Content == "" {
			return m, readClipboardImage()
		}
		input, cmd := m.Input.Update(msg)
		m.Input = input
		m.refreshSuggestions()
		return m, cmd

	case tea.MouseMsg:
		if m.tasks != nil && m.tasks.open && m.state != stateConfirming && m.state != stateQuestioning {
			key := ""
			switch msg.Mouse().Button {
			case tea.MouseWheelUp:
				key = "up"
			case tea.MouseWheelDown:
				key = "down"
			}
			if key != "" {
				return m, m.handleTaskKey(tea.KeyPressMsg{Code: map[string]rune{"up": tea.KeyUp, "down": tea.KeyDown}[key]})
			}
			return m, nil
		}
		_, cmd := m.Messages.Update(msg)
		m.Input.SetFollow(m.Messages.Follow)
		return m, cmd
	}

	return m, nil
}
