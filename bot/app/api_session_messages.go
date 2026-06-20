package app

import (
	"nekocode/bot/sessionview"
	"nekocode/common"
)

func (b *Bot) SessionMessages() []common.DisplayMessage {
	snap := b.ctxMgr.Snapshot()
	return sessionview.DisplayMessages(snap.Messages, snap.CompactBoundary)
}
