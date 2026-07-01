package subagent

import (
	"context"

	"nekocode/bot/agent/runtime/model"
	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/llm/types"
	"nekocode/bot/tools"
)

func (e *Engine) reason(ctx context.Context, mgr *ctxmgr.Manager, allowed []string, addTokens func(int, int), phase func(string)) ([]tools.ToolCallItem, string, error) {
	toolDefs := e.filteredToolDefs(allowed)
	result, err := model.CallLLMWithRetry(ctx, e.llmClient, func() tools.LLMCallOptions {
		return tools.LLMCallOptions{
			Ctx:      ctx,
			Messages: mgr.Build(),
			ToolDefs: toolDefs,
			Callbacks: tools.StreamCallbacks{
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
		mgr.AddAssistantToolCall(result.Text, result.Reasoning, tools.ToLLMToolCalls(result.ToolCalls))
	}
	return result.ToolCalls, result.Text, nil
}

func (e *Engine) filteredToolDefs(allowed []string) []types.ToolDef {
	all := e.toolRegistry.Descriptors()
	set := make(map[string]bool, len(allowed))
	for _, n := range allowed {
		set[n] = true
	}
	var filtered []tools.Descriptor
	for _, d := range all {
		if d.Name == taskToolName {
			continue
		}
		if set[d.Name] {
			filtered = append(filtered, d)
		}
	}
	return tools.ToToolDefs(filtered)
}
