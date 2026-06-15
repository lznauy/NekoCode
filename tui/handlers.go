// handlers.go — 按键处理 + 完成处理 + spinner tick + 调试日志。
package tui

import (
	"fmt"
	"strings"
	"time"

	"nekocode/tui/components/block"
	"nekocode/tui/components/message"
	"nekocode/tui/components/processing"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
)

const (
	contentMarginV = 2
)

// --- done ---

func (m *Model) handleDone(msg doneMsg) tea.Cmd {
	finalBlocks := block.FilterFinalBlocks(m.Messages.ProcessingBlocks())

	// Use msg.content (the final chat output) as primary rendered content.
	// AccumulatedText() may include intermediate turn text when PostTurn hooks
	// trigger additional agent loops, so fall back to it only when final output
	// is empty.
	accumulated := strings.TrimSpace(msg.content)
	if accumulated == "" {
		accumulated = strings.TrimSpace(m.Messages.AccumulatedText())
	}
	m.transitionTo(stateReady)

	if msg.err != nil {
		// Preserve tool blocks even on error — show what was attempted.
		if len(finalBlocks) > 0 {
			m.Messages.AddMessage(message.ChatMessage{
				Role:   "assistant",
				Blocks: finalBlocks,
			})
		}
		m.Messages.AddMessage(message.ChatMessage{
			Role:    "error",
			Content: fmt.Sprintf("Error: %v", msg.err),
		})
	} else {
		footer := ""
		if msg.duration != "" || msg.tokens != "" {
			footer = "Duration: " + msg.duration
			if msg.tokens != "" {
				footer += "  " + msg.tokens
			}
		}
		m.Messages.AddMessage(message.ChatMessage{
			Role:            "assistant",
			Content:         msg.content,
			RenderedContent: accumulated,
			Footer:          footer,
			Blocks:          finalBlocks,
		})
	}

	st := m.Bot.Stats()
	m.Header.SetTokens(st.PromptTokens + st.CompletionTokens)
	if m.Messages.Follow {
		m.Messages.GotoBottom()
	}
	return nil
}

// --- keys: confirm ---

func (m *Model) handleConfirmKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter", "y", "Y":
		m.ConfirmBar.Respond(true)
	case "esc", "n", "N", "ctrl+c":
		m.ConfirmBar.Respond(false)
	default:
		return m, nil
	}
	m.state = m.preConfirmState
	m.resizeMessages()
	if m.state == stateProcessing {
		return m, tea.Batch(listenConfirm(m.confirmCh), spinnerTick())
	}
	return m, nil
}

// --- keys: dispatch ---

func (m *Model) handleKeyPress(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "ctrl+c":
		return tea.Quit

	case "ctrl+e":
		if m.state != stateProcessing {
			m.Messages.ToggleLastAssistant()
		}
		return nil

	case "up":
		if m.Suggestions.Visible() {
			m.Suggestions.Cycle(-1)
		} else if m.state == stateProcessing {
			m.Messages.Update(msg)
		} else if m.Input.CanCursorUp() {
			input, cmd := m.Input.Update(msg)
			m.Input = input
			return cmd
		} else {
			m.Input.HistoryUp()
		}
		return nil
	case "down":
		if m.Suggestions.Visible() {
			m.Suggestions.Cycle(1)
		} else if m.state == stateProcessing {
			m.Messages.Update(msg)
		} else if m.Input.CanCursorDown() {
			input, cmd := m.Input.Update(msg)
			m.Input = input
			return cmd
		} else {
			m.Input.HistoryDown()
		}
		return nil

	case "pgup", "pgdown":
		m.Messages.Update(msg)
		m.Input.SetFollow(m.Messages.Follow)
		return nil
	}

	if m.state == stateProcessing {
		return m.handleProcessingKey(msg)
	}

	return m.handleIdleKey(msg)
}

func (m *Model) handleProcessingKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "enter":
		value := m.Input.Value()
		if value != "" {
			m.Suggestions.Hide()
			m.resizeMessages()
			m.Input.AddHistory(value)
			m.Input.Reset()
			m.Messages.AddMessage(message.ChatMessage{Role: "user", Content: value})
			m.Messages.ClearProcessing()
			m.Messages.SetBlocks(nil)
			m.Messages.GotoBottom()
			m.Input.SetFollow(true)
			m.processingStart = time.Now()
			m.processingPhase = phaseSteer
			m.Messages.SetProcessingStatus(phaseSteer)
			m.Bot.Steer(value)
		}
	case "esc":
		m.Bot.Abort()
		m.Messages.SetProcessingStatus("Aborted")
	default:
		input, cmd := m.Input.Update(msg)
		m.Input = input
		return cmd
	}
	return nil
}

func (m *Model) handleIdleKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "end":
		m.Messages.GotoBottom()
		m.Input.SetFollow(true)
	case "tab":
		m.cycleSuggestion(1)
		return nil
	case "shift+tab":
		m.cycleSuggestion(-1)
		return nil
	case "esc":
		if m.Suggestions.Visible() {
			m.Suggestions.Hide()
			m.resizeMessages()
			return nil
		}
	case "enter":
		if m.Suggestions.Visible() {
			m.acceptSuggestion()
			return nil
		}
		value := m.Input.Value()
		if value == "" {
			m.Messages.GotoBottom()
			m.Input.SetFollow(true)
			return nil
		}
		m.Suggestions.Hide()
		m.resizeMessages()
		m.Input.AddHistory(value)
		m.Input.Reset()
		return m.startChat(value)
	default:
		input, cmd := m.Input.Update(msg)
		m.Input = input
		m.refreshSuggestions()
		return cmd
	}
	return nil
}

// --- suggestions ---

func (m *Model) refreshSuggestions() {
	m.Suggestions.Refresh(m.Input.Value(), m.Bot.CommandNames())
	m.resizeMessages()
}

func (m *Model) acceptSuggestion() {
	if val := m.Suggestions.Accept(); val != "" {
		m.Input.SetValue(val)
		m.Input.SetCursorEnd()
		m.resizeMessages()
	}
}

func (m *Model) cycleSuggestion(delta int) {
	m.Suggestions.Cycle(delta)
}

// --- spinner ---

func (m *Model) handleSpinnerTick(msg spinner.TickMsg) tea.Cmd {
	m.Spinner, _ = m.Spinner.Update(msg)

	if m.state == stateConfirming {
		m.Messages.SetSpinnerView("")
		return nil
	}

	if m.state == stateProcessing {
		elapsed := time.Since(m.processingStart)
		statusText := fmt.Sprintf("%s (%.1fs)", m.processingPhase, elapsed.Seconds())
		st := m.Bot.Stats()
		if st.PromptTokens == 0 {
			st.PromptTokens = st.ContextTokens
		}
		spinnerView := m.Spinner.View()
		m.Messages.UpdateProcessing(func(p *processing.ProcessingItem) {
			p.SetSpinnerView(spinnerView)
			p.SetStatusText(statusText)
			p.SetTokens(st.PromptTokens, st.CompletionTokens)
			p.SetCompactCount(st.CompactCount)
		})

		return spinnerTick()
	}

	return nil
}

func spinnerTick() tea.Cmd {
	return func() tea.Msg {
		time.Sleep(100 * time.Millisecond)
		return spinner.TickMsg{}
	}
}
