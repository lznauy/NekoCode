// agent.go — 启动 agent 对话流程：startChat、startAgent。
package tui

import (
	"context"
	"fmt"
	"nekocode/interaction/tui/components"
	"nekocode/interaction/tui/styles"
	"strings"
	"time"

	"nekocode/interaction"
	"nekocode/interaction/tui/components/block"
	"nekocode/interaction/tui/components/message"
	controlruntime "nekocode/runtime"

	tea "charm.land/bubbletea/v2"
)

func (m *Model) startChat(value string, images []controlruntime.ImageAttachment) tea.Cmd {
	if handled, cmd := m.tryLocalCommand(value); handled {
		m.finishLocalCommandInput()
		return cmd
	}
	if m.sessionHistoryPaging && m.sessionHistoryPage > 0 {
		m.sessionPendingChat = &pendingSessionChat{text: value, images: images}
		return m.loadSessionWindow(0, 0)
	}
	m.transitionTo(stateProcessing)
	m.Messages.SetSpinnerView(m.Spinner.View())
	status := PhaseWaiting
	if isCompactCommand(value) {
		status = phaseCompacting
	} else if fields := strings.Fields(value); len(fields) > 1 && fields[0] == "/sessions" {
		status = phaseSessionLoading
	}
	m.setPhase(status)
	m.Messages.SetProcessingStatus(status)
	if _, err := m.Runtime.StartRun(context.Background(), controlruntime.Input{
		Source: controlruntime.SourceRef{Kind: "tui"},
		Text:   value,
		Images: images,
	}); err != nil {
		m.transitionTo(stateReady)
		m.Messages.AddMessage(message.ChatMessage{
			Role:    "error",
			Content: err.Error(),
		})
		return nil
	}
	// Text-only input can be cleared immediately. Image ownership transfers
	// only after EventInputAccepted confirms that the agent received it.
	if len(m.Input.ImageAttachments()) == 0 {
		m.Input.Reset()
	}
	return spinnerTick()
}

func (m *Model) finishLocalCommandInput() {
	if len(m.Input.ImageAttachments()) == 0 {
		m.Input.Reset()
		return
	}
	m.notifyImageDraftPreserved("图片未发送，草稿已保留。")
}

func (m *Model) notifyImageDraftPreserved(content string) {
	m.Messages.AddMessage(message.ChatMessage{Role: "system", Content: content, RenderedContent: content})
	m.Messages.GotoBottom()
}

func isCompactCommand(value string) bool {
	// The command ignores arguments, so any argumented form ("/compact now")
	// still routes to ForceCompact and still deserves the compacting phase.
	fields := strings.Fields(value)
	return len(fields) > 0 && fields[0] == "/compact"
}

// tryLocalCommand executes during-task-safe commands immediately, without a
// run lifecycle — the fork that keeps status queries and local toggles off
// the prompt FIFO. It reports whether the input was fully handled: either
// executed, or rejected because the command needs an idle runtime while a
// task is in progress. The returned command runs after the message lands
// (e.g. copying an emitted authorization link to the clipboard).
func (m *Model) tryLocalCommand(value string) (bool, tea.Cmd) {
	if handled, cmd := m.taskCommand(value); handled {
		return true, cmd
	}
	out, status := m.Runtime.ExecuteLocalCommand(context.Background(), value)
	addSystem := func(content string) {
		m.Messages.AddMessage(message.ChatMessage{Role: "system", Content: content, RenderedContent: content})
		m.Messages.GotoBottom()
	}
	switch status {
	case controlruntime.LocalCommandExecuted:
		if strings.TrimSpace(out) != "" {
			addSystem(out)
		}
		// Local commands emit no runtime events, so status fields they can
		// change (e.g. the permission mode) must be refreshed here.
		m.Input.SetPermissionMode(m.Runtime.PermissionMode())
		// The TUI captures the mouse, so terminal hyperlinks are not
		// clickable here: copy an emitted authorization link to the
		// clipboard instead — paste it into the browser to authorize.
		if url := message.LastBareURL(out); url != "" {
			return true, tea.SetClipboard(url)
		}
		return true, nil
	case controlruntime.LocalCommandRequiresIdle:
		if m.state == stateProcessing {
			addSystem("命令 " + value + " 需在任务结束后执行")
			return true, nil
		}
	}
	return false, nil
}

func interactiveTool(toolName string) bool {
	return toolName == "todo_write" || toolName == "question"
}

type pendingSessionChat struct {
	text   string
	images []controlruntime.ImageAttachment
}

type sessionLoadedMsg struct {
	window        *controlruntime.SessionHistoryWindow
	pageIndex     int
	requestBefore int
	generation    uint64
	messages      *components.Messages
	status        runtimeViewStatus
	width, height int
	err           error
}

// Projection and initial layout may read large tool histories. Neither may
// run inside Update: doing so freezes the spinner and keyboard handling.
func (m *Model) loadSessionMessages() tea.Cmd {
	m.sessionHistoryCursors = []int{0}
	m.sessionHistoryPage = 0
	m.sessionHistoryPaging = false
	return m.loadSessionWindow(0, 0)
}

func (m *Model) loadSessionWindow(before, pageIndex int) tea.Cmd {
	m.sessionLoadGeneration++
	m.sessionLoadStarted = time.Now()
	m.sessionLoadLabel = phaseSessionLoading
	if pageIndex > m.sessionHistoryPage {
		m.sessionLoadLabel = "正在加载更早消息…"
	} else if pageIndex < m.sessionHistoryPage {
		m.sessionLoadLabel = "正在加载较新消息…"
	}
	generation := m.sessionLoadGeneration
	if !m.sessionLoading {
		m.sessionLoadNotices = nil
		m.sessionLoadWaitForRun = m.state == stateProcessing ||
			((m.state == stateConfirming || m.state == stateQuestioning) && m.preConfirmState == stateProcessing)
		m.sessionLoadPhase = m.processingPhase
	}
	m.sessionLoading = true
	if m.state == stateReady {
		m.transitionTo(stateProcessing)
	}
	m.setPhase(phaseSessionLoading)
	m.Messages.SetProcessingStatus(phaseSessionLoading)
	rt, width, height := m.Runtime, m.Messages.Width(), m.Messages.Height()
	return func() (result tea.Msg) {
		defer func() {
			if failure := recover(); failure != nil {
				result = sessionLoadedMsg{generation: generation, err: fmt.Errorf("加载会话失败：%v", failure)}
			}
		}()
		sty := styles.DefaultStyles()
		var prepared *components.Messages
		var window *controlruntime.SessionHistoryWindow
		if paged, ok := rt.(interface {
			SessionHistoryWindow(int, int) (controlruntime.SessionHistoryWindow, error)
		}); ok {
			limit := 100
			for {
				cursor := before
				var page controlruntime.SessionHistoryWindow
				for {
					var err error
					page, err = paged.SessionHistoryWindow(cursor, limit)
					if err != nil {
						return sessionLoadedMsg{generation: generation, err: err}
					}
					if len(page.Messages) > 0 || !page.HasMore {
						break
					}
					if page.Before < 1 || (cursor > 0 && page.Before >= cursor) {
						return sessionLoadedMsg{generation: generation, err: fmt.Errorf("history cursor did not advance")}
					}
					cursor = page.Before
				}
				history := restoredChatMessages(page.Messages)
				prepared = components.NewMessages(width, height, &sty)
				prepared.ReplaceHistoryWindow(history, page.Before, page.End, page.Total, page.HasMore, pageIndex > 0)
				if prepared.HistoryWindowBytes(history) <= 8<<20 || limit == 1 {
					window = &page
					break
				}
				limit = max(1, limit/2)
			}
		} else {
			history := restoredChatMessages(rt.SessionMessages())
			prepared = components.NewMessages(width, height, &sty)
			prepared.ReplaceMessages(history)
		}
		return sessionLoadedMsg{generation: generation, messages: prepared, window: window, pageIndex: pageIndex, requestBefore: before, status: readRuntimeViewStatus(rt), width: width, height: height}
	}
}

func (m *Model) applySessionLoaded(msg sessionLoadedMsg) tea.Cmd {
	if msg.generation != m.sessionLoadGeneration {
		return nil
	}
	if msg.err == nil && (msg.width != m.Messages.Width() || msg.height != m.Messages.Height()) {
		// Build at the new size in the background too; resizing must not move
		// the expensive cold layout back onto the event loop.
		return m.loadSessionWindow(msg.requestBefore, msg.pageIndex)
	}
	m.sessionLoading = false
	if msg.err != nil {
		m.sessionPendingChat = nil
		m.Messages.AddMessage(message.ChatMessage{Role: "error", Content: msg.err.Error()})
	} else {
		movingNewer := msg.pageIndex < m.sessionHistoryPage
		if m.sessionLoadWaitForRun {
			msg.messages.MoveProcessingFrom(m.Messages)
		}
		m.Messages = msg.messages
		for _, notice := range m.sessionLoadNotices {
			m.Messages.AddMessage(notice)
		}
		if msg.window != nil {
			m.sessionHistoryPaging = true
			m.sessionHistoryPage = msg.pageIndex
			m.sessionHistoryBefore = msg.window.Before
			if msg.pageIndex == 0 {
				m.sessionHistoryCursors = []int{0}
			}
			m.Messages.SetHistoryPageHandler(m.requestHistoryPage)
			if movingNewer {
				m.Messages.ScrollToTop()
				m.Messages.SetFollow(false)
			}
		}
		m.applyRuntimeViewStatus(msg.status)
	}
	notices := m.sessionLoadNotices
	m.sessionLoadNotices = nil
	if m.sessionLoadWaitForRun && m.processingPhase == phaseSessionLoading {
		phase := m.sessionLoadPhase
		if phase == "" || phase == PhaseReady || phase == phaseSessionLoading {
			phase = PhaseWaiting
		}
		m.setPhase(phase)
		m.Messages.SetProcessingStatus(phase)
	}
	done := m.sessionLoadDone
	m.sessionLoadDone = nil
	var completion tea.Cmd
	if done != nil {
		completion = m.handleDone(*done)
		if m.sessionLoading {
			// Completion can immediately refresh a stale window again. Carry
			// newly delivered notices through that replacement as well.
			m.sessionLoadNotices = append(m.sessionLoadNotices, notices...)
			return completion
		}
	} else if !m.sessionLoadWaitForRun && m.state == stateProcessing {
		m.transitionTo(stateReady)
	}
	if completion != nil {
		return m.continuePendingChat(completion)
	}
	return m.continuePendingChat(loadWorkspaceChanges(m.Runtime))
}

func (m *Model) continuePendingChat(cmd tea.Cmd) tea.Cmd {
	if m.sessionPendingChat == nil || m.sessionLoading || m.state != stateReady {
		return cmd
	}
	pending := m.sessionPendingChat
	m.sessionPendingChat = nil
	return tea.Batch(cmd, m.startChat(pending.text, pending.images))
}

func (m *Model) requestHistoryPage(older bool) tea.Cmd {
	if m.sessionLoading || m.state == stateProcessing {
		return nil
	}
	target := m.sessionHistoryPage - 1
	if older {
		target = m.sessionHistoryPage + 1
		if target >= len(m.sessionHistoryCursors) {
			m.sessionHistoryCursors = append(m.sessionHistoryCursors, m.sessionHistoryBefore)
		}
	}
	if target < 0 || target >= len(m.sessionHistoryCursors) {
		return nil
	}
	load := m.loadSessionWindow(m.sessionHistoryCursors[target], target)
	return tea.Batch(spinnerTick(), load)
}

func restoredChatMessages(history []controlruntime.DisplayMessage) []message.ChatMessage {
	messages := make([]message.ChatMessage, 0, len(history))
	for _, dm := range history {
		var blocks []block.ContentBlock
		for _, b := range dm.Blocks {
			brief, content := interaction.ToolBrief(b.ToolName, b.Args), b.Content
			if b.ToolName == "子任务" {
				brief, content = b.Content, "Ctrl+G 查看任务记录"
			}
			blocks = append(blocks, block.ContentBlock{
				Type:       block.BlockTool,
				ToolName:   b.ToolName,
				ToolArgs:   brief,
				ToolAction: interaction.ToolAction(b.ToolName, b.Args),
				Content:    content,
				Done:       true,
				IsError:    b.IsError,
			})
		}
		messages = append(messages, message.ChatMessage{
			Role:            dm.Role,
			Content:         dm.Content,
			Reasoning:       dm.Reasoning,
			RenderedContent: dm.Content,
			Blocks:          blocks,
		})
	}
	return messages
}
