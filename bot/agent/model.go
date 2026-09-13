package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"nekocode/bot/agent/internal/llmstream"
	"nekocode/bot/contextmgr"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/policy"
	"nekocode/bot/provider/types"
	"nekocode/logger"
	"nekocode/protocol"
)

const synthesizePrompt = "Based on the information collected above, provide a final answer. Do NOT call any more tools. Output your conclusion directly."

const fallbackSynthesize = "Unable to produce a final summary — the model is currently unavailable. The task's tool operations may have completed, but the results could not be synthesized. Please try again or check the conversation log for details."

// modelRunner wraps the LLM call for a turn: streaming, retry, response
// classification, and final-answer synthesis.
type modelRunner struct {
	agent *Agent
}

func newModelRunner(agent *Agent) *modelRunner {
	return &modelRunner{agent: agent}
}

func (r *modelRunner) reason(input string) *reasoningResult {
	a := r.agent
	a.stream.emitPhase(protocol.PhaseThinking)
	if strings.HasPrefix(input, "/") && !strings.Contains(input, " ") {
		return commandResult()
	}

	result, err := r.callLLMForTool()
	if err != nil {
		return fromLLM(nil, "", err)
	}
	reasoning := fromLLM(result.ToolCalls, result.Text, nil)
	reasoning.ReasoningContent = result.Reasoning
	reasoning.ReasoningSignature = result.ReasoningSignature
	return reasoning
}

func (r *modelRunner) callLLMForTool() (*llmstream.LLMCallResult, error) {
	a := r.agent
	toolDefs := core.ToToolDefs(a.deps.toolRegistry.Descriptors())
	policyHints := r.preModelHints()
	messages := a.deps.ctxMgr.BuildRequest(contextmgr.ModelRequest{Tools: toolDefs, PolicyHints: policyHints})

	result, err := llmstream.CallLLMWithRetry(a.getCtx(), a.deps.llmClient, func() llmstream.LLMCallOptions {
		return llmstream.LLMCallOptions{
			Ctx:         a.getCtx(),
			Messages:    messages,
			ToolDefs:    toolDefs,
			Callbacks:   r.streamCallbacks(),
			CheckDone:   a.life.Finished().Load,
			Source:      "main",
			SessionID:   a.sessionID(),
			Diagnostics: a.deps.ctxMgr.PrefixDiagnostics,
		}
	})
	if err != nil {
		return nil, err
	}

	if len(result.ToolCalls) > 0 {
		a.deps.ctxMgr.AddAssistant(types.Message{Role: "assistant", Content: result.Text,
			ReasoningContent: result.Reasoning, ReasoningSignature: result.ReasoningSignature,
			ToolCalls: llmstream.ToLLMToolCalls(result.ToolCalls)})
	}
	return result, nil
}

// synthesize produces a final answer when the run ended without one (step
// limit, repeated failures): it asks the LLM once for a tool-free summary
// and persists it. On failure or garbled output it falls back to a fixed
// message — no retries: this path fires precisely when the model is already
// misbehaving, so extra calls rarely help.
func (r *modelRunner) synthesize() string {
	a := r.agent
	text, err := r.streamSynthesize(a.getCtx())
	if err != nil || !usableFinalText(text) {
		logger.Log("synthesize: LLM summary failed (err=%v), using fallback", err)
		text = fallbackSynthesize
	}
	a.deps.ctxMgr.AddAssistant(types.Message{Role: "assistant", Content: text})
	return text
}

// usableFinalText reports whether text is a non-empty, non-garbled final answer.
func usableFinalText(text string) bool {
	return text != "" && !isGarbledToolCall(text)
}

func (r *modelRunner) streamSynthesize(ctx context.Context) (string, error) {
	a := r.agent
	messages := a.deps.ctxMgr.BuildRequest(contextmgr.ModelRequest{})
	messages = append(messages, types.Message{Role: "user", Content: synthesizePrompt})

	result, err := llmstream.CallLLM(a.deps.llmClient, llmstream.LLMCallOptions{
		Ctx:         ctx,
		Messages:    messages,
		Callbacks:   r.streamCallbacks(),
		Source:      "synthesize",
		SessionID:   a.sessionID(),
		Diagnostics: a.deps.ctxMgr.PrefixDiagnostics,
	})
	if err != nil {
		return "", err
	}
	return result.Text, nil
}

func (r *modelRunner) preModelHints() string {
	gov := r.agent.deps.gov
	if gov == nil {
		return ""
	}
	hints := collectHints(gov.BeforeModel())
	if len(hints) == 0 {
		return ""
	}
	return policy.FormatHints(hints)
}

func (r *modelRunner) streamCallbacks() llmstream.StreamCallbacks {
	a := r.agent
	return llmstream.StreamCallbacks{
		OnText: func(delta string) {
			a.stream.emitText(delta)
		},
		OnReasoning: func(delta string) {
			a.stream.emitReasoning(delta)
		},
		OnPhase: func(phase string) {
			a.stream.emitPhase(phase)
		},
		AddTokens: func(prompt, completion int) {
			a.AddCompletionTokens(completion)
		},
		OnUsage: func(usage types.StreamUsage) {
			a.deps.ctxMgr.RecordModelUsage(usage)
			a.RecordLLMUsage(usage)
		},
	}
}

const fallbackNoAction = "Sorry, I couldn't determine what to do"

type actionType int

const (
	actionChat actionType = iota
	actionExecuteTool
)

type reasoningResult struct {
	Thought            string
	Action             actionType
	ActionInput        string
	ToolCalls          []core.ToolCallItem
	TextContent        string
	ReasoningContent   string
	ReasoningSignature string
	Interrupted        bool
	GarbledToolCall    bool
	IsError            bool
}

func commandResult() *reasoningResult {
	return &reasoningResult{Thought: "User entered a command", Action: actionChat}
}

func fromLLM(toolCalls []core.ToolCallItem, textContent string, err error) *reasoningResult {
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return &reasoningResult{Thought: "User interrupted", Action: actionChat, Interrupted: true}
		}
		if textContent != "" && !isGarbledToolCall(textContent) {
			return &reasoningResult{Thought: "Truncated reply", Action: actionChat, ActionInput: textContent}
		}
		return &reasoningResult{Thought: "LLM call failed", Action: actionChat, ActionInput: fmt.Sprintf("LLM call failed: %v", err), IsError: true}
	}

	if len(toolCalls) == 0 {
		if isGarbledToolCall(textContent) {
			logger.Log("GarbledToolCall: XML leaked (len=%d)", len(textContent))
			return &reasoningResult{Thought: "Format correction", Action: actionChat, GarbledToolCall: true}
		}
		if textContent == "" {
			textContent = fallbackNoAction
		}
		return &reasoningResult{Thought: "Direct reply", Action: actionChat, ActionInput: textContent}
	}

	if len(toolCalls) == 1 {
		tc := toolCalls[0]
		return &reasoningResult{
			Thought:     "Call tool: " + tc.Name,
			Action:      actionExecuteTool,
			ActionInput: tc.Name + ":" + core.FormatArgs(tc.Args),
			ToolCalls:   toolCalls,
			TextContent: textContent,
		}
	}

	var names []string
	for _, tc := range toolCalls {
		names = append(names, tc.Name)
	}
	return &reasoningResult{
		Thought:     "Parallel tool calls: " + strings.Join(names, ", "),
		Action:      actionExecuteTool,
		ToolCalls:   toolCalls,
		TextContent: textContent,
	}
}

func isGarbledToolCall(text string) bool {
	t := strings.TrimSpace(text)
	if t == "" {
		return false
	}
	if strings.Contains(t, "<invoke") || strings.Contains(t, "</invoke") ||
		strings.Contains(t, "<parameter") || strings.Contains(t, "</parameter") ||
		strings.Contains(t, "<tool_call") || strings.Contains(t, "</tool_call") {
		return true
	}
	return strings.Contains(t, `"tool_calls"`) || strings.Contains(t, `"tool_use"`)
}
