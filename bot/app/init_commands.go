package app

import (
	"nekocode/bot/command"
)

func (b *Bot) initCommands() {
	command.RegisterAll(b.cmdParser, command.Deps{
		CtxMgr:        b.ctxMgr,
		Ag:            func() command.PlanModeController { return b.getAgent() },
		Skills:        b.ext.skills,
		ToolRegistry:  b.toolRegistry,
		ContextWindow: b.cfg.ContextWindow,
		GetConfigFn:   b.ProviderModel,
		ListModelsFn:  b.cfg.AllModelNames,
		FreshStart: func() (string, error) {
			return command.ForceFreshStart(b.ctxMgr, b.ext.skills, b.hookReg)
		},
		SwitchModel: b.SwitchModel,
	}, b.skillState)

	b.ext.RegisterPluginCommands(b.cmdParser)
	b.sess.RegisterCommands(b.cmdParser)
}
