package agent

import (
	"context"

	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/llm/types"
	"nekocode/bot/tools"

	"nekocode/bot/agent/runtime"
)

type Agent = runtime.Agent

func New(ctx context.Context, ctxMgr *ctxmgr.Manager, llmClient types.LLM, toolRegistry *tools.Registry) *Agent {
	return runtime.New(ctx, ctxMgr, llmClient, toolRegistry)
}
