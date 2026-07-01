package subagent

import (
	"context"

	"nekocode/bot/agent/runtime/model"
	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/llm/types"
	"nekocode/bot/tools/llmstream"
	"nekocode/bot/tools/core"
)

func (e *Engine) reason(ctx context.Context, mgr *ctxmgr.Manager, allowed []string, addTokens func(int, int), phase func(string)) ([]core.ToolCallItem, string, error) {
	toolDefs := e.filteredToolDefs(allowed)
	result, err := model.CallLLMWithRetry(ctx, e.llmClient, func() llmstream.LLMCallOptions {
		return llmstream.LLMCallOptions{
			Ctx:      ctx,
			Messages: mgr.Build(),
			ToolDefs: toolDefs,
			Callbacks: llmstream.StreamCallbacks{
				OnPhase: phase,
				AddTokens: func(p, c int) {
					if addTokens != nil {
						addTokens(p, c)
					}
				},
			},
			CheckDone: func() bool { return false },
		}
	})
	if err != nil {
		return nil, "", err
	}

	if len(result.ToolCalls) > 0 {
		mgr.AddAssistantToolCall(result.Text, result.Reasoning, llmstream.ToLLMToolCalls(result.ToolCalls))
	}
	return result.ToolCalls, result.Text, nil
}

func (e *Engine) filteredToolDefs(allowed []string) []types.ToolDef {
	all := e.toolRegistry.Descriptors()
	set := make(map[string]bool, len(allowed))
	for _, n := range allowed {
		set[n] = true
	}
	var filtered []core.Descriptor
	for _, d := range all {
		if d.Name == taskToolName {
			continue
		}
		if set[d.Name] {
			filtered = append(filtered, d)
		}
	}
	return core.ToToolDefs(filtered)
}
