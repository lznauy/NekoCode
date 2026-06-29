package app

import (
	"nekocode/common"
)

func (b *Bot) Stats() common.BotStats {
	ag := b.getAgent()

	p, c := ag.TokenUsage()
	tp, tc := ag.TurnTokenUsage()
	d := ag.Duration()
	compactCount, _ := b.ctxMgr.CompactStats()
	return common.BotStats{
		PromptTokens: p, CompletionTokens: c,
		TurnPrompt: tp, TurnCompletion: tc,
		ContextTokens: ag.ContextTokens(),
		CompactCount:  compactCount,
		Duration:      common.FormatDuration(d),
	}
}
