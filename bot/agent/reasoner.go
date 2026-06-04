package agent

import (
	"nekocode/bot/debug"
	"context"
	"encoding/json"
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
}

func (a *Agent) Reason(state *stepState) *ReasoningResult {
	a.drainSteering()
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
		return &ReasoningResult{Thought: "LLM call failed", Action: ActionChat, ActionInput: fmt.Sprintf("调用失败: %v", err)}
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
			ActionInput: tc.Name + ":" + formatArgs(tc.Args),
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

		tokenCh, errCh := a.llmClient.ChatStream(a.getCtx(), messages, toolDefs)
		if tokenCh == nil {
			if err, ok := <-errCh; ok {
				return err
			}
			return fmt.Errorf("chat stream failed")
		}

		a.AddTokens(estimatePrompt(messages), 0)

		stream := streamResult{}
		a.consumeStream(tokenCh, &stream)
	if a.finished {
		return context.Canceled
	}

		if ctxErr := a.getCtx().Err(); ctxErr != nil {
			return ctxErr
		}
		select {
		case err := <-errCh:
			if err != nil {
				return err
			}
		default:
		}

		textContent = tools.StripAnsi(stream.textBuf.String())
		if stream.reasoningBuf.Len() > 0 {
			a.lastReason = stream.reasoningBuf.String()
		}
		if len(stream.tcAccum) == 0 {
			return nil
		}

		items = stream.toolCalls()
		a.ctxMgr.AddAssistantToolCall(textContent, a.lastReason, stream.llmToolCalls())
		return nil
	})

	return items, textContent, err
}

// streamResult holds the output of consuming a ChatStream.
type streamResult struct {
	textBuf      strings.Builder
	reasoningBuf strings.Builder
	tcAccum      map[int]*toolAccum
}

func (a *Agent) consumeStream(tokenCh <-chan types.StreamToken, s *streamResult) {
	firstContent := true
	firstReasoning := true
	for token := range tokenCh {
		if a.finished {
			go func() { for range tokenCh {} }() // drain
			return
		}
		if token.ReasoningContent != "" && firstReasoning {
			firstReasoning = false
			if a.phase != nil {
				a.phase(common.PhaseThinking)
			}
		}
		if token.Content != "" {
			if firstContent {
				firstContent = false
				if a.phase != nil {
					a.phase(common.PhaseReasoning)
				}
			}
			s.textBuf.WriteString(token.Content)
			if a.textFn != nil {
				a.textFn(token.Content, false)
			}
			a.AddTokens(0, 1)
		}
		if token.ReasoningContent != "" {
			s.reasoningBuf.WriteString(token.ReasoningContent)
			if a.reasonFn != nil {
				a.reasonFn(token.ReasoningContent)
			}
			a.AddTokens(0, 1)
		}
		if token.Usage != nil {
			if token.Usage.PromptTokens > 0 || token.Usage.CompletionTokens > 0 {
				a.ctxMgr.RecordUsage(token.Usage.PromptTokens, token.Usage.CompletionTokens)
			}
			if token.Usage.CacheHitTokens > 0 || token.Usage.CacheMissTokens > 0 {
				a.ctxMgr.RecordCache(token.Usage.CacheHitTokens, token.Usage.CacheMissTokens)
			}
		}
		if token.ToolCallDelta != nil {
			if firstContent {
				firstContent = false
				if a.phase != nil {
					a.phase(common.PhaseReasoning)
				}
			}
			if s.tcAccum == nil {
				s.tcAccum = make(map[int]*toolAccum)
			}
			idx := token.ToolCallDelta.Index
			acc := s.tcAccum[idx]
			if acc == nil {
				acc = &toolAccum{}
				s.tcAccum[idx] = acc
			}
			if token.ToolCallDelta.ID != "" {
				acc.id = token.ToolCallDelta.ID
			}
			if token.ToolCallDelta.Name != "" {
				acc.name = token.ToolCallDelta.Name
			}
			acc.args.WriteString(token.ToolCallDelta.Arguments)
			a.AddTokens(0, 1)
		}
	}
}

func (s *streamResult) toolCalls() []tools.ToolCallItem {
	var items []tools.ToolCallItem
	for i := 0; i < len(s.tcAccum); i++ {
		acc := s.tcAccum[i]
		if acc == nil {
			continue
		}
		var args map[string]any
		if err := json.Unmarshal([]byte(acc.args.String()), &args); err != nil {
			continue
		}
		items = append(items, tools.ToolCallItem{ID: acc.id, Name: acc.name, Args: args})
	}
	return items
}

func (s *streamResult) llmToolCalls() []types.ToolCall {
	var calls []types.ToolCall
	for i := 0; i < len(s.tcAccum); i++ {
		acc := s.tcAccum[i]
		if acc == nil {
			continue
		}
		calls = append(calls, types.ToolCall{
			ID: acc.id, Type: "function",
			Function: types.FunctionCall{Name: acc.name, Arguments: acc.args.String()},
		})
	}
	return calls
}

type toolAccum struct {
	id   string
	name string
	args strings.Builder
}

func estimatePrompt(messages []types.Message) int {
	n := 0
	for _, m := range messages {
		n += len(m.Content) + len(m.Role)
	}
	return n / 4
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
		messages := a.ctxMgr.Build(false)
		messages = append(messages, types.Message{Role: "user", Content: synthesizePrompt})

		tokenCh, errCh := a.llmClient.ChatStream(a.getCtx(), messages, nil)
		if tokenCh == nil {
			if err, ok := <-errCh; ok {
				return err
			}
			return fmt.Errorf("chat stream failed")
		}

		a.AddTokens(estimatePrompt(messages), 0)

		stream := streamResult{}
		a.consumeStream(tokenCh, &stream)
	if a.finished {
		return context.Canceled
	}

		select {
		case err := <-errCh:
			if err != nil {
				return err
			}
		default:
		}
		text = tools.StripAnsi(stream.textBuf.String())
		return nil
	})

	if err != nil && !errors.Is(err, context.Canceled) && text != "" && !isGarbledToolCall(text) {
		return text
	}
	return text
}

func (a *Agent) emergencySynthesize() string {
	a.ctxMgr.AutoCompactIfNeeded()
	msgs := a.ctxMgr.Build(false)
	msgs = append(msgs, types.Message{Role: "user", Content: synthesizePrompt})

	ctx, cancel := context.WithTimeout(a.getCtx(), 30*time.Second)
	defer cancel()

	tokenCh, _ := a.llmClient.ChatStream(ctx, msgs, nil)
	if tokenCh == nil {
		return ""
	}
	stream := streamResult{}
	a.consumeStream(tokenCh, &stream)
	if a.finished {
		return ""
	}
	text := tools.StripAnsi(stream.textBuf.String())
	if isGarbledToolCall(text) {
		return ""
	}
	return text
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
