package app

import (
	"context"

	"nekocode/bot/agent/runtime"
	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/governance"
	"nekocode/bot/llm"
	"nekocode/bot/llm/types"
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

	b.ag.SetContextTransform(func(msgs []types.Message) []types.Message {
		return ctxmgr.ApplyToolResultGuardrail(msgs, ctxmgr.ToolResultGuardrailOptions{
			LastWarned: &b.lastGuardrailWarned,
			Warning:    governance.ToolResultWarning,
		})
	})

	b.subWiring.WireTaskTool(fm)
}
