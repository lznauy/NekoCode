// agent.go — 启动 agent 对话流程：startChat、startAgent。
package tui

import (
	"fmt"
	"os"
	"runtime/debug"
	"time"

	"nekocode/common"
	"nekocode/tui/components/block"
	"nekocode/tui/components/message"

	tea "charm.land/bubbletea/v2"
)

func logPanic(r any) {
	stack := debug.Stack()
	path := fmt.Sprintf("/tmp/nekocode/nekocode-panic-%d.log", time.Now().Unix())
	msg := fmt.Sprintf("PANIC: %v\n\nStack:\n%s", r, string(stack))
	_ = os.WriteFile(path, []byte(msg), 0644)
}

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
		func(delta string) { m.Messages.ProcessReasoningText(delta) },
	)

	return tea.Batch(
		spinnerTick(),
		listenConfirm(m.confirmCh),
		m.runAgent(value),
	)
}

func (m *Model) runAgent(value string) func() tea.Msg {
	return func() tea.Msg {
		defer func() {
			if r := recover(); r != nil {
				logPanic(r)
			}
		}()

		var finalResponse string

		result, err := m.Bot.RunAgent(value, m.onAgentStep(&finalResponse))

		if finalResponse == "" {
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
		case action == "tool_start":
			m.Messages.ProcessToolBlock(block.ContentBlock{
				Type:      block.BlockTool,
				ToolName:  toolName,
				ToolArgs:  formatBriefArgs(toolName, toolArgs),
				Content:   output,
				Collapsed: toolName != "edit" && toolName != "write" && toolName != "bash",
			})
		case action == "tool_preview":
			m.Messages.UpdateToolPreview(toolName, output)
		case toolName != "":
			m.Messages.AddToolOutput(toolName, output)
		}
	}
}

