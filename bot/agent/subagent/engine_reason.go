package subagent

import (
	"context"

	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/tools"
	"nekocode/llm"
	"nekocode/llm/types"
)

func (e *Engine) reason(ctx context.Context, mgr *ctxmgr.Manager, allowed []string, addTokens func(int, int), phase func(string)) ([]tools.ToolCallItem, string, error) {
	var calls []tools.ToolCallItem
	var textContent string
	var reasoningContent string

	toolDefs := e.filteredToolDefs(allowed)
	firstAttempt := true
	err := llm.Retry(ctx, llm.DefaultRetryConfig, func() error {
		result, err := tools.CallLLM(e.llmClient, tools.LLMCallOptions{
			Ctx:      ctx,
			Messages: mgr.Build(true),
			ToolDefs: toolDefs,
			Callbacks: tools.StreamCallbacks{
				OnPhase: phase,
				AddTokens: func(p, c int) {
					if addTokens != nil {
						addTokens(p, c)
					}
				},
			},
			CheckDone:      func() bool { return false },
			EstimatePrompt: firstAttempt,
		})
		if err != nil {
			return err
		}

		if firstAttempt {
			firstAttempt = false
		}

		textContent = result.Text
		reasoningContent = result.Reasoning
		calls = result.ToolCalls
		return nil
	})
	if err != nil {
		return nil, "", err
	}

	if len(calls) > 0 {
		mgr.AddAssistantToolCall(textContent, reasoningContent, tools.ToLLMToolCalls(calls))
	}
	return calls, textContent, nil
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
