package core

import (
	"nekocode/bot/extension/tool/runtime/taskbridge"
	"nekocode/bot/session"
)

func (b *Bot) WireTaskLister(list taskbridge.TaskLister) {
	b.toolbox.WireTaskLister(list)
}

// These readers use an explicit parent identity so a delayed UI request cannot
// silently switch to another conversation while the user navigates sessions.
func (b *Bot) Subagents(parentID string) ([]session.SubagentInfo, error) {
	return session.ListSubagents(parentID)
}

func (b *Bot) SubagentAttempts(parentID, id string, before, limit int) (session.SubagentAttemptPage, error) {
	return session.SubagentAttempts(parentID, id, before, limit)
}

func (b *Bot) SubagentHistory(parentID, id string, before, limit int) (session.HistoryPage, error) {
	return session.SubagentHistory(parentID, id, before, limit)
}

func (b *Bot) SubagentToolOutput(parentID, id string, sequence int, offset int64) (session.SubagentOutputPage, error) {
	return session.SubagentToolOutput(parentID, id, sequence, offset)
}
