package app

import (
	"fmt"

	"nekocode/bot/command"
)

func (b *Bot) SelectSkill(name string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	skills := skillCommandProvider{manager: b.ext.skills}
	sk, ok := skills.GetForCommand(name)
	if !ok {
		return fmt.Errorf("skill %q not found", name)
	}
	command.ClearSkillContext(b.ctxMgr, b.skillState)
	b.skillState.MsgStart = b.ctxMgr.Len()
	b.ctxMgr.Add("user", sk.Context)
	b.skillState.MsgEnd = b.ctxMgr.Len()
	b.skillState.Hint = name
	skills.MarkLoaded(name)
	return nil
}

func (b *Bot) ClearSelectedSkill() {
	b.mu.Lock()
	defer b.mu.Unlock()
	command.ClearSkillContext(b.ctxMgr, b.skillState)
	b.skillState.Hint = ""
	b.skillState.WantsAgent = false
}
