package app

import (
	"nekocode/bot/command"
	"nekocode/common"
)

func (b *Bot) ContextStatus() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return command.ContextStats(b.ctxMgr)
}

func (b *Bot) ContextReport() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return command.ContextReport(b.ctxMgr, b.toolRegistry.Descriptors())
}

func (b *Bot) ContextSnapshot() common.ContextSnapshot {
	b.mu.Lock()
	defer b.mu.Unlock()

	r := b.ctxMgr.Report()
	r.ToolDefCount = len(b.toolRegistry.Descriptors())
	r.ToolDefTokens = command.EstimateToolDefTokens(b.toolRegistry.Descriptors())
	return buildContextSnapshot(r)
}
