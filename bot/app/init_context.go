package app

import (
	"nekocode/bot/app/contextinit"
	"nekocode/bot/config"
	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/contextmgr/memory"
	"nekocode/bot/prompt"
)

func (b *Bot) initConfig() {
	b.cfg, _ = config.Load()
	b.promptBuilder = prompt.NewBuilder(b.cwd)
}

func (b *Bot) initCtxMgr() {
	systemPrompt := b.promptBuilder.Build()
	memFile, _ := memory.Load(memory.DefaultPath())
	b.ctxMgr = ctxmgr.New(ctxmgr.Config{SystemPrompt: systemPrompt, Memory: memFile})

	result := contextinit.ApplyProjectContextAndIndex(b.ctxMgr, contextinit.Options{
		CWD:           b.cwd,
		ContextWindow: b.cfg.ContextWindow,
	})
	b.projCtx = result.ProjectContext
	b.indexMgr = result.IndexManager
}

func (b *Bot) initSummarizer() {
	b.ctxMgr.CM.Summarizer = ctxmgr.MakeSummarizer(b.ctxMgr.CM.CancelCtx, b.ctxMgr.MergeClient)
}
