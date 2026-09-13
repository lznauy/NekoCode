// agent.go — 启动 agent 对话流程：startChat、startAgent。
package tui

import (
	"context"
	"strings"

	"nekocode/interaction"
	"nekocode/interaction/tui/components/block"
	"nekocode/interaction/tui/components/message"
	controlruntime "nekocode/runtime"

	tea "charm.land/bubbletea/v2"
)

func (m *Model) startChat(value string) tea.Cmd {
	if m.tryLocalCommand(value) {
		return nil
	}
	m.transitionTo(stateProcessing)
	m.Messages.SetSpinnerView(m.Spinner.View())
	status := PhaseWaiting
	if isCompactCommand(value) {
		status = phaseCompacting
	}
	m.setPhase(status)
	m.Messages.SetProcessingStatus(status)
	if _, err := m.Runtime.StartRun(context.Background(), controlruntime.Input{
		Source: controlruntime.SourceRef{Kind: "tui"},
		Text:   value,
	}); err != nil {
		m.transitionTo(stateReady)
		m.Messages.AddMessage(message.ChatMessage{
			Role:    "error",
			Content: err.Error(),
		})
		return nil
	}
	return spinnerTick()
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
// task is in progress.
func (m *Model) tryLocalCommand(value string) bool {
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
		return true
	case controlruntime.LocalCommandRequiresIdle:
		if m.state == stateProcessing {
			addSystem("命令 " + value + " 需在任务结束后执行")
			return true
		}
	}
	return false
}

func interactiveTool(toolName string) bool {
	return toolName == "todo_write" || toolName == "question"
}

// loadSessionMessages populates the TUI message list from a restored session.
func (m *Model) loadSessionMessages() {
	m.Messages.SetProcessing(false)
	m.Messages.SetItems()
	for _, dm := range m.Runtime.SessionMessages() {
		var blocks []block.ContentBlock
		for _, b := range dm.Blocks {
			blocks = append(blocks, block.ContentBlock{
				Type:       block.BlockTool,
				ToolName:   b.ToolName,
				ToolArgs:   interaction.ToolBrief(b.ToolName, b.Args),
				ToolAction: interaction.ToolAction(b.ToolName, b.Args),
				Content:    b.Content,
				Done:       true,
				IsError:    b.IsError,
			})
		}
		m.Messages.AddMessage(message.ChatMessage{
			Role:            dm.Role,
			Content:         dm.Content,
			Reasoning:       dm.Reasoning,
			RenderedContent: dm.Content,
			Blocks:          blocks,
		})
	}
	m.Messages.GotoBottom()
}
