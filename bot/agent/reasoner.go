package agent

import (
	"nekocode/bot/debug"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"nekocode/bot/tools"
	"nekocode/llm/types"
	"nekocode/llm"

	"nekocode/common"
)

type ActionType int

const (
	ActionChat ActionType = iota
	ActionExecuteTool
)

const (
	synthesizePrompt = "Based on the information collected above, provide a final answer. Do NOT call any more tools. Output your conclusion directly."
)

type ReasoningResult struct {
	Thought         string
	Action          ActionType
	ActionInput     string
	ToolCalls       []tools.ToolCallItem
	TextContent     string
	Interrupted     bool
	GarbledToolCall bool
	IsError         bool // set when the LLM call itself failed (not a text response)
}

func (a *Agent) Reason(state *stepState) *ReasoningResult {
	if a.phase != nil {
		a.phase(common.PhaseThinking)
	}
	if strings.HasPrefix(state.Input, "/") && !strings.Contains(state.Input, " ") {
		return &ReasoningResult{Thought: "User entered a command", Action: ActionChat}
	}

	toolCalls, textContent, err := a.callLLMForTool()
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return &ReasoningResult{Thought: "User interrupted", Action: ActionChat, Interrupted: true}
		}
		if textContent != "" && !isGarbledToolCall(textContent) {
			return &ReasoningResult{Thought: "Truncated reply", Action: ActionChat, ActionInput: textContent}
		}
		return &ReasoningResult{Thought: "LLM call failed", Action: ActionChat, ActionInput: fmt.Sprintf("LLM call failed: %v", err), IsError: true}
	}

	if len(toolCalls) == 0 {
		if isGarbledToolCall(textContent) {
			debug.Log("GarbledToolCall: XML leaked (len=%d)", len(textContent))
			return &ReasoningResult{Thought: "Format correction", Action: ActionChat, GarbledToolCall: true}
		}
		if textContent == "" {
			textContent = "Sorry, I couldn't determine what to do"
		}
		return &ReasoningResult{Thought: "Direct reply", Action: ActionChat, ActionInput: textContent}
	}

	if len(toolCalls) == 1 {
		tc := toolCalls[0]
		return &ReasoningResult{
			Thought: "Call tool: " + tc.Name, Action: ActionExecuteTool,
			ActionInput: tc.Name + ":" + tools.FormatArgs(tc.Args),
			ToolCalls:   toolCalls, TextContent: textContent,
		}
	}

	var names []string
	for _, tc := range toolCalls {
		names = append(names, tc.Name)
	}
	return &ReasoningResult{
		Thought: "Parallel tool calls: " + strings.Join(names, ", "),
		Action:  ActionExecuteTool, ToolCalls: toolCalls, TextContent: textContent,
	}
}

func (a ActionType) String() string {
	switch a {
	case ActionChat:
		return "chat"
	case ActionExecuteTool:
		return "execute_tool"
	default:
		return "unknown"
	}
}

func (a *Agent) callLLMForTool() ([]tools.ToolCallItem, string, error) {
	toolDefs := tools.ToToolDefs(a.toolRegistry.Descriptors())
	var items []tools.ToolCallItem
	var textContent string

	err := withRetry(a.getCtx(), func() error {
		messages := a.ctxMgr.Build(true)
		if a.transform != nil {
			messages = a.transform(messages)
		}

		result, err := tools.CallLLM(a.llmClient, tools.LLMCallOptions{
			Ctx:            a.getCtx(),
			Messages:       messages,
			ToolDefs:       toolDefs,
			Callbacks:      a.streamCallbacks(),
			CheckDone:      func() bool { return a.finished.Load() },
			EstimatePrompt: true,
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

// -- synthesize --

func (a *Agent) forceSynthesize() string {
	if text := a.trySynthesize(); text != "" {
		return text
	}
	debug.Log("forceSynthesize: primary path failed, attempting emergency fallback")
	if fb := a.emergencySynthesize(); fb != "" {
		return fb
	}
	return "Task completed but the model was unable to produce a final summary."
}

func (a *Agent) trySynthesize() string {
	var text string

	err := withRetry(a.getCtx(), func() error {
		result, err := a.streamSynthesize(a.getCtx())
		if err != nil {
			return err
		}
		text = result
		return nil
	})

	if err != nil && errors.Is(err, context.Canceled) {
		return ""
	}
	return text
}

func (a *Agent) emergencySynthesize() string {
	a.ctxMgr.AutoCompactIfNeeded()

	ctx, cancel := context.WithTimeout(a.getCtx(), 30*time.Second)
	defer cancel()

	text, _ := a.streamSynthesize(ctx)
	if isGarbledToolCall(text) {
		return ""
	}
	return text
}

// streamSynthesize executes a single synthesis LLM call (no tools).
// Returns the synthesized text. Used by both trySynthesize and emergencySynthesize.
func (a *Agent) streamSynthesize(ctx context.Context) (string, error) {
	messages := a.ctxMgr.Build(false)
	messages = append(messages, types.Message{Role: "user", Content: synthesizePrompt})

	result, err := tools.CallLLM(a.llmClient, tools.LLMCallOptions{
		Ctx:            ctx,
		Messages:       messages,
		Callbacks:      a.streamCallbacks(),
		CheckDone:      func() bool { return a.finished.Load() },
		EstimatePrompt: true,
	})
	if err != nil {
		return "", err
	}
	return result.Text, nil
}

// -- helpers --

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

// withRetry executes fn with exponential backoff, logging retries to the debug log.
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
