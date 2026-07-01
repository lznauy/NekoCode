package model

import (
	"context"
	"strings"
	"time"

	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/common/debug"
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

func CallLLMWithRetry(ctx context.Context, client types.LLM, buildOptions func() tools.LLMCallOptions) (*tools.LLMCallResult, error) {
	var result *tools.LLMCallResult
	err := WithRetry(ctx, func() error {
		var err error
		result, err = tools.CallLLM(client, buildOptions())
		return err
	})
	return result, err
}

func (r *Runner) Reason(input string) *Result {
	r.host.Phase(common.PhaseThinking)
	if strings.HasPrefix(input, "/") && !strings.Contains(input, " ") {
		return CommandResult()
	}

	toolCalls, textContent, err := r.callLLMForTool()
	return FromLLM(toolCalls, textContent, err)
}

func (r *Runner) callLLMForTool() ([]tools.ToolCallItem, string, error) {
	toolDefs := tools.ToToolDefs(r.host.ToolRegistry().Descriptors())
	messages := r.host.ContextManager().Build()
	messages = r.applyPreModelRequestHooks(messages)

	result, err := CallLLMWithRetry(r.host.Context(), r.host.LLM(), func() tools.LLMCallOptions {
		return tools.LLMCallOptions{
			Ctx:       r.host.Context(),
			Messages:  messages,
			ToolDefs:  toolDefs,
			Callbacks: r.streamCallbacks(),
			CheckDone: r.host.IsFinished,
		}
	})
	if err != nil {
		return nil, "", err
	}

	textContent := result.Text
	if result.Reasoning != "" {
		r.host.SetLastReason(result.Reasoning)
	}
	if len(result.ToolCalls) > 0 {
		r.host.ContextManager().AddAssistantToolCall(textContent, r.host.LastReason(), tools.ToLLMToolCalls(result.ToolCalls))
	}
	return result.ToolCalls, textContent, nil
}

const synthesizePrompt = "Based on the information collected above, provide a final answer. Do NOT call any more tools. Output your conclusion directly."
const fallbackSynthesize = "Unable to produce a final summary — the model is currently unavailable. The task's tool operations may have completed, but the results could not be synthesized. Please try again or check the conversation log for details."

func (r *Runner) Synthesize() string {
	output := r.forceSynthesize()
	r.host.ContextManager().AddAssistantResponse(output, "")
	return output
}

func (r *Runner) forceSynthesize() string {
	var text string
	_ = WithRetry(r.host.Context(), func() error {
		result, err := r.streamSynthesize(r.host.Context())
		if err != nil {
			return err
		}
		text = result
		return nil
	})
	if text != "" && !IsGarbledToolCall(text) {
		return text
	}

	debug.Log("forceSynthesize: primary path failed, attempting emergency fallback")
	r.host.ContextManager().AutoCompactIfNeeded()
	ctx, cancel := context.WithTimeout(r.host.Context(), 30*time.Second)
	defer cancel()
	if fb, _ := r.streamSynthesize(ctx); fb != "" && !IsGarbledToolCall(fb) {
		return fb
	}

	return fallbackSynthesize
}

func (r *Runner) streamSynthesize(ctx context.Context) (string, error) {
	messages := r.host.ContextManager().Build()
	messages = append(messages, types.Message{Role: "user", Content: synthesizePrompt})

	result, err := tools.CallLLM(r.host.LLM(), tools.LLMCallOptions{
		Ctx:       ctx,
		Messages:  messages,
		Callbacks: r.streamCallbacks(),
	})
	if err != nil {
		return "", err
	}
	return result.Text, nil
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

func WithRetry(ctx context.Context, fn func() error) error {
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
