package bot

import (
	"time"

	"nekocode/bot/command"

	"nekocode/common"
)

// -- TUI interface (BotInterface) ------------------------------------------

func (b *Bot) Steer(msg string)                         { b.ag.Steer(msg) }
func (b *Bot) Abort()                                   { b.ag.Abort() }
func (b *Bot) ProviderModel() (string, string) { return b.cfg.Provider, b.cfg.Model }
func (b *Bot) CommandNames() []string { return b.cmdParser.Commands() }

func (b *Bot) Stats() common.BotStats {
	p, c := b.ag.TokenUsage()
	tp, tc := b.ag.TurnTokenUsage()
	d := b.ag.Duration()
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
		ContextTokens: b.ag.ContextTokens(),
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
	result := b.ag.Run(input, onStep)
	b.ag.SetPlanMode(false)
	b.ctxMgr.SetSystemPrompt(b.promptBuilder.Build())
	command.SummarizeIfNeeded(b.ctxMgr)
	b.saveSession()
	return result.FinalOutput, result.Error
}

func (b *Bot) Configure(confirmFn common.ConfirmFunc, phaseFn common.PhaseFunc, todoFn common.TodoFunc, notifyFn func(string), confirmCh chan common.ConfirmRequest) {
	b.confirmFn = confirmFn
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
	b.ag.SetStreamFn(func(delta string, _ bool) { textFn(delta) })
	b.ag.SetReasoningStreamFn(reasonFn)
}
