package app

import "nekocode/common"

func (b *Bot) Configure(confirmFn common.ConfirmFunc, phaseFn common.PhaseFunc, todoFn common.TodoFunc, notifyFn func(string), confirmCh chan common.ConfirmRequest, questionFn common.QuestionFunc) {
	b.cb.Configure(confirmFn, phaseFn, todoFn, notifyFn, confirmCh, questionFn)
}

func (b *Bot) SetCallbacks(textFn, reasonFn func(string)) {
	b.cb.SetCallbacks(textFn, reasonFn)
}
