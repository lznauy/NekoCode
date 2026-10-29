// handlers.go — 按键处理 + 完成处理 + spinner tick + 调试日志。
package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"nekocode/interaction/tui/components"
	"nekocode/interaction/tui/components/block"
	"nekocode/interaction/tui/components/message"
	"nekocode/interaction/tui/components/processing"
	controlruntime "nekocode/runtime"
	"nekocode/util/attachment"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
)

const (
	contentMarginV = 2
)

// --- done ---

func (m *Model) handleDone(msg doneMsg) tea.Cmd {
	if m.sessionLoading {
		m.sessionLoadDone = &msg
		return nil
	}
	m.Messages.SettleCompactions()
	for id, status := range m.taskStates {
		if status == "running" || status == "queued" || status == "waiting_approval" || status == "waiting_question" {
			m.taskStates[id] = "interrupted"
		}
	}
	finalBlocks := block.FilterFinalBlocks(m.Messages.ProcessingBlocks())
	for _, b := range m.Messages.ProcessingBlocks() {
		if b.ToolName == "子任务失败" || b.ToolName == "子任务未完成" {
			finalBlocks = append(finalBlocks, b)
		}
	}
	if summary := m.taskSummary(); summary != "" {
		finalBlocks = append(finalBlocks, block.ContentBlock{Type: block.BlockTool, ToolName: "子任务", ToolArgs: strings.TrimPrefix(summary, "子任务 · "), Content: "Ctrl+G 查看任务记录", Done: true})
	}

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
			return m.refreshCompletedHistory()
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
	if msg.err != nil {
		// Keep the live error visible; it may not exist in conversation history.
		return m.continuePendingChat(loadWorkspaceChanges(m.Runtime))
	}
	return m.refreshCompletedHistory()
}

func (m *Model) refreshCompletedHistory() tea.Cmd {
	refresh := m.sessionHistoryPaging && m.sessionHistoryDirty
	m.sessionHistoryDirty = false
	if refresh {
		return tea.Batch(loadWorkspaceChanges(m.Runtime), m.loadSessionWindow(0, 0))
	}
	return m.continuePendingChat(loadWorkspaceChanges(m.Runtime))
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
	case "ctrl+g":
		return m.toggleTasks()
	case "ctrl+v":
		return readClipboardImage()
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
			_, cmd := m.Messages.Update(msg)
			return cmd
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
			_, cmd := m.Messages.Update(msg)
			return cmd
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
		_, cmd := m.Messages.Update(msg)
		m.Input.SetFollow(m.Messages.Follow)
		return cmd
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
		preserveImageDraft := false
		acceptedSubmit := false
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
			acceptedSubmit = true
			preserveImageDraft = len(m.Input.ImageAttachments()) > 0
		}
		if value != "" {
			// A fully typed command with a menu (e.g. /permission) opens it
			// instead of executing, mirroring the idle path. Record the typed
			// command as the menu's parent so esc walks back to it, exactly
			// like the suggestion-accept path above.
			if !acceptedSubmit && m.openCommandMenu(value) {
				if m.Suggestions.IsMenu() {
					m.commandMenuBack = append(m.commandMenuBack, value)
				} else {
					m.commandMenuBack = nil
				}
				return nil
			}
			m.Suggestions.Hide()
			m.resizeMessages()
			images := inputImageAttachments(m.Input.ImageAttachments())
			if len(images) == 0 {
				m.rememberInput(value)
			}
			m.Messages.GotoBottom()
			m.Input.SetFollow(true)
			if handled, cmd := m.tryLocalCommand(value); handled {
				m.finishLocalCommandInput()
				return cmd
			}
			m.processingStart = time.Now()
			previousPhase := m.processingPhase
			m.processingPhase = phaseSteer
			m.Messages.SetProcessingStatus(phaseSteer)
			runImages := images
			if preserveImageDraft {
				runImages = nil
			}
			if err := m.Runtime.SteerRun(context.Background(), "", controlruntime.Input{
				Source: controlruntime.SourceRef{Kind: "tui"},
				Text:   value,
				Images: runImages,
			}); err != nil {
				m.processingPhase = previousPhase
				m.Messages.SetProcessingStatus(previousPhase)
				m.Messages.AddMessage(message.ChatMessage{Role: "error", Content: err.Error()})
			} else if preserveImageDraft {
				m.notifyImageDraftPreserved("命令已提交；图片未发送，草稿已保留。")
			} else {
				m.Input.Reset()
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
				if selected.Submit {
					images := inputImageAttachments(m.Input.ImageAttachments())
					m.commandMenuBack = nil
					m.resizeMessages()
					m.rememberInput(selected.Value)
					if len(images) > 0 {
						return m.startChat(selected.Value, nil)
					}
					m.Input.SetValue(selected.Value + " ")
					m.Input.SetCursorEnd()
					return m.startChat(selected.Value, nil)
				}
				m.Input.SetValue(selected.Value + " ")
				m.Input.SetCursorEnd()
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
		images := inputImageAttachments(m.Input.ImageAttachments())
		if len(images) == 0 {
			m.rememberInput(value)
		}
		return m.startChat(value, images)
	default:
		input, cmd := m.Input.Update(msg)
		m.Input = input
		m.refreshSuggestions()
		return cmd
	}
	return nil
}

type attachmentSessionRuntime interface {
	CurrentSessionID() string
	NewSession() (controlruntime.SessionMeta, error)
	ImageAttachmentsEnabled() bool
}

func (m *Model) handleClipboardImage(msg clipboardImageMsg) tea.Cmd {
	if msg.err != nil {
		m.Messages.AddMessage(message.ChatMessage{Role: "error", Content: "读取剪贴板图片失败：" + msg.err.Error()})
		return nil
	}
	if len(msg.data) == 0 {
		return nil
	}
	if m.Input.BrowsingHistory() {
		m.Messages.AddMessage(message.ChatMessage{Role: "error", Content: "浏览历史记录时不能粘贴图片，请先按 ↓ 返回当前草稿"})
		return nil
	}
	sessionRuntime, ok := m.Runtime.(attachmentSessionRuntime)
	if !ok {
		m.Messages.AddMessage(message.ChatMessage{Role: "error", Content: "当前运行时不支持图片附件"})
		return nil
	}
	if !sessionRuntime.ImageAttachmentsEnabled() {
		m.Messages.AddMessage(message.ChatMessage{Role: "error", Content: "当前模型不支持图片且未配置图片理解模型；可在模型配置中开启 vision，或配置 image_understand_models"})
		return nil
	}
	sessionID := sessionRuntime.CurrentSessionID()
	if sessionID == "" {
		meta, err := sessionRuntime.NewSession()
		if err != nil {
			m.Messages.AddMessage(message.ChatMessage{Role: "error", Content: "创建图片附件会话失败：" + err.Error()})
			return nil
		}
		sessionID = meta.ID
	}
	path, err := attachment.SaveImage(sessionID, msg.data)
	if err != nil {
		m.Messages.AddMessage(message.ChatMessage{Role: "error", Content: "保存剪贴板图片失败：" + err.Error()})
		return nil
	}
	if !m.Input.AddImage(path) {
		_ = attachment.DeleteImage(path)
		m.Messages.AddMessage(message.ChatMessage{Role: "error", Content: fmt.Sprintf("单条消息最多附加 %d 张图片", controlruntime.MaxImageAttachments)})
		return nil
	}
	m.refreshSuggestions()
	m.resizeMessages()
	return nil
}

func inputImageAttachments(images []components.InputImageAttachment) []controlruntime.ImageAttachment {
	out := make([]controlruntime.ImageAttachment, 0, len(images))
	for _, image := range images {
		out = append(out, controlruntime.ImageAttachment{Label: image.Label, Path: image.Path})
	}
	return out
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
	if prefix == "/" {
		if canObserveTasks(m.Runtime) {
			menu.Items = append(menu.Items, controlruntime.CommandMenuItem{Value: "/tasks", Label: "/tasks", Description: "查看子任务执行记录 · Ctrl+G", Submit: true})
			ok = true
		}
	}
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
	if input == "/tasks" {
		return false
	}
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
				if sessionDeleteCommitted(err) {
					content := fmt.Sprintf("会话 %s 已删除，但关联数据清理未完成：%v", sessionID, err)
					m.Messages.AddMessage(message.ChatMessage{Role: "system", Content: content, RenderedContent: content})
				} else {
					content := fmt.Sprintf("删除会话 %s 失败：%v", sessionID, err)
					m.Messages.AddMessage(message.ChatMessage{Role: "error", Content: content})
				}
				m.Messages.GotoBottom()
			}
			m.openCommandMenu("/sessions")
		},
	)
	m.resizeMessages()
	return true
}

func sessionDeleteCommitted(err error) bool {
	type committedMutation interface{ MutationCommitted() bool }
	var committed committedMutation
	return errors.As(err, &committed) && committed.MutationCommitted()
}

func (m *Model) cycleSuggestion(delta int) {
	m.Suggestions.Cycle(delta)
}

// --- spinner ---

func (m *Model) handleSpinnerTick(msg spinner.TickMsg) tea.Cmd {
	m.Spinner, _ = m.Spinner.Update(msg)
	if m.tasks != nil && m.tasks.open && m.tasks.detail != nil && m.tasks.detail.processing != nil {
		m.tasks.detail.processing.SetSpinnerView(m.Spinner.View())
	}

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
			if m.processingPhase != phaseCompacting {
				st := m.metrics
				p.SetTokens(st.TurnPrompt, st.TurnCompletion)
				p.SetCompactCount(st.CompactCount)
			}
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

func (m *Model) handleRuntimeEvent(ev controlruntime.Event) (cmd tea.Cmd) {
	defer func() {
		// Lifecycle changes should refresh an open overlay immediately rather
		// than depending on its polling timer. In-flight reads retain dirty.
		if s := m.tasks; s != nil && s.open && s.listDirty && !s.listLoading {
			cmd = tea.Batch(cmd, m.loadTasks())
		}
	}()
	m.observeTaskLifecycle(ev)
	switch ev.Type {
	case controlruntime.EventCompaction:
		if p, ok := ev.Payload.(controlruntime.CompactionPayload); ok {
			m.Messages.UpdateCompaction(p)
		}
	case controlruntime.EventInputAccepted:
		m.sessionHistoryDirty = true
		if p, ok := ev.Payload.(controlruntime.MessagePayload); ok {
			if p.Source.Kind == "tui" {
				if len(p.Images) > 0 || len(m.Input.ImageAttachments()) == 0 {
					m.Input.Reset()
				} else {
					m.notifyImageDraftPreserved("命令已执行；图片未发送，草稿已保留。")
				}
			}
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
		var clipboard tea.Cmd
		if p, ok := ev.Payload.(controlruntime.MessagePayload); ok && strings.TrimSpace(p.Content) != "" && !m.Messages.IsDuplicateCompactionResponse(p.Content) {
			notice := message.ChatMessage{
				Role:            "system",
				Content:         p.Content,
				RenderedContent: p.Content,
			}
			m.Messages.AddMessage(notice)
			if m.sessionLoading {
				m.sessionLoadNotices = append(m.sessionLoadNotices, notice)
			}
			if url := message.LastBareURL(p.Content); url != "" {
				clipboard = tea.SetClipboard(url)
			}
		}
		m.refreshRuntimeStatus()
		return clipboard
	case controlruntime.EventRunStarted:
		m.taskStates = nil
		m.taskSummarySyncedAt = time.Time{}
		reloadHistory := m.sessionLoading && m.sessionLoadDone != nil
		if reloadHistory {
			// A deferred terminal event belongs to the previous run. Never
			// apply it to the new run when an old history worker finishes.
			if err := m.sessionLoadDone.err; err != nil {
				notice := message.ChatMessage{Role: "error", Content: fmt.Sprintf("Error: %v", err)}
				m.Messages.AddMessage(notice)
				m.sessionLoadNotices = append(m.sessionLoadNotices, notice)
			}
			m.sessionLoadDone = nil
			m.transitionTo(stateReady)
			m.sessionLoadPhase = PhaseWaiting
		}
		m.sessionHistoryDirty = false
		if m.sessionLoading {
			m.sessionLoadWaitForRun = true
		}
		if m.state != stateProcessing {
			m.transitionTo(stateProcessing)
		}
		m.Messages.UpdateProcessing(func(item *processing.ProcessingItem) { item.SetSubagentSummary("") })
		m.metrics = controlruntime.MetricsSnapshot{}
		if reloadHistory {
			return tea.Batch(spinnerTick(), m.loadSessionWindow(0, 0))
		}
		return spinnerTick()
	case controlruntime.EventAssistantDelta:
		if p, ok := ev.Payload.(controlruntime.DeltaPayload); ok {
			m.Messages.ProcessStreamText(p.Delta)
		}
	case controlruntime.EventAssistantReset:
		m.Messages.DiscardStreamText()
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
		if canObserveTasks(m.Runtime) {
			break
		}
		if p, ok := ev.Payload.(controlruntime.SubAgentPayload); ok {
			if p.Status == "queued" {
				break
			}
			profile := p.Profile
			if profile == "" {
				profile = p.Type
			}
			m.Messages.AddSubAgent(p.ID, profile, p.Color)
		}
	case controlruntime.EventSubAgentEnded:
		if canObserveTasks(m.Runtime) {
			break
		}
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
		m.taskStates = nil
		m.taskSummarySyncedAt = time.Time{}
		if m.tasks != nil {
			m.closeTasks()
			m.tasks.detail = nil
			m.tasks.savedDetail = nil
			m.tasks.sessionID = ""
		}
		return m.loadSessionMessages()
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
	if p.SubAgentID == "" && p.ToolName == "task" && canObserveTasks(m.Runtime) {
		// Keep actionable failures visible, without expanding every successful
		// delegation into a separate activity/tool-output box.
		if ev.Type == controlruntime.EventToolBlocked || (ev.Type == controlruntime.EventToolCompleted && p.IsError) {
			m.Messages.ProcessToolBlock(block.ContentBlock{Type: block.BlockTool, ToolName: "子任务失败", CallID: p.CallID, Content: p.Output, Done: true, IsError: true})
		} else if ev.Type == controlruntime.EventToolCompleted && strings.HasPrefix(p.Output, "[Subagent returned a partial result]") {
			m.Messages.ProcessToolBlock(block.ContentBlock{Type: block.BlockTool, ToolName: "子任务未完成", CallID: p.CallID, Content: p.Output, Done: true, IsError: true})
		}
		return
	}
	if p.SubAgentID != "" {
		switch ev.Type {
		case controlruntime.EventToolStarted:
			tool := block.NewTool(p.ToolName, p.CallID, p.Args)
			tool.SubID, tool.SubColor = p.SubAgentID, p.SubAgentColor
			m.Messages.ProcessToolBlock(tool)
		case controlruntime.EventToolPreview:
			m.Messages.UpdateToolPreviewForCall(p.CallID, p.SubAgentID, p.ToolName, p.Preview, p.Decision)
		case controlruntime.EventToolCompleted:
			m.Messages.AddSubToolOutput(p.SubAgentID, p.ToolName, p.Output, p.IsError, p.CallID)
		}
		return
	}
	switch ev.Type {
	case controlruntime.EventToolStarted:
		tool := block.NewTool(p.ToolName, p.CallID, p.Args)
		tool.Content = p.Preview
		m.Messages.ProcessToolBlock(tool)
	case controlruntime.EventToolBlocked:
		tool := block.NewTool(p.ToolName, p.CallID, p.Args)
		tool.Content, tool.Done, tool.IsError = p.Output, true, true
		m.Messages.ProcessToolBlock(tool)
	case controlruntime.EventToolPreview:
		m.Messages.UpdateToolPreviewForCall(p.CallID, p.SubAgentID, p.ToolName, p.Preview, p.Decision)
	case controlruntime.EventToolCompleted:
		m.Messages.AddToolOutput(p.ToolName, p.Output, p.IsError, p.CallID)
	}
}
