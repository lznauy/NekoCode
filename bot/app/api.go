package app

import (
	"nekocode/bot/agent"
	"nekocode/bot/command"
	"nekocode/common"
)

func (b *Bot) Steer(msg string) { b.getAgent().Steer(msg) }
func (b *Bot) Abort()           { b.getAgent().Abort() }

func (b *Bot) ProviderModel() (string, string) {
	am := b.cfg.ActiveModelConfig()
	return am.Provider, am.Model
}

func (b *Bot) CommandNames() []string { return b.cmdParser.Commands() }

func (b *Bot) ExecuteCommand(input string) (string, common.CmdResult) {
	b.skillState.WantsAgent = false
	cmd := b.cmdParser.Parse(input)
	if cmd.Name == "" {
		command.ClearSkillContext(b.ctxMgr, b.skillState)
		return "", common.CmdNone
	}
	resp, _ := b.cmdParser.Execute(cmd)

	pending := b.pendingConfirmation()
	resumed := b.sessionResumed
	result := commandResult(pending, resumed)
	if resumed {
		b.sessionResumed = false
	}
	return resp, result
}

func (b *Bot) SkillHint() (string, bool) {
	hint := b.skillState.Hint
	cont := b.skillState.WantsAgent
	b.skillState.Hint = ""
	b.skillState.WantsAgent = false
	return hint, cont
}

func (b *Bot) getAgent() *agent.Agent {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.ag
}
