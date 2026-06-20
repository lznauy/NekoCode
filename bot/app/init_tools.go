package app

import (
	"nekocode/bot/hooks"
	"nekocode/bot/index/projecttool"
	"nekocode/bot/tools"
	"nekocode/bot/tools/catalog"
	edittool "nekocode/bot/tools/filesystem/edit"
)

func (b *Bot) initToolRegistry() {
	b.toolRegistry = tools.NewRegistry()
	catalog.RegisterAll(b.toolRegistry, b.cfg.ImageGenModels)

	if b.indexMgr != nil {
		b.toolRegistry.Register(projecttool.NewProjectInfoTool(b.indexMgr))
	}

	edittool.InitBlockResolver()
}

func (b *Bot) initHooks() {
	b.hookReg = hooks.NewRegistry()
	hooks.RegisterBuiltin(b.hookReg)
}
