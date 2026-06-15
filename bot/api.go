package bot

import (
	"fmt"
	"strings"
	"time"

	"nekocode/bot/agent"
	"nekocode/bot/command"

	"nekocode/common"
)

// -- TUI interface (BotInterface) ------------------------------------------

func (b *Bot) Steer(msg string) { b.getAgent().Steer(msg) }
func (b *Bot) Abort()           { b.getAgent().Abort() }
func (b *Bot) ProviderModel() (string, string) {
	am := b.cfg.ActiveModelConfig()
	return am.Provider, am.Model
}
func (b *Bot) CommandNames() []string { return b.cmdParser.Commands() }

func (b *Bot) Stats() common.BotStats {
	ag := b.getAgent()

	p, c := ag.TokenUsage()
	tp, tc := ag.TurnTokenUsage()
	d := ag.Duration()
	s := ""
	if d > 0 {
		if d < time.Second {
			s = "0s"
		} else {
			s = d.Truncate(100 * time.Millisecond).String()
		}
	}
	return common.BotStats{
		PromptTokens: p, CompletionTokens: c,
		TurnPrompt: tp, TurnCompletion: tc,
		ContextTokens: ag.ContextTokens(),
		CompactCount:  b.ctxMgr.CompactCount,
		Duration:      s,
	}
}

func (b *Bot) ExecuteCommand(input string) (string, common.CmdResult) {
	b.skillState.WantsAgent = false
	cmd := b.cmdParser.Parse(input)
	if cmd.Name == "" {
		command.ClearSkillContext(b.ctxMgr, b.skillState)
		return "", common.CmdNone
	}
	resp, _ := b.cmdParser.Execute(cmd)

	b.confirmMu.Lock()
	pending := b.pendingConfirm
	b.confirmMu.Unlock()
	if pending {
		return resp, common.CmdConfirming
	}
	if b.sessionResumed {
		b.sessionResumed = false
		return resp, common.CmdSessionResumed
	}
	return resp, common.CmdHandled
}

func (b *Bot) SkillHint() (string, bool) {
	hint := b.skillState.Hint
	cont := b.skillState.WantsAgent
	b.skillState.Hint = ""
	b.skillState.WantsAgent = false
	return hint, cont
}

func (b *Bot) RunAgent(input string, onStep func(action, toolName, toolArgs, output string)) (string, error) {
	b.lastGuardrailWarned = 0 // reset per-run so guardrail can fire again for new conversations
	ag := b.getAgent()
	result := ag.Run(input, onStep)
	ag.SetPlanMode(false)
	b.ctxMgr.SetSystemPrompt(b.promptBuilder.Build())
	command.SummarizeIfNeeded(b.ctxMgr)
	b.saveSession()
	return result.FinalOutput, result.Error
}

func (b *Bot) Configure(confirmFn common.ConfirmFunc, phaseFn common.PhaseFunc, todoFn common.TodoFunc, notifyFn func(string), confirmCh chan common.ConfirmRequest) {
	b.confirmFn = confirmFn
	b.phaseFn = phaseFn
	b.todoFn = todoFn
	b.notifyFn = notifyFn
	b.confirmCh = confirmCh
	b.ag.SetConfirmFn(confirmFn)
	b.ag.SetPhaseFn(phaseFn)
	b.ag.WireTodoWrite(func(items []common.TodoItem) {
		b.ctxMgr.SetTodos(items)
		if todoFn != nil {
			todoFn(items)
		}
	})
}

func (b *Bot) SetCallbacks(textFn, reasonFn func(string)) {
	ag := b.getAgent()
	ag.SetStreamFn(func(delta string, _ bool) { textFn(delta) })
	ag.SetReasoningStreamFn(reasonFn)
}

func (b *Bot) getAgent() *agent.Agent {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.ag
}

// SwitchModel switches to the named model and rebuilds LLM clients.
// Returns the new model name and provider, or an error if the model is not found.
func (b *Bot) SwitchModel(name string) (string, string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.cfg.SwitchModel(name) {
		return "", "", fmt.Errorf("model %q not found. Available: %v", name, b.cfg.AllModelNames())
	}

	// Save old agent's token counters before initAgent replaces it,
	// so session persistence doesn't lose accumulated stats.
	oldPrompt, oldCompl := b.ag.TokenUsage()

	b.initAgent()
	b.ag.AddTokens(oldPrompt, oldCompl)
	b.ctxMgr.ResetCache()

	am := b.cfg.ActiveModelConfig()
	return am.Model, am.Provider, nil
}

// SessionMessages returns the restored chat history after a session resume.
// Assistant turns with tool calls have their thinking text suppressed but
// persistent tool results (edit/write/bash) are preserved as Blocks so the
// TUI can render them — matching live streaming where FilterFinalBlocks
// keeps edit/write/bash output. Read-only tools (read/grep/glob) are skipped.
func (b *Bot) SessionMessages() []common.DisplayMessage {
	snap := b.ctxMgr.Snapshot()
	msgs := snap.Messages
	if snap.CompactBoundary > 0 && snap.CompactBoundary < len(msgs) {
		msgs = msgs[snap.CompactBoundary:]
	}

	// Map ToolCallID → tool name for pairing assistant ↔ tool results.
	toolNames := make(map[string]string, len(msgs))
	for _, m := range msgs {
		if m.Role == "assistant" {
			for _, tc := range m.ToolCalls {
				if tc.ID != "" {
					toolNames[tc.ID] = tc.Function.Name
				}
			}
		}
	}

	isPersistent := func(name string) bool {
		return name == "edit" || name == "write" || name == "bash"
	}
	isInternal := func(content string) bool {
		return strings.Contains(content, "<hints>") ||
			strings.Contains(content, "<skill") ||
			strings.Contains(content, "Current working directory") ||
			strings.Contains(content, "<system-reminder>") ||
			strings.HasPrefix(content, "[Hook:")
	}

	var out []common.DisplayMessage
	i := 0
	for i < len(msgs) {
		m := msgs[i]
		switch m.Role {
		case "user":
			if !isInternal(m.Content) {
				out = append(out, common.DisplayMessage{Role: "user", Content: m.Content})
			}
			i++
		case "assistant":
			var blocks []common.DisplayBlock
			if len(m.ToolCalls) > 0 {
				// Consume subsequent tool results for persistent tools.
				i++
				for i < len(msgs) && msgs[i].Role == "tool" {
					name := toolNames[msgs[i].ToolCallID]
					if isPersistent(name) {
						blocks = append(blocks, common.DisplayBlock{
							ToolName: name,
							Content:  msgs[i].Content,
						})
					}
					i++
				}
			} else {
				i++
			}
			content := m.Content
			if len(m.ToolCalls) > 0 {
				content = "" // thinking text, not final output
			}
			// Only emit when there is something to show — either a final
			// reply or persistent tool blocks.  Read-only turns produce
			// neither and would render as an empty "▐ Assistant".
			if content != "" || len(blocks) > 0 {
				out = append(out, common.DisplayMessage{
					Role:    "assistant",
					Content: content,
					Blocks:  blocks,
				})
			}
		case "system":
			if !isInternal(m.Content) {
				out = append(out, common.DisplayMessage{Role: "system", Content: m.Content})
			}
			i++
		default:
			// tool messages consumed by assistant loop above
			i++
		}
	}
	return out
}
