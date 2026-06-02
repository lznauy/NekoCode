package bot

import (
	"fmt"
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

	b.initAgent()
	b.ctxMgr.ResetCache()

	am := b.cfg.ActiveModelConfig()
	return am.Model, am.Provider, nil
}
