package agent

import (
	"context"

	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/hooks"
	"nekocode/bot/tools"
	"nekocode/llm/types"

	"nekocode/bot/agent/runtime"
)

type Agent = runtime.Agent
type ContextTransform = runtime.ContextTransform
type StreamCallback = runtime.StreamCallback
type ReasoningCallback = runtime.ReasoningCallback
type RunResult = runtime.RunResult
type RunCallback = runtime.RunCallback
type GovManager = runtime.GovManager
type ResponseGate = runtime.ResponseGate
type SubSlotManager = runtime.SubSlotManager
type ToolQuotaData = runtime.ToolQuotaData
type ToolCallInfo = runtime.ToolCallInfo

func New(ctx context.Context, ctxMgr *ctxmgr.Manager, llmClient types.LLM, toolRegistry *tools.Registry) *Agent {
	return runtime.New(ctx, ctxMgr, llmClient, toolRegistry)
}

func NewResponseGate() *ResponseGate {
	return runtime.NewResponseGate()
}

func NewSubSlotManager() *SubSlotManager {
	return runtime.NewSubSlotManager()
}

func NewGovernanceManager(hookReg *hooks.Registry) *GovManager {
	return runtime.NewGovernanceManager(hookReg)
}
