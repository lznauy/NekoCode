package types

import (
	"context"
	"net/http"
	"time"
)

var sharedTransport = &http.Transport{
	MaxIdleConns:       20,
	IdleConnTimeout:    90 * time.Second,
	DisableCompression: false,
}

var SharedHTTPClientTimeout = &http.Client{
	Transport: sharedTransport,
	Timeout:   120 * time.Second,
}

// SharedHTTPStreamClient is used for streaming requests. The timeout is intentionally
// long (10 min) — streaming responses can take minutes of token generation.
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
	FinishReason string  `json:"finish_reason"`
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

type StreamToken struct {
	Content          string
	ReasoningContent string
	ToolCallDelta    *ToolCallDelta
	Usage            *StreamUsage
	FinishReason     string
}

type ToolCallDelta struct {
	Index     int
	ID        string
	Name      string
	Arguments string
}

type StreamUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	CacheHitTokens   int `json:"prompt_cache_hit_tokens"`
	CacheMissTokens  int `json:"prompt_cache_miss_tokens"`
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
	SetMaxTokens(n int)
	SetDisableThinking(disable bool)
}
