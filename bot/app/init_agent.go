package app

import (
	"context"

	"nekocode/bot/agent/runtime"
	"nekocode/bot/llm"
)

func (b *Bot) initAgent() {
	am := b.cfg.ActiveModelConfig()
	llmClient := llm.NewClientWithProtocol(am.Provider, am.APIKey, am.BaseURL, am.Model, am.Protocol)

	fm := b.cfg.ResolveModel(b.cfg.FlashModel)
	mergeClient := llm.NewClientWithProtocol(fm.Provider, fm.APIKey, fm.BaseURL, fm.Model, fm.Protocol)
	mergeClient.SetDisableThinking(true)
	mergeClient.SetMaxTokens(2000)
	b.ctxMgr.MergeClient = mergeClient

	b.ag = runtime.New(context.Background(), b.ctxMgr, llmClient, b.toolRegistry)
	b.ag.SetHookRegistry(b.hookReg)
	b.cb.applyAgentCallbacksTo(b.ag)

	b.subWiring.WireTaskTool(fm)
}
