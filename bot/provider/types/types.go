package types

import (
	nethttp "net/http"
	"os"
	"sync"
	"time"

	"nekocode/bot/reasoning"
	utilhttp "nekocode/util/http"
)

var SharedHTTPClientTimeout = &nethttp.Client{
	Transport: utilhttp.SharedTransport,
	Timeout:   120 * time.Second,
}

// SharedHTTPStreamClient is used for streaming requests. The timeout is intentionally
// long (10 min) — streaming responses can take minutes of token generation.
var SharedHTTPStreamClient = &nethttp.Client{
	Transport: utilhttp.SharedTransport,
	Timeout:   10 * time.Minute,
}

type Message struct {
	Role               string         `json:"role"`
	Content            string         `json:"content,omitempty"`
	ReasoningContent   string         `json:"reasoning_content,omitempty"`
	ReasoningSignature string         `json:"reasoning_signature,omitempty"`
	Name               string         `json:"name,omitempty"`
	ToolCalls          []ToolCall     `json:"tool_calls,omitempty"`
	ToolCallID         string         `json:"tool_call_id,omitempty"`
	IsError            bool           `json:"is_error,omitempty"`
	Source             string         `json:"source,omitempty"` // internal routing metadata; provider wire structs omit it
	// Images carries native image input for user messages when the active
	// model supports vision. Only local file references are persisted; the
	// base64 encoding happens at provider wire-build time. Providers with
	// Vision disabled strip this field at the wire boundary, leaving the
	// placeholder text (and image_attachments envelope) as the only image
	// trace, so switching models never breaks a conversation.
	Images []MessageImage `json:"images,omitempty"`
}

// MessageImage references one image attached to a user message. Width and
// Height describe the stored file so token estimation never needs file IO.
type MessageImage struct {
	Path   string `json:"path"`
	MIME   string `json:"mime,omitempty"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
}

// DetectImageMIME falls back to header sniffing when a MessageImage carries
// no recorded MIME type (e.g. sessions written before metadata was tracked).
func DetectImageMIME(data []byte) string {
	switch {
	case len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		return "image/webp"
	case len(data) >= 3 && string(data[:3]) == "GIF":
		return "image/gif"
	case len(data) >= 2 && data[0] == 0xFF && data[1] == 0xD8:
		return "image/jpeg"
	default:
		return "image/png"
	}
}

// LoadImage reads one attachment's bytes and resolves its media type,
// preferring the recorded MIME and falling back to header sniffing. ok is
// false when the file cannot be read; callers skip such images with a log
// trail instead of failing the whole request.
func LoadImage(image MessageImage) (data []byte, mediaType string, ok bool) {
	data, err := os.ReadFile(image.Path)
	if err != nil {
		return nil, "", false
	}
	if mediaType = image.MIME; mediaType == "" {
		mediaType = DetectImageMIME(data)
	}
	return data, mediaType, true
}

const (
	// MessageSourceVolatileTail is kept for replaying sessions written by the
	// old layered context builder. New runtime context is appended as user
	// messages instead of transient system messages.
	MessageSourceVolatileTail = "volatile-tail"
	// MessageSourceRuntimeContext marks controller-generated tagged user
	// messages. Frontends hide these messages while providers retain them in
	// the append-only conversation prefix.
	MessageSourceRuntimeContext = "runtime-context"
	// MessageSourceHint marks transient controller guidance appended as its own
	// user message. Hint changes must not cause unrelated runtime state to be
	// emitted again.
	MessageSourceHint = "hint"
	// MessageSourceRuntimeEvent marks an immutable controller event that is
	// hidden by interaction surfaces but remains part of conversation history.
	MessageSourceRuntimeEvent = "runtime-event"
)

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

// Usage is shared by streaming and non-streaming responses so cache and
// reasoning details are decoded consistently on both request paths.
type Usage = StreamUsage

type Response struct {
	ID      string   `json:"id"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

type StreamToken struct {
	Content            string
	ReasoningContent   string
	ReasoningSignature string
	ToolCallDelta      *ToolCallDelta
	Usage              *StreamUsage
	FinishReason       string
	// Request is emitted once, as the first token of a stream: wire-level
	// facts about the request for the per-call evidence log.
	Request *RequestMeta
}

// RequestMeta describes the provider request a stream belongs to without
// retaining prompt or credential-bearing request bytes.
type RequestMeta struct {
	Model           string
	Protocol        string
	BaseURL         string
	RequestedEffort string
	EffectiveEffort string
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
	TotalTokens      int `json:"total_tokens"`
	ReasoningTokens  int `json:"reasoning_tokens,omitempty"`
	CacheHitTokens   int
	CacheMissTokens  int
	// CacheUsageReported distinguishes a real zero-hit/full-miss report from
	// providers that only return aggregate prompt tokens.
	CacheUsageReported bool `json:"-"`
	// SystemFingerprint identifies the serving cluster that produced this
	// response (e.g. DeepSeek's fp_*_kvcache_*). Comparing it across calls
	// shows whether cache misses correlate with landing on a different pool.
	SystemFingerprint string `json:"system_fingerprint,omitempty"`
	// OpenAI standard: prompt_tokens_details.cached_tokens.
	PromptTokensDetails *struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"prompt_tokens_details,omitempty"`
	// OpenAI-compatible reasoning models report the thinking subset of output
	// tokens here. ReasoningTokens remains zero when the provider omits it.
	CompletionTokensDetails *struct {
		ReasoningTokens int `json:"reasoning_tokens"`
	} `json:"completion_tokens_details,omitempty"`
	// DeepSeek also reports flat fields (kept for backward compatibility):
	// prompt_cache_hit_tokens / prompt_cache_miss_tokens.
	FlatCacheHit  *int `json:"prompt_cache_hit_tokens,omitempty"`
	FlatCacheMiss *int `json:"prompt_cache_miss_tokens,omitempty"`
}

// HasTokens reports whether a provider supplied any billable usage.
func (u StreamUsage) HasTokens() bool {
	return u.PromptTokens > 0 || u.CompletionTokens > 0
}

// Merge folds a partial provider report into u. Streaming protocols may split
// prompt/cache usage and completion usage across different events.
func (u *StreamUsage) Merge(next *StreamUsage) {
	if next == nil {
		return
	}
	if next.PromptTokens > 0 {
		u.PromptTokens = next.PromptTokens
	}
	if next.CompletionTokens > 0 {
		u.CompletionTokens = next.CompletionTokens
	}
	if next.ReasoningTokens > 0 {
		u.ReasoningTokens = next.ReasoningTokens
	}
	if next.CacheUsageReported {
		u.CacheUsageReported = true
		u.CacheHitTokens = next.CacheHitTokens
		u.CacheMissTokens = next.CacheMissTokens
	}
	if next.SystemFingerprint != "" {
		u.SystemFingerprint = next.SystemFingerprint
	}
	u.TotalTokens = u.PromptTokens + u.CompletionTokens
}

// Normalize extracts cache fields from protocol-specific usage formats.
// Provider-reported values win over derived ones: the OpenAI-standard
// nested field wins for hit, DeepSeek's flat fields fill any gap, and a
// reported miss is trusted over the prompt-hit arithmetic.
func (u *StreamUsage) Normalize() {
	reportedHit := u.CacheUsageReported || u.CacheHitTokens > 0 || u.CacheMissTokens > 0
	if u.PromptTokensDetails != nil {
		u.CacheHitTokens = u.PromptTokensDetails.CachedTokens
		reportedHit = true
	} else if u.FlatCacheHit != nil {
		u.CacheHitTokens = *u.FlatCacheHit
		reportedHit = true
	}
	switch {
	case u.FlatCacheMiss != nil:
		u.CacheMissTokens = *u.FlatCacheMiss
	case reportedHit && u.PromptTokens > 0:
		u.CacheMissTokens = max(0, u.PromptTokens-u.CacheHitTokens)
	}
	u.CacheUsageReported = reportedHit || u.FlatCacheMiss != nil
	if u.CompletionTokensDetails != nil {
		u.ReasoningTokens = u.CompletionTokensDetails.ReasoningTokens
	}
	u.PromptTokens = max(0, u.PromptTokens)
	u.CompletionTokens = max(0, u.CompletionTokens)
	u.TotalTokens = u.PromptTokens + u.CompletionTokens
	u.ReasoningTokens = max(0, u.ReasoningTokens)
	if u.CompletionTokens > 0 {
		u.ReasoningTokens = min(u.ReasoningTokens, u.CompletionTokens)
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
	Type        string              `json:"type"`
	Description string              `json:"description,omitempty"`
	Enum        []string            `json:"enum,omitempty"`
	Items       *Property           `json:"items,omitempty"`
	Properties  map[string]Property `json:"properties,omitempty"`
	Required    []string            `json:"required,omitempty"`
}

// BaseClient holds common fields and setters shared by all LLM client implementations.
// Embed this in concrete clients to avoid duplicating struct fields and trivial methods.
type BaseClient struct {
	APIKey      string
	BaseURL     string
	Model       string
	MaxTokens   int
	Temperature float64
	// Vision enables native image input on the wire. Compaction/flash
	// clients keep it false so image-bearing history is always safe to send.
	Vision bool

	reasoning   ReasoningSettings
	reasoningMu sync.RWMutex // protects reasoning configuration mutated by subagent setup
	maxTokensMu sync.RWMutex // protects MaxTokens (merge/summarize mutates concurrently)
}

// SetVision toggles native image input for this client.
func (b *BaseClient) SetVision(vision bool) {
	b.Vision = vision
}

type ReasoningSettings = reasoning.Settings

// ReasoningForRequest returns the provider-issued reasoning and whether its
// wire field/block must be present for the active model contract. A required
// tool-call field may intentionally contain an empty string.
func ReasoningForRequest(message Message, settings ReasoningSettings) (string, bool) {
	if message.Role != "assistant" {
		return "", false
	}
	switch settings.Replay {
	case reasoning.ReplayToolCalls:
		return message.ReasoningContent, len(message.ToolCalls) > 0
	case reasoning.ReplaySigned:
		return message.ReasoningContent, message.ReasoningSignature != ""
	default:
		return "", false
	}
}

// ProjectReasoning returns the model-visible conversation projection while
// leaving the durable session messages unchanged.
func ProjectReasoning(messages []Message, settings ReasoningSettings) []Message {
	var projected []Message
	for i, message := range messages {
		if _, replay := ReasoningForRequest(message, settings); replay ||
			(message.ReasoningContent == "" && message.ReasoningSignature == "") {
			if projected != nil {
				projected = append(projected, message)
			}
			continue
		}
		if projected == nil {
			projected = append([]Message(nil), messages[:i]...)
		}
		message.ReasoningContent = ""
		message.ReasoningSignature = ""
		projected = append(projected, message)
	}
	if projected == nil {
		return messages
	}
	return projected
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
	c.reasoningMu.Lock()
	c.reasoning.Disabled = d
	c.reasoningMu.Unlock()
}
func (c *BaseClient) GetDisableThinking() bool {
	return c.ReasoningSettings().Disabled
}
func (c *BaseClient) SetReasoningSettings(settings ReasoningSettings) {
	c.reasoningMu.Lock()
	c.reasoning = settings
	c.reasoningMu.Unlock()
}
func (c *BaseClient) ReasoningSettings() ReasoningSettings {
	c.reasoningMu.RLock()
	defer c.reasoningMu.RUnlock()
	settings := c.reasoning
	settings.Disabled = settings.Disabled || settings.Effort == "none"
	return settings
}
