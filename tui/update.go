// update.go — tea.Update 主循环消息分发。
package tui

import (
	"nekocode/common"
	"nekocode/tui/components"
	"nekocode/tui/components/message"

	"charm.land/bubbles/v2/cursor"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
)

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	defer func() {
		if r := recover(); r != nil {
			common.WritePanicLog(r)
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

	case notifyMsg:
		m.Messages.AddMessage(message.ChatMessage{
			Role: "system", Content: msg.content, RenderedContent: msg.content,
		})
		return m, listenNotify(m.notifyCh)

	case confirmMsg:
		if msg.req.Response == nil {
			m.state = stateReady
			m.resizeMessages()
			return m, nil
		}
		m.ConfirmBar.SetRequest(&msg.req)
		m.preConfirmState = m.state
		m.state = stateConfirming
		m.resizeMessages()
		return m, nil

	case questionMsg:
		if msg.req.Response == nil {
			m.state = stateReady
			m.resizeMessages()
			return m, nil
		}
		m.QuestionBar.SetRequest(&msg.req)
		m.preConfirmState = m.state
		m.state = stateQuestioning
		m.resizeMessages()
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
		input, cmd := m.Input.Update(msg)
		m.Input = input
		return m, cmd

	case tea.MouseMsg:
		m.Messages.Update(msg)
		m.Input.SetFollow(m.Messages.Follow)
	}

	return m, nil
}
