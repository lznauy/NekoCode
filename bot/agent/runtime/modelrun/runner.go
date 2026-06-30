package modelrun

import (
	"context"
	"strings"

	"nekocode/bot/agent/runtime/reasoning"
	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/debug"
	"nekocode/bot/hooks"
	"nekocode/bot/llm"
	"nekocode/bot/llm/types"
	aggov "nekocode/bot/policy"
	"nekocode/bot/tools"

	"nekocode/common"
)

type Host interface {
	Context() context.Context
	ContextManager() *ctxmgr.Manager
	LLM() types.LLM
	ToolRegistry() *tools.Registry
	Governance() *aggov.Manager
	IsFinished() bool
	LastReason() string
	SetLastReason(string)
	Phase(string)
	StreamText(delta string)
	StreamReasoning(delta string)
	AddTokens(prompt, completion int)
}

type Runner struct {
	host Host
}

func New(host Host) *Runner {
	return &Runner{host: host}
}

func (r *Runner) Reason(input string) *reasoning.Result {
	r.host.Phase(common.PhaseThinking)
	if strings.HasPrefix(input, "/") && !strings.Contains(input, " ") {
		return reasoning.CommandResult()
	}

	toolCalls, textContent, err := r.callLLMForTool()
	return reasoning.FromLLM(toolCalls, textContent, err)
}

func (r *Runner) callLLMForTool() ([]tools.ToolCallItem, string, error) {
	toolDefs := tools.ToToolDefs(r.host.ToolRegistry().Descriptors())
	var items []tools.ToolCallItem
	var textContent string

	err := withRetry(r.host.Context(), func() error {
		messages := r.host.ContextManager().Build(true)
		messages = r.applyPreModelRequestHooks(messages)

		result, err := tools.CallLLM(r.host.LLM(), tools.LLMCallOptions{
			Ctx:       r.host.Context(),
			Messages:  messages,
			ToolDefs:  toolDefs,
			Callbacks: r.streamCallbacks(),
			CheckDone: r.host.IsFinished,
		})
		if err != nil {
			return err
		}

		textContent = result.Text
		if result.Reasoning != "" {
			r.host.SetLastReason(result.Reasoning)
		}
		if len(result.ToolCalls) == 0 {
			return nil
		}

		items = result.ToolCalls
		r.host.ContextManager().AddAssistantToolCall(textContent, r.host.LastReason(), tools.ToLLMToolCalls(items))
		return nil
	})

	return items, textContent, err
}

func (r *Runner) applyPreModelRequestHooks(messages []types.Message) []types.Message {
	gov := r.host.Governance()
	if gov == nil || gov.HookReg == nil {
		return messages
	}
	gov.HookReg.Set(hooks.StoreToolResultCount, countToolResults(messages))
	var hints []hooks.Hint
	for _, result := range gov.HookReg.Evaluate(hooks.PreModelRequest, "", false) {
		if result.Hint != nil {
			hints = append(hints, *result.Hint)
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

func (r *Runner) streamCallbacks() tools.StreamCallbacks {
	return tools.StreamCallbacks{
		OnText: func(delta string) {
			r.host.StreamText(delta)
		},
		OnReasoning: func(delta string) {
			r.host.StreamReasoning(delta)
		},
		OnPhase: func(phase string) {
			r.host.Phase(phase)
		},
		AddTokens: func(prompt, completion int) {
			r.host.AddTokens(prompt, completion)
		},
		RecordUsage: func(prompt, completion int) {
			r.host.ContextManager().RecordUsage(prompt, completion)
		},
		RecordCache: func(hit, miss int) {
			r.host.ContextManager().RecordCache(hit, miss)
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
