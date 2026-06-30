package runtime

import (
	"context"
	"strings"

	"nekocode/bot/agent/runtime/reasoning"
	"nekocode/bot/debug"
	"nekocode/bot/hooks"
	"nekocode/bot/llm"
	"nekocode/bot/llm/types"
	"nekocode/bot/tools"

	"nekocode/common"
)

type ActionType = reasoning.ActionType
type ReasoningResult = reasoning.Result

const (
	ActionChat        = reasoning.ActionChat
	ActionExecuteTool = reasoning.ActionExecuteTool
)

func (a *Agent) Reason(input string) *ReasoningResult {
	if a.phase != nil {
		a.phase(common.PhaseThinking)
	}
	if strings.HasPrefix(input, "/") && !strings.Contains(input, " ") {
		return reasoning.CommandResult()
	}

	toolCalls, textContent, err := a.callLLMForTool()
	return reasoning.FromLLM(toolCalls, textContent, err)
}

func (a *Agent) callLLMForTool() ([]tools.ToolCallItem, string, error) {
	toolDefs := tools.ToToolDefs(a.toolRegistry.Descriptors())
	var items []tools.ToolCallItem
	var textContent string

	err := withRetry(a.getCtx(), func() error {
		messages := a.ctxMgr.Build(true)
		messages = a.applyPreModelRequestHooks(messages)

		result, err := tools.CallLLM(a.llmClient, tools.LLMCallOptions{
			Ctx:       a.getCtx(),
			Messages:  messages,
			ToolDefs:  toolDefs,
			Callbacks: a.streamCallbacks(),
			CheckDone: func() bool { return a.finished.Load() },
		})
		if err != nil {
			return err
		}

		textContent = result.Text
		if result.Reasoning != "" {
			a.lastReason = result.Reasoning
		}
		if len(result.ToolCalls) == 0 {
			return nil
		}

		items = result.ToolCalls
		a.ctxMgr.AddAssistantToolCall(textContent, a.lastReason, tools.ToLLMToolCalls(items))
		return nil
	})

	return items, textContent, err
}

func (a *Agent) applyPreModelRequestHooks(messages []types.Message) []types.Message {
	if a.gov == nil || a.gov.HookReg == nil {
		return messages
	}
	a.gov.HookReg.Set(hooks.StoreToolResultCount, countToolResults(messages))
	var hints []hooks.Hint
	for _, r := range a.gov.HookReg.Evaluate(hooks.PreModelRequest, "", false) {
		if r.Hint != nil {
			hints = append(hints, *r.Hint)
		}
	}
	if len(hints) == 0 {
		return messages
	}
	return append(messages, types.Message{
		Role:    "system",
		Content: hooks.FormatHints(hints),
		Source:  "hook",
	})
}

func countToolResults(messages []types.Message) int64 {
	var count int64
	for _, m := range messages {
		if m.Role == "tool" {
			count++
		}
	}
	return count
}

// streamCallbacks returns the StreamCallbacks for the main agent.
func (a *Agent) streamCallbacks() tools.StreamCallbacks {
	return tools.StreamCallbacks{
		OnText: func(delta string) {
			if a.textFn != nil {
				a.textFn(delta, false)
			}
		},
		OnReasoning: func(delta string) {
			if a.reasonFn != nil {
				a.reasonFn(delta)
			}
		},
		OnPhase: func(phase string) {
			if a.phase != nil {
				a.phase(phase)
			}
		},
		AddTokens: func(prompt, completion int) {
			a.AddTokens(prompt, completion)
		},
		RecordUsage: func(prompt, completion int) {
			a.ctxMgr.RecordUsage(prompt, completion)
		},
		RecordCache: func(hit, miss int) {
			a.ctxMgr.RecordCache(hit, miss)
		},
	}
}

func withRetry(ctx context.Context, fn func() error) error {
	var attempt int
	return llm.Retry(ctx, llm.DefaultRetryConfig, func() error {
		err := fn()
		if err != nil && llm.IsRetryable(err) {
			attempt++
			debug.Log("retry %d: %v", attempt, err)
		}
		return err
	})
}
