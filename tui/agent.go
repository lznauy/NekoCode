// agent.go — 启动 agent 对话流程：startChat、startAgent。
package tui

import (
	"fmt"

	"nekocode/common"
	"nekocode/tui/components/block"
	"nekocode/tui/components/message"

	tea "charm.land/bubbletea/v2"
)

func (m *Model) startChat(value string) tea.Cmd {
	resp, cr := m.Bot.ExecuteCommand(value)
	if cr != common.CmdNone && resp != "" {
		m.Messages.AddMessage(message.ChatMessage{
			Role: "system", Title: value, Content: resp, RenderedContent: resp,
		})
	}
	// Refresh header after command (e.g. /model switch)
	prov, mod := m.Bot.ProviderModel()
	m.Header.SetModel(prov, mod)
	if hint, wantsAgent := m.Bot.SkillHint(); wantsAgent {
		m.activeSkill = hint
		return m.startAgent(value)
	}
	m.activeSkill = ""
	switch cr {
	case common.CmdConfirming:
		return listenConfirm(m.confirmCh)
	case common.CmdSessionResumed:
		m.loadSessionMessages()
		return nil
	case common.CmdHandled:
		return nil
	}
	return m.startAgent(value)
}

func (m *Model) startAgent(value string) tea.Cmd {
	m.Messages.AddMessage(message.ChatMessage{Role: "user", Content: value})
	m.Messages.GotoBottom()
	m.Input.SetFollow(true)
	m.transitionTo(stateProcessing)

	// Show active skill for this turn in the status bar.
	if m.activeSkill != "" {
		m.Messages.SetSkill(m.activeSkill)
	}

	m.Bot.SetCallbacks(
		func(delta string) { m.Messages.ProcessStreamText(delta) },
		func(delta string) { m.Messages.ProcessThinkingText(delta) },
	)

	return tea.Batch(
		spinnerTick(),
		listenConfirm(m.confirmCh),
		listenQuestion(m.questionCh),
		m.runAgent(value),
	)
}

func (m *Model) runAgent(value string) func() tea.Msg {
	return func() (msg tea.Msg) {
		defer func() {
			if r := recover(); r != nil {
				common.WritePanicLog(r)
				// Ensure the TUI does not get stuck in stateProcessing.
				msg = doneMsg{
					content: fmt.Sprintf("internal panic: %v", r),
					err:     fmt.Errorf("panic: %v", r),
				}
			}
		}()

		var finalResponse string

		result, err := m.Bot.RunAgent(value, m.onAgentStep(&finalResponse))

		// Use RunAgent returned FinalOutput as the primary source.
		// finalResponse from callbacks can be stale when hooks trigger
		// intermediate turns without "chat" actions.
		if result != "" {
			finalResponse = result
		}
		if finalResponse == "" {
			finalResponse = "sorry, could not complete this task."
		}

		return doneMsg{
			content:  finalResponse,
			duration: m.Bot.Stats().Duration,
			tokens:   tokensSummary(m.Bot),
			err:      err,
		}
	}
}

func (m *Model) onAgentStep(finalResponse *string) func(string, string, string, string) {
	return func(action, toolName, toolArgs, output string) {
		switch {
		case action == "think":
		case action == "chat":
			*finalResponse = output
			m.Messages.AddThinkBlock(output)
		case action == "sub_agent_start":
			// toolName = subType, toolArgs = subID, output = colorIdx
			colorIdx := 0
			if n, err := fmt.Sscanf(output, "%d", &colorIdx); err != nil || n != 1 {
				colorIdx = -1
			}
			m.Messages.AddSubAgent(toolArgs, toolName, colorIdx)
		case action == "sub_agent_end":
			// toolArgs = subID
			m.Messages.RemoveSubAgent(toolArgs)
		case action == "sub_tool_start":
			// toolName = actual tool name, toolArgs = args, output = subID:colorIdx
			subID, colorIdx := parseSubEvent(output)
			m.Messages.ProcessToolBlock(block.ContentBlock{
				Type:      block.BlockTool,
				ToolName:  toolName,
				ToolArgs:  formatBriefArgs(toolName, toolArgs),
				Content:   "",
				Collapsed: !block.IsPersistent(toolName),
				SubID:     subID,
				SubColor:  colorIdx,
			})
		case action == "sub_execute_tool":
			// toolName = actual tool name, output = text, toolArgs = subID:colorIdx
			subID, _ := parseSubEvent(toolArgs)
			m.Messages.AddSubToolOutput(subID, toolName, output)
		case action == "tool_start":
			m.Messages.ProcessToolBlock(block.ContentBlock{
				Type:      block.BlockTool,
				ToolName:  toolName,
				ToolArgs:  formatBriefArgs(toolName, toolArgs),
				Content:   output,
				Collapsed: !block.IsPersistent(toolName),
			})
		case action == "tool_blocked":
			// Blocked by quota — create a tool block showing the rejection reason.
			m.Messages.ProcessToolBlock(block.ContentBlock{
				Type:      block.BlockTool,
				ToolName:  toolName,
				ToolArgs:  formatBriefArgs(toolName, toolArgs),
				Content:   output,
				Collapsed: false,
				Done:      true,
			})
		case action == "tool_preview":
			m.Messages.UpdateToolPreview(toolName, output)
		case toolName != "":
			m.Messages.AddToolOutput(toolName, output)
		}
	}
}

// parseSubEvent parses "subID:colorIdx" from event payload.
func parseSubEvent(payload string) (subID string, colorIdx int) {
	colorIdx = -1
	for i := len(payload) - 1; i >= 0; i-- {
		if payload[i] == ':' {
			if n, err := fmt.Sscanf(payload[i+1:], "%d", &colorIdx); err != nil || n != 1 {
				colorIdx = -1
			}
			subID = payload[:i]
			return
		}
	}
	subID = payload
	return
}

// loadSessionMessages populates the TUI message list from a restored session.
func (m *Model) loadSessionMessages() {
	for _, dm := range m.Bot.SessionMessages() {
		var blocks []block.ContentBlock
		for _, b := range dm.Blocks {
			blocks = append(blocks, block.ContentBlock{
				Type:      block.BlockTool,
				ToolName:  b.ToolName,
				Content:   b.Content,
				Done:      true,
				Collapsed: false, // persistent tools always expanded
			})
		}
		m.Messages.AddMessage(message.ChatMessage{
			Role:            dm.Role,
			Content:         dm.Content,
			RenderedContent: dm.Content,
			Blocks:          blocks,
		})
	}
	m.Messages.GotoBottom()
}
