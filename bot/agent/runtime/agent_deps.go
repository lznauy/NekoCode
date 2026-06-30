package runtime

import (
	"nekocode/bot/agent/runtime/subagents"
	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/llm/types"
	aggov "nekocode/bot/policy"
	"nekocode/bot/tools"
)

type agentDeps struct {
	ctxMgr       *ctxmgr.Manager
	llmClient    types.LLM
	toolRegistry *tools.Registry
	executor     *tools.Executor
	subSlotMgr   *subagents.SlotManager
	gov          *aggov.Manager
}

func newAgentDeps(ctxMgr *ctxmgr.Manager, llmClient types.LLM, toolRegistry *tools.Registry) agentDeps {
	return agentDeps{
		ctxMgr:       ctxMgr,
		llmClient:    llmClient,
		toolRegistry: toolRegistry,
		executor:     tools.NewExecutor(toolRegistry),
		subSlotMgr:   subagents.NewSlotManager(),
	}
}
