package app

import (
	"nekocode/bot/command"
)

func (b *Bot) initCommands() {
	command.RegisterAll(b.cmdParser, command.Deps{
		CtxMgr:        b.ctxMgr,
		Ag:            b.getAgent,
		Skills:        b.skills,
		ToolRegistry:  b.toolRegistry,
		ContextWindow: b.cfg.ContextWindow,
		GetConfigFn:   b.ProviderModel,
		ListModelsFn:  b.cfg.AllModelNames,
		FreshStart: func() (string, error) {
			return command.ForceFreshStart(b.ctxMgr, b.skills, b.hookReg)
		},
		SwitchModel: b.SwitchModel,
	}, b.skillState)

	b.registerSessionCommand()
	b.registerExportCommand()
	b.registerPluginCommands()
}
