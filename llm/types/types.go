package types

import (
	"context"
	"net/http"
	"sync"
	"time"

	"nekocode/common"
)

var SharedHTTPClientTimeout = &http.Client{
	Transport: common.SharedTransport,
	Timeout:   120 * time.Second,
}

// SharedHTTPStreamClient is used for streaming requests. The timeout is intentionally
// long (10 min) — streaming responses can take minutes of token generation.
var SharedHTTPStreamClient = &http.Client{
	Transport: common.SharedTransport,
	Timeout:   10 * time.Minute,
}

type Message struct {
	Role             string     `json:"role"`
	Content          string     `json:"content,omitempty"`
	ReasoningContent string     `json:"reasoning_content,omitempty"`
	Name             string     `json:"name,omitempty"`
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string     `json:"tool_call_id,omitempty"`
	Source           string     `json:"source,omitempty"` // internal: "user" | "hint" | "" (legacy); stripped before API call
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
	CacheHitTokens   int
	CacheMissTokens  int
	// OpenAI standard: prompt_tokens_details.cached_tokens.
	PromptTokensDetails *struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"prompt_tokens_details,omitempty"`
}

// Normalize extracts cache fields from protocol-specific usage formats.
func (u *StreamUsage) Normalize() {
	if u.PromptTokensDetails != nil && u.PromptTokensDetails.CachedTokens > 0 {
		u.CacheHitTokens = u.PromptTokensDetails.CachedTokens
	}
	if u.CacheHitTokens > 0 && u.PromptTokens > 0 {
		u.CacheMissTokens = max(0, u.PromptTokens-u.CacheHitTokens)
	}
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
	GetMaxTokens() int
	SetDisableThinking(disable bool)
	GetDisableThinking() bool
}

// BaseClient holds common fields and setters shared by all LLM client implementations.
// Embed this in concrete clients to avoid duplicating struct fields and trivial methods.
type BaseClient struct {
	APIKey          string
	BaseURL         string
	Model           string
	MaxTokens       int
	Temperature     float64
	DisableThinking bool
	thinkingMu      sync.RWMutex // protects DisableThinking (subagent engine mutates concurrently)
	maxTokensMu     sync.RWMutex // protects MaxTokens (merge/summarize mutates concurrently)
}

func (c *BaseClient) SetMaxTokens(n int) {
	c.maxTokensMu.Lock()
	c.MaxTokens = n
	c.maxTokensMu.Unlock()
}
func (c *BaseClient) GetMaxTokens() int {
	c.maxTokensMu.RLock()
	defer c.maxTokensMu.RUnlock()
	return c.MaxTokens
}
func (c *BaseClient) SetDisableThinking(d bool) {
	c.thinkingMu.Lock()
	c.DisableThinking = d
	c.thinkingMu.Unlock()
}
func (c *BaseClient) GetDisableThinking() bool {
	c.thinkingMu.RLock()
	defer c.thinkingMu.RUnlock()
	return c.DisableThinking
}
