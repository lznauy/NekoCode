// streaming.go — shared stream consumption for main agent and subagent.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"nekocode/bot/ctxmgr/token"
	"nekocode/common"
	"nekocode/llm/types"
)

// StreamCallbacks holds per-token callbacks for ConsumeStream.
// Both the main agent and subagent provide their own implementations.
type StreamCallbacks struct {
	OnText      func(delta string)
	OnReasoning func(delta string)
	OnPhase     func(phase string)
	AddTokens   func(prompt, completion int)
	RecordUsage func(prompt, completion int) // called with actual API-reported usage
	RecordCache func(hit, miss int)          // called with cache token stats
}

// StreamResult accumulates the output of consuming a ChatStream.
type StreamResult struct {
	TextBuf      strings.Builder
	ReasoningBuf strings.Builder
	TcAccum      map[int]*ToolAccum
	LastUsage    *types.StreamUsage // last API-reported usage (for subagent correction)
}

// ToolAccum accumulates incremental tool call deltas for a single tool call.
type ToolAccum struct {
	ID   string
	Name string
	Args strings.Builder
}

// ConsumeStream reads tokens from tokenCh and populates s.
// checkDone is an optional abort signal — when it returns true the stream is
// drained and ConsumeStream returns early. Shared by the main agent and subagent.
func ConsumeStream(tokenCh <-chan types.StreamToken, s *StreamResult, cb StreamCallbacks, checkDone func() bool) {
	firstContent := true
	firstReasoning := true
	for token := range tokenCh {
		if checkDone != nil && checkDone() {
			go func() { for range tokenCh {} }() // drain
			return
		}
		if token.ReasoningContent != "" && firstReasoning {
			firstReasoning = false
			if cb.OnPhase != nil {
				cb.OnPhase(common.PhaseThinking)
			}
		}
		if token.Content != "" {
			if firstContent {
				firstContent = false
				if cb.OnPhase != nil {
					cb.OnPhase(common.PhaseReasoning)
				}
			}
			s.TextBuf.WriteString(token.Content)
			if cb.OnText != nil {
				cb.OnText(token.Content)
			}
			if cb.AddTokens != nil {
				cb.AddTokens(0, 1)
			}
		}
		if token.ReasoningContent != "" {
			s.ReasoningBuf.WriteString(token.ReasoningContent)
			if cb.OnReasoning != nil {
				cb.OnReasoning(token.ReasoningContent)
			}
			if cb.AddTokens != nil {
				cb.AddTokens(0, 1)
			}
		}
		if token.Usage != nil {
			s.LastUsage = token.Usage
			if token.Usage.PromptTokens > 0 || token.Usage.CompletionTokens > 0 {
				if cb.RecordUsage != nil {
					cb.RecordUsage(token.Usage.PromptTokens, token.Usage.CompletionTokens)
				}
			}
			if token.Usage.CacheHitTokens > 0 || token.Usage.CacheMissTokens > 0 {
				if cb.RecordCache != nil {
					cb.RecordCache(token.Usage.CacheHitTokens, token.Usage.CacheMissTokens)
				}
			}
		}
		if token.ToolCallDelta != nil {
			if firstContent {
				firstContent = false
				if cb.OnPhase != nil {
					cb.OnPhase(common.PhaseReasoning)
				}
			}
			if s.TcAccum == nil {
				s.TcAccum = make(map[int]*ToolAccum)
			}
			idx := token.ToolCallDelta.Index
			acc := s.TcAccum[idx]
			if acc == nil {
				acc = &ToolAccum{}
				s.TcAccum[idx] = acc
			}
			if token.ToolCallDelta.ID != "" {
				acc.ID = token.ToolCallDelta.ID
			}
			if token.ToolCallDelta.Name != "" {
				acc.Name = token.ToolCallDelta.Name
			}
			acc.Args.WriteString(token.ToolCallDelta.Arguments)
			if cb.AddTokens != nil {
				cb.AddTokens(0, 1)
			}
		}
	}
}

// CollectToolCalls converts accumulated tool call deltas into ToolCallItems.
// Iterates in sorted index order to preserve LLM-intended tool call sequence.
func (s *StreamResult) CollectToolCalls() []ToolCallItem {
	if len(s.TcAccum) == 0 {
		return nil
	}
	// Collect and sort indices so tool calls are returned in the order
	// the LLM emitted them. Map iteration is non-deterministic in Go.
	indices := make([]int, 0, len(s.TcAccum))
	for idx := range s.TcAccum {
		indices = append(indices, idx)
	}
	sort.Ints(indices)

	var items []ToolCallItem
	for _, idx := range indices {
		acc := s.TcAccum[idx]
		if acc == nil {
			continue
		}
		var args map[string]any
		if err := json.Unmarshal([]byte(acc.Args.String()), &args); err != nil {
			continue
		}
		items = append(items, ToolCallItem{ID: acc.ID, Name: acc.Name, Args: args})
	}
	return items
}

// ToLLMToolCalls converts ToolCallItems to the LLM types.ToolCall format.
func ToLLMToolCalls(calls []ToolCallItem) []types.ToolCall {
	out := make([]types.ToolCall, len(calls))
	for i, c := range calls {
		args, _ := json.Marshal(c.Args)
		out[i] = types.ToolCall{
			ID:       c.ID,
			Type:     "function",
			Function: types.FunctionCall{Name: c.Name, Arguments: string(args)},
		}
	}
	return out
}

// LLMCallResult holds the result of a single LLM stream call.
type LLMCallResult struct {
	Text      string
	Reasoning string
	ToolCalls []ToolCallItem
}

// LLMCallOptions configures a single LLM call.
type LLMCallOptions struct {
	Ctx            context.Context
	Messages       []types.Message
	ToolDefs       []types.ToolDef
	Callbacks      StreamCallbacks
	CheckDone      func() bool // returns true if stream should be aborted (e.g. user interrupt)
	EstimatePrompt bool        // whether to estimate and report prompt tokens via Callbacks.AddTokens
}

// CallLLM executes a single LLM stream call and returns the result.
// It handles ChatStream, ConsumeStream, errCh drain, and text/tool-call extraction.
// Retry logic is the caller's responsibility (withRetry or llm.Retry).
func CallLLM(client types.LLM, opts LLMCallOptions) (*LLMCallResult, error) {
	tokenCh, errCh := client.ChatStream(opts.Ctx, opts.Messages, opts.ToolDefs)
	if tokenCh == nil {
		select {
		case err := <-errCh:
			return nil, err
		default:
			return nil, fmt.Errorf("chat stream failed")
		}
	}

	if opts.EstimatePrompt {
		est := token.EstimateTokens(opts.Messages)
		if opts.Callbacks.AddTokens != nil {
			opts.Callbacks.AddTokens(est, 0)
		}
	}

	stream := StreamResult{}
	ConsumeStream(tokenCh, &stream, opts.Callbacks, opts.CheckDone)

	if opts.CheckDone != nil && opts.CheckDone() {
		// Drain errCh to prevent goroutine leak. The producer goroutine
		// closes tokenCh then sends to errCh — we must consume it so the
		// producer doesn't block on an unbuffered send.
		go func() { <-errCh }()
		return nil, context.Canceled
	}

	// Blocking read: tokenCh is closed so the producer goroutine has either
	// already sent its error or will send nil shortly. A non-blocking read
	// risks dropping errors that arrive slightly after the stream ends.
	if err := <-errCh; err != nil {
		return nil, err
	}

	return &LLMCallResult{
		Text:      StripAnsi(stream.TextBuf.String()),
		Reasoning: stream.ReasoningBuf.String(),
		ToolCalls: stream.CollectToolCalls(),
	}, nil
}
