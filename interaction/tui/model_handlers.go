// handlers.go — 按键处理 + 完成处理 + spinner tick + 调试日志。
package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"nekocode/interaction"
	"nekocode/interaction/tui/components/block"
	"nekocode/interaction/tui/components/message"
	"nekocode/interaction/tui/components/processing"
	controlruntime "nekocode/runtime"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
)

const (
	contentMarginV = 2
)

// --- done ---

func (m *Model) handleDone(msg doneMsg) tea.Cmd {
	m.Messages.SettleCompactions()
	finalBlocks := block.FilterFinalBlocks(m.Messages.ProcessingBlocks())

	// Use msg.content (the final chat output) as primary rendered content.
	// AccumulatedText() may include intermediate turn text when Stop hooks
	// trigger additional agent loops, so fall back to it only when final output
	// is empty.
	accumulated := strings.TrimSpace(msg.content)
	if accumulated == "" {
		accumulated = strings.TrimSpace(m.Messages.AccumulatedText())
	}
	m.transitionTo(stateReady)
	var telemetry *controlruntime.MetricsSnapshot
	if msg.metrics.HasTurnUsage() {
		telemetry = &msg.metrics
	}

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
		if telemetry != nil {
			m.Messages.AddMessage(message.ChatMessage{Role: "telemetry", Telemetry: telemetry})
		}
	} else {
		if accumulated == "" && len(finalBlocks) == 0 {
			if telemetry != nil {
				m.Messages.AddMessage(message.ChatMessage{Role: "telemetry", Telemetry: telemetry})
			}
			m.refreshRuntimeStatus()
			return loadWorkspaceChanges(m.Runtime)
		}
		footer := ""
		if telemetry == nil && msg.metrics.Duration != "" {
			footer = "Duration: " + msg.metrics.Duration
		}
		m.Messages.AddMessage(message.ChatMessage{
			Role:            "assistant",
			Content:         msg.content,
			RenderedContent: accumulated,
			Footer:          footer,
			Telemetry:       telemetry,
			Blocks:          finalBlocks,
		})
	}

	m.refreshRuntimeStatus()
	if m.Messages.Follow {
		m.Messages.GotoBottom()
	}
	return loadWorkspaceChanges(m.Runtime)
}

// --- keys: confirm ---

func (m *Model) handleConfirmKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		m.ConfirmBar.Submit()
	case "y", "Y":
		if m.ConfirmBar.IsDestructive() {
			m.ConfirmBar.Respond(true, false)
		} else {
			m.ConfirmBar.Submit()
		}
	case "up":
		m.ConfirmBar.Move(-1)
		return m, nil
	case "down":
		m.ConfirmBar.Move(1)
		return m, nil
	case "esc", "n", "N", "ctrl+c":
		m.ConfirmBar.Respond(false, false)
	default:
		return m, nil
	}
	m.state = m.preConfirmState
	m.resizeMessages()
	if m.state == stateProcessing {
		return m, spinnerTick()
	}
	return m, nil
}

func (m *Model) handleQuestionKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := tea.Key(msg)
	if key.Text != "" && key.Text != " " {
		m.QuestionBar.Type(key.Text)
		return m, nil
	}
	switch msg.String() {
	case "up":
		m.QuestionBar.Move(-1)
		return m, nil
	case "down", "tab":
		m.QuestionBar.Move(1)
		return m, nil
	case "backspace":
		m.QuestionBar.Backspace()
		return m, nil
	case "delete":
		m.QuestionBar.DeleteForward()
		return m, nil
	case "left":
		m.QuestionBar.MoveCustomCursor(-1)
		return m, nil
	case "right":
		m.QuestionBar.MoveCustomCursor(1)
		return m, nil
	case "home", "ctrl+a":
		m.QuestionBar.CustomCursorHome()
		return m, nil
	case "end", "ctrl+e":
		m.QuestionBar.CustomCursorEnd()
		return m, nil
	case "space":
		if m.QuestionBar.CustomActive() {
			m.QuestionBar.Type(" ")
			return m, nil
		}
		m.QuestionBar.Toggle()
		return m, nil
	case "enter":
		m.QuestionBar.Submit()
	case "esc", "ctrl+c":
		m.QuestionBar.Reject()
	default:
		return m, nil
	}
	m.state = m.preConfirmState
	m.resizeMessages()
	if m.state == stateProcessing {
		return m, spinnerTick()
	}
	return m, nil
}

// --- keys: question ---

// --- keys: dispatch ---

func (m *Model) handleKeyPress(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "ctrl+c":
		if m.Input.HasContent() {
			m.Input.Clear()
			m.Suggestions.Hide()
			m.commandMenuBack = nil
			m.resizeMessages()
			return nil
		}
		return tea.Quit

	case "up":
		if m.Suggestions.Visible() {
			m.Suggestions.Cycle(-1)
		} else if m.state == stateProcessing {
			m.Messages.Update(msg)
		} else if m.Input.CanCursorUp() {
			input, cmd := m.Input.Update(msg)
			m.Input = input
			m.resizeMessages()
			return cmd
		} else {
			m.Input.HistoryUp()
			m.resizeMessages()
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
			m.resizeMessages()
			return cmd
		} else {
			m.Input.HistoryDown()
			m.resizeMessages()
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
		// Accept a highlighted suggestion first, mirroring the idle path:
		// without this the popup would be visible but not selectable.
		if m.Suggestions.Visible() {
			parent := m.Input.Value()
			wasMenu := m.Suggestions.IsMenu()
			selected, ok := m.Suggestions.Accept()
			if !ok {
				// Current item: keep the picker open with its hint.
				return nil
			}
			if !selected.Submit {
				m.Input.SetValue(selected.Value + " ")
				m.Input.SetCursorEnd()
				// Mirror the idle path: a command with a nested menu
				// (e.g. /permission → manual/full) expands it instead of
				// merely completing into the input.
				if m.openCommandMenu(selected.Value) {
					if wasMenu {
						m.commandMenuBack = append(m.commandMenuBack, parent)
					}
					return nil
				}
				m.Suggestions.Hide()
				m.resizeMessages()
				return nil
			}
			value = selected.Value
		}
		if value != "" {
			// A fully typed command with a menu (e.g. /permission) opens it
			// instead of executing, mirroring the idle path. Record the typed
			// command as the menu's parent so esc walks back to it, exactly
			// like the suggestion-accept path above.
			if m.openCommandMenu(value) {
				if m.Suggestions.IsMenu() {
					m.commandMenuBack = append(m.commandMenuBack, value)
				} else {
					m.commandMenuBack = nil
				}
				return nil
			}
			m.Suggestions.Hide()
			m.resizeMessages()
			m.rememberInput(value)
			m.Input.Reset()
			m.Messages.GotoBottom()
			m.Input.SetFollow(true)
			if m.tryLocalCommand(value) {
				return nil
			}
			m.processingStart = time.Now()
			m.processingPhase = phaseSteer
			m.Messages.SetProcessingStatus(phaseSteer)
			if err := m.Runtime.SteerRun(context.Background(), "", controlruntime.Input{
				Source: controlruntime.SourceRef{Kind: "tui"},
				Text:   value,
			}); err != nil {
				m.Messages.AddMessage(message.ChatMessage{Role: "error", Content: err.Error()})
			}
		}
	case "esc":
		if m.Suggestions.Visible() {
			if m.Suggestions.IsMenu() && len(m.commandMenuBack) > 0 {
				last := len(m.commandMenuBack) - 1
				parent := m.commandMenuBack[last]
				m.commandMenuBack = m.commandMenuBack[:last]
				m.Input.SetValue(parent)
				m.Input.SetCursorEnd()
				m.openCommandMenu(parent)
				return nil
			}
			m.Suggestions.Hide()
			m.commandMenuBack = nil
			m.resizeMessages()
			return nil
		}
		if err := m.Runtime.CancelRun(context.Background(), ""); err != nil {
			m.Messages.AddMessage(message.ChatMessage{Role: "error", Content: err.Error()})
		} else {
			m.Messages.SetProcessingStatus("Aborted")
		}
	default:
		input, cmd := m.Input.Update(msg)
		m.Input = input
		m.refreshSuggestions()
		return cmd
	}
	return nil
}

func (m *Model) handleIdleKey(msg tea.KeyPressMsg) tea.Cmd {
	if (msg.String() == "d" || msg.String() == "D") && m.requestSessionDelete() {
		return nil
	}
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
			if m.Suggestions.IsMenu() && len(m.commandMenuBack) > 0 {
				last := len(m.commandMenuBack) - 1
				parent := m.commandMenuBack[last]
				m.commandMenuBack = m.commandMenuBack[:last]
				m.Input.SetValue(parent)
				m.Input.SetCursorEnd()
				m.openCommandMenu(parent)
				return nil
			}
			m.Suggestions.Hide()
			m.commandMenuBack = nil
			m.resizeMessages()
			return nil
		}
	case "enter":
		if m.Suggestions.Visible() {
			parent := m.Input.Value()
			wasMenu := m.Suggestions.IsMenu()
			if selected, ok := m.Suggestions.Accept(); ok {
				m.Input.SetValue(selected.Value + " ")
				m.Input.SetCursorEnd()
				if selected.Submit {
					m.commandMenuBack = nil
					m.resizeMessages()
					m.rememberInput(selected.Value)
					m.Input.Reset()
					return m.startChat(selected.Value)
				}
				if m.openCommandMenu(selected.Value) {
					if wasMenu {
						m.commandMenuBack = append(m.commandMenuBack, parent)
					}
					return nil
				}
			}
			m.resizeMessages()
			return nil
		}
		value := m.Input.Value()
		if value == "" {
			m.Messages.GotoBottom()
			m.Input.SetFollow(true)
			return nil
		}
		if m.openCommandMenu(value) {
			m.commandMenuBack = nil
			return nil
		}
		m.Suggestions.Hide()
		m.commandMenuBack = nil
		m.resizeMessages()
		m.rememberInput(value)
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
	m.commandMenuBack = nil
	value := m.Input.Value()
	prefix := ""
	if strings.HasPrefix(value, "/") {
		prefix = "/"
	} else if strings.HasPrefix(value, "$") {
		prefix = "$"
	}
	if prefix == "" {
		m.Suggestions.Hide()
		m.resizeMessages()
		return
	}
	menu, ok := m.Runtime.CommandMenu(context.Background(), prefix)
	if !ok {
		m.Suggestions.Hide()
		m.resizeMessages()
		return
	}
	m.Suggestions.Refresh(value, menu.Items)
	m.resizeMessages()
}

func (m *Model) openCommandMenu(input string) bool {
	input = strings.TrimSpace(input)
	menu, ok := m.Runtime.CommandMenu(context.Background(), input)
	if !ok {
		return false
	}
	m.Suggestions.OpenMenu(menu.Title, menu.Empty, menu.Items)
	if input == "/sessions" {
		if _, ok := m.Runtime.(sessionDeleter); ok {
			for _, item := range menu.Items {
				if item.Key != "" {
					m.Suggestions.SetActionHint("d delete")
					break
				}
			}
		}
	}
	m.resizeMessages()
	return true
}

func (m *Model) requestSessionDelete() bool {
	if strings.TrimSpace(m.Input.Value()) != "/sessions" || !m.Suggestions.IsMenu() {
		return false
	}
	deleter, ok := m.Runtime.(sessionDeleter)
	if !ok {
		return false
	}
	selected, ok := m.Suggestions.Selected()
	if !ok {
		return false
	}
	if selected.Key == "" {
		return false
	}
	sessionID := selected.Key
	m.Suggestions.Hide()
	m.preConfirmState = m.state
	m.state = stateConfirming
	m.ConfirmBar.SetDestructive(
		"删除会话",
		fmt.Sprintf("确定删除会话 %s？会话记录和检查点将被永久删除。", sessionID),
		"删除",
		func(confirmed bool) {
			if !confirmed {
				m.openCommandMenu("/sessions")
				return
			}
			if err := deleter.DeleteSession(sessionID); err != nil {
				content := fmt.Sprintf("删除会话 %s 失败：%v", sessionID, err)
				m.Messages.AddMessage(message.ChatMessage{Role: "error", Content: content})
				m.Messages.GotoBottom()
			}
			m.openCommandMenu("/sessions")
		},
	)
	m.resizeMessages()
	return true
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
	if m.state == stateQuestioning {
		m.Messages.SetSpinnerView("")
		return nil
	}

	if m.state == stateProcessing {
		elapsed := time.Since(m.processingStart)
		statusText := fmt.Sprintf("%s (%.1fs)", m.processingPhase, elapsed.Seconds())
		spinnerView := m.Spinner.View()
		m.Messages.UpdateProcessing(func(p *processing.ProcessingItem) {
			p.SetSpinnerView(spinnerView)
			p.SetStatusText(statusText)
		})
		if m.processingPhase != phaseCompacting {
			st := m.metrics
			m.Messages.UpdateProcessing(func(p *processing.ProcessingItem) {
				p.SetTokens(st.TurnPrompt, st.TurnCompletion)
				p.SetCompactCount(st.CompactCount)
			})
		}
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

func (m *Model) handleRuntimeEvent(ev controlruntime.Event) tea.Cmd {
	switch ev.Type {
	case controlruntime.EventCompaction:
		if p, ok := ev.Payload.(controlruntime.CompactionPayload); ok {
			m.Messages.UpdateCompaction(p)
		}
	case controlruntime.EventInputAccepted:
		if p, ok := ev.Payload.(controlruntime.MessagePayload); ok {
			title := ""
			if p.Source.Kind != "" && p.Source.Kind != "tui" {
				title = "You · " + p.Source.Kind
				if p.Sender.Display != "" {
					title += " " + p.Sender.Display
				} else if p.Sender.Username != "" {
					title += " @" + p.Sender.Username
				}
			}
			m.Messages.AddMessage(message.ChatMessage{Role: "user", Title: title, Content: p.Content})
			m.Messages.ClearProcessing()
			m.Messages.SetBlocks(nil)
			m.Messages.GotoBottom()
			m.Input.SetFollow(true)
		}
	case controlruntime.EventSystemMessage:
		if p, ok := ev.Payload.(controlruntime.MessagePayload); ok && strings.TrimSpace(p.Content) != "" && !m.Messages.IsDuplicateCompactionResponse(p.Content) {
			m.Messages.AddMessage(message.ChatMessage{
				Role:            "system",
				Content:         p.Content,
				RenderedContent: p.Content,
			})
		}
		m.refreshRuntimeStatus()
	case controlruntime.EventRunStarted:
		if m.state != stateProcessing {
			m.transitionTo(stateProcessing)
		}
		m.metrics = controlruntime.MetricsSnapshot{}
		return spinnerTick()
	case controlruntime.EventAssistantDelta:
		if p, ok := ev.Payload.(controlruntime.DeltaPayload); ok {
			m.Messages.ProcessStreamText(p.Delta)
		}
	case controlruntime.EventReasoningDelta:
		if p, ok := ev.Payload.(controlruntime.DeltaPayload); ok {
			m.Messages.ProcessThinkingText(p.Delta)
		}
	case controlruntime.EventPhaseChanged:
		if p, ok := ev.Payload.(controlruntime.PhasePayload); ok {
			m.setPhase(p.Phase)
		}
	case controlruntime.EventMetricsUpdated:
		if metrics, ok := ev.Payload.(controlruntime.MetricsSnapshot); ok {
			m.metrics = metrics
		}
	case controlruntime.EventTodosUpdated:
		if items, ok := ev.Payload.([]controlruntime.TodoItem); ok {
			m.Messages.SetTodos(todoItemsText(items))
		}
	case controlruntime.EventToolStarted:
		m.applyRuntimeToolEvent(ev)
	case controlruntime.EventToolBlocked:
		m.applyRuntimeToolEvent(ev)
	case controlruntime.EventToolPreview:
		m.applyRuntimeToolEvent(ev)
	case controlruntime.EventToolCompleted:
		m.applyRuntimeToolEvent(ev)
	case controlruntime.EventSubAgentStarted:
		if p, ok := ev.Payload.(controlruntime.SubAgentPayload); ok {
			profile := p.Profile
			if profile == "" {
				profile = p.Type
			}
			m.Messages.AddSubAgent(p.ID, profile, p.Color)
		}
	case controlruntime.EventSubAgentEnded:
		if p, ok := ev.Payload.(controlruntime.SubAgentPayload); ok {
			m.Messages.RemoveSubAgent(p.ID)
		}
	case controlruntime.EventApprovalRequested:
		if p, ok := ev.Payload.(controlruntime.ApprovalView); ok {
			req := p.ToConfirmRequest()
			m.ConfirmBar.SetRequest(&req, func(ok, remember bool) {
				_ = m.Runtime.DecideApproval(context.Background(), p.ID, controlruntime.ApprovalDecision{
					Allowed:  ok,
					Remember: ok && remember,
				})
			})
			m.preConfirmState = m.state
			m.state = stateConfirming
			m.resizeMessages()
		}
	case controlruntime.EventApprovalResolved:
		if m.state == stateConfirming {
			m.state = m.preConfirmState
			m.ConfirmBar.Clear()
			m.resizeMessages()
		}
	case controlruntime.EventQuestionRequested:
		if p, ok := ev.Payload.(controlruntime.QuestionView); ok {
			req := p.ToQuestionRequest()
			m.QuestionBar.SetRequest(&req, func(reply controlruntime.QuestionReply) {
				_ = m.Runtime.AnswerQuestion(context.Background(), p.ID, reply)
			})
			m.preConfirmState = m.state
			m.state = stateQuestioning
			m.resizeMessages()
		}
	case controlruntime.EventQuestionResolved:
		if m.state == stateQuestioning {
			m.state = m.preConfirmState
			m.QuestionBar.Clear()
			m.resizeMessages()
		}
	case controlruntime.EventRunDone:
		payload, _ := ev.Payload.(controlruntime.RunResult)
		return m.handleDone(doneMsg{
			content: payload.Output,
			metrics: m.metrics,
		})
	case controlruntime.EventRunFailed:
		payload, _ := ev.Payload.(controlruntime.RunResult)
		err := errors.New("run failed")
		if payload.Error != "" {
			err = errors.New(payload.Error)
		}
		return m.handleDone(doneMsg{
			content: payload.Output,
			metrics: m.metrics,
			err:     err,
		})
	case controlruntime.EventRunCancelled:
		return m.handleDone(doneMsg{metrics: m.metrics, err: errors.New("cancelled")})
	case controlruntime.EventSessionChanged:
		m.refreshRuntimeStatus()
		m.loadSessionMessages()
		return loadWorkspaceChanges(m.Runtime)
	case controlruntime.EventConnectorStatus:
		if p, ok := ev.Payload.(controlruntime.ConnectorStatusPayload); ok && p.Message != "" {
			m.Messages.AddMessage(message.ChatMessage{
				Role:            "system",
				Content:         p.Message,
				RenderedContent: p.Message,
			})
		}
	}
	return nil
}

func (m *Model) applyRuntimeToolEvent(ev controlruntime.Event) {
	p, ok := ev.Payload.(controlruntime.ToolPayload)
	if !ok || interactiveTool(p.ToolName) {
		return
	}
	if p.SubAgentID != "" {
		switch ev.Type {
		case controlruntime.EventToolStarted:
			m.Messages.ProcessToolBlock(block.ContentBlock{
				Type: block.BlockTool, ToolName: p.ToolName,
				ToolArgs:   interaction.ToolBrief(p.ToolName, p.Args),
				ToolAction: interaction.ToolAction(p.ToolName, p.Args),
				SubID:      p.SubAgentID, SubColor: p.SubAgentColor,
			})
		case controlruntime.EventToolCompleted:
			m.Messages.AddSubToolOutput(p.SubAgentID, p.ToolName, p.Output, p.IsError)
		}
		return
	}
	switch ev.Type {
	case controlruntime.EventToolStarted:
		m.Messages.ProcessToolBlock(block.ContentBlock{
			Type: block.BlockTool, ToolName: p.ToolName,
			ToolArgs:   interaction.ToolBrief(p.ToolName, p.Args),
			ToolAction: interaction.ToolAction(p.ToolName, p.Args),
			Content:    p.Preview,
		})
	case controlruntime.EventToolBlocked:
		m.Messages.ProcessToolBlock(block.ContentBlock{
			Type: block.BlockTool, ToolName: p.ToolName,
			ToolArgs:   interaction.ToolBrief(p.ToolName, p.Args),
			ToolAction: interaction.ToolAction(p.ToolName, p.Args),
			Content:    p.Output, Done: true, IsError: true,
		})
	case controlruntime.EventToolPreview:
		m.Messages.UpdateToolPreview(p.ToolName, p.Preview)
	case controlruntime.EventToolCompleted:
		m.Messages.AddToolOutput(p.ToolName, p.Output, p.IsError)
	}
}
