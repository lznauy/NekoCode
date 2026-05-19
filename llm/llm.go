package llm

import (
	"context"
	"net/http"
	"time"
)

var sharedTransport = &http.Transport{
	MaxIdleConns:        20,
	IdleConnTimeout:     90 * time.Second,
	DisableCompression:  false,
}

var SharedHTTPClientTimeout = &http.Client{
	Transport: sharedTransport,
	Timeout:   120 * time.Second,
}

// SharedHTTPStreamClient is used for streaming requests. The timeout is intentionally
// long (10 min) — streaming responses can take minutes of token generation.
// The client-level timeout provides a safety net against server-side hangs
// that would otherwise cause goroutine leaks (the caller's context may not have a deadline).
var SharedHTTPStreamClient = &http.Client{
	Transport: sharedTransport,
	Timeout:   10 * time.Minute,
}

type Message struct {
	Role             string     `json:"role"`
	Content          string     `json:"content,omitempty"`
	ReasoningContent string     `json:"reasoning_content,omitempty"`
	Name             string     `json:"name,omitempty"`
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string     `json:"tool_call_id,omitempty"`
}

type ToolCall struct {
	Index    int          `json:"index"`
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type Choice struct {
	Message      Message `json:"message"`
	Delta        Delta   `json:"delta"`
	FinishReason string  `json:"finish_reason"`
}

type Delta struct {
	Content          string     `json:"content"`
	ReasoningContent string     `json:"reasoning_content"`
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type Response struct {
	ID      string   `json:"id"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

type StreamChunk struct {
	Choices []struct {
		Delta        Delta  `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *StreamUsage `json:"usage"`
}

type StreamToken struct {
	Content          string
	ReasoningContent string
	ToolCallDelta    *ToolCallDelta // non-nil when streaming a tool call fragment
	Usage            *StreamUsage   // final chunk carries usage
	FinishReason     string         // "stop", "length", "tool_calls", etc.
}

type ToolCallDelta struct {
	Index     int    // which tool call (0-based)
	ID        string // set on first fragment
	Name      string // function name, set on first fragment
	Arguments string // JSON fragment, accumulated across chunks
}

type StreamUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	// Anthropic prompt cache
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	// DeepSeek disk KV cache (enabled by default, no config needed)
	CacheHitTokens  int `json:"prompt_cache_hit_tokens"`
	CacheMissTokens int `json:"prompt_cache_miss_tokens"`
}

type ToolDef struct {
	Type     string      `json:"type"`
	Function FunctionDef `json:"function"`
}

type FunctionDef struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Parameters  Parameters `json:"parameters"`
}

type Parameters struct {
	Type       string              `json:"type"`
	Properties map[string]Property `json:"properties"`
	Required   []string            `json:"required,omitempty"`
}

type Property struct {
	Type        string   `json:"type"`
	Description string   `json:"description,omitempty"`
	Enum        []string `json:"enum,omitempty"`
}

type LLM interface {
	Chat(ctx context.Context, messages []Message, tools []ToolDef) (*Response, error)
	ChatStream(ctx context.Context, messages []Message, tools []ToolDef) (<-chan StreamToken, <-chan error)
	SetAPIKey(apiKey string)
	SetBaseURL(url string)
	SetMaxTokens(n int)
	MaxTokens() int
	SetDisableThinking(disable bool)
	SetThinkingBudget(tokens int)     // 0 = use default, -1 = disabled
	SetReasoningEffort(effort string) // "high"/"max" (DeepSeek/openai-compat)
}

