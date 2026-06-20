package app

import (
	"fmt"

	"nekocode/bot/app/pluginops"
	"nekocode/bot/extension/plugin"
	"nekocode/common"
)

func (b *Bot) unblockConfirm() {
	b.confirmMu.Lock()
	b.pendingConfirm = false
	b.confirmMu.Unlock()
	if b.confirmCh != nil {
		select {
		case b.confirmCh <- common.ConfirmRequest{Response: nil}:
		default:
		}
	}
}

func (b *Bot) confirmInstall(source string, p *plugin.Plugin, isRemote bool) bool {
	summary := pluginops.ConfirmSummary(p, isRemote)
	if b.confirmFn == nil {
		b.unblockConfirm()
		return false
	}
	result := b.confirmFn(common.NewConfirmRequest("/plugin install", map[string]any{"source": source, "summary": summary}, common.LevelWrite))
	b.confirmMu.Lock()
	b.pendingConfirm = false
	b.confirmMu.Unlock()
	if !result && b.notifyFn != nil {
		b.notifyFn(fmt.Sprintf("Install cancelled: %s", source))
	}
	return result
}
