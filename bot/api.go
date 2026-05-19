package bot

import (
	"time"

	"nekocode/bot/command"

	"nekocode/common"
)

// -- TUI interface (BotInterface) ------------------------------------------

func (b *Bot) Steer(msg string)                         { b.ag.Steer(msg) }
func (b *Bot) Abort()                                   { b.ag.Abort() }
func (b *Bot) Provider() string                         { return b.cfg.Provider }
func (b *Bot) Model() string                            { return b.cfg.Model }
func (b *Bot) ContextTokens() int                       { return b.ag.ContextTokens() }
func (b *Bot) CompactCount() int                        { return b.ctxMgr.CompactCount() }
func (b *Bot) CommandNames() []string                   { return b.cmdParser.Commands() }
func (b *Bot) TokenUsage() (prompt, completion int)     { return b.ag.TokenUsage() }
func (b *Bot) TurnTokenUsage() (prompt, completion int) { return b.ag.TurnTokenUsage() }

func (b *Bot) ExecuteCommand(input string) (string, bool) {
	b.skillState.WantsAgent = false
	cmd := b.cmdParser.Parse(input)
	if cmd.Name == "" {
		command.ClearSkillContext(b.ctxMgr, b.skillState)
		return "", false
	}
	return b.cmdParser.Execute(cmd)
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

func (b *Bot) Configure(confirmFn common.ConfirmFunc, phaseFn common.PhaseFunc, todoFn common.TodoFunc) {
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

func (b *Bot) Duration() string {
	d := b.ag.Duration()
	if d == 0 {
		return ""
	}
	if d < time.Second {
		return "0s"
	}
	return d.Truncate(100 * time.Millisecond).String()
}
