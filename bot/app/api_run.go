package app

import (
	"nekocode/bot/command"
	"nekocode/common"
)

func (b *Bot) RunAgent(input string, onStep func(action, toolName, toolArgs, output string)) (string, error) {
	b.lastGuardrailWarned = 0
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
