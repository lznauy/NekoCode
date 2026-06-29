package app

import "nekocode/common"

func (b *Bot) configureCallbacks(confirmFn common.ConfirmFunc, phaseFn common.PhaseFunc, todoFn common.TodoFunc, notifyFn func(string), confirmCh chan common.ConfirmRequest) {
	b.confirmFn = confirmFn
	b.phaseFn = phaseFn
	b.todoFn = todoFn
	b.notifyFn = notifyFn
	b.confirmCh = confirmCh
	b.applyAgentCallbacks()
}

func (b *Bot) applyAgentCallbacks() {
	if b.ag == nil {
		return
	}
	b.ag.SetConfirmFn(b.confirmFn)
	b.ag.SetPhaseFn(b.phaseFn)
	b.ag.WireTodoWrite(func(items []common.TodoItem) {
		b.ctxMgr.SetTodos(items)
		if b.todoFn != nil {
			b.todoFn(items)
		}
	})
}

func (b *Bot) pendingConfirmation() bool {
	b.confirmMu.Lock()
	defer b.confirmMu.Unlock()
	return b.pendingConfirm
}

func (b *Bot) setPendingConfirmation(pending bool) {
	b.confirmMu.Lock()
	b.pendingConfirm = pending
	b.confirmMu.Unlock()
}
