package llmstream

import (
	"context"
	"strings"

	"nekocode/bot/llm/types"
	"nekocode/bot/tools/core"
)

// StreamCallbacks holds per-token callbacks for ConsumeStream.
type StreamCallbacks struct {
	OnText      func(delta string)
	OnReasoning func(delta string)
	OnPhase     func(phase string)
	AddTokens   func(prompt, completion int)
	RecordUsage func(prompt, completion int)
	RecordCache func(hit, miss int)
}

// StreamResult accumulates the output of consuming a ChatStream.
type StreamResult struct {
	TextBuf      strings.Builder
	ReasoningBuf strings.Builder
	TcAccum      map[int]*ToolAccum
	LastUsage    *types.StreamUsage
}

// ToolAccum accumulates incremental tool call deltas for a single tool call.
type ToolAccum struct {
	ID   string
	Name string
	Args strings.Builder
}

// LLMCallResult holds the result of a single LLM stream call.
type LLMCallResult struct {
	Text      string
	Reasoning string
	ToolCalls []core.ToolCallItem
}

// LLMCallOptions configures a single LLM call.
type LLMCallOptions struct {
	Ctx       context.Context
	Messages  []types.Message
	ToolDefs  []types.ToolDef
	Callbacks StreamCallbacks
	CheckDone func() bool
}
