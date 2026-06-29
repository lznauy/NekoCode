package app

import (
	"nekocode/bot/command"
	"nekocode/common"
)

func (b *Bot) Run(input string, callbacks common.RunCallbacks) (string, error) {
	if callbacks.Text != nil || callbacks.Reason != nil {
		b.SetCallbacks(callbacks.Text, callbacks.Reason)
	}
	return b.RunAgent(input, callbacks.Step)
}

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

func (b *Bot) Configure(confirmFn common.ConfirmFunc, phaseFn common.PhaseFunc, todoFn common.TodoFunc, notifyFn func(string), confirmCh chan common.ConfirmRequest, questionFn common.QuestionFunc) {
	b.configureCallbacks(confirmFn, phaseFn, todoFn, notifyFn, confirmCh)
	b.setQuestionFunc(questionFn)
}

func (b *Bot) setQuestionFunc(fn common.QuestionFunc) {
	t, err := b.toolRegistry.Get("question")
	if err != nil {
		return
	}
	if qt, ok := t.(interface{ SetQuestionFunc(common.QuestionFunc) }); ok {
		qt.SetQuestionFunc(fn)
	}
}

func (b *Bot) SetCallbacks(textFn, reasonFn func(string)) {
	ag := b.getAgent()
	if textFn != nil {
		ag.SetStreamFn(func(delta string, _ bool) { textFn(delta) })
	}
	if reasonFn != nil {
		ag.SetReasoningStreamFn(reasonFn)
	}
}
