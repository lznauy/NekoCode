package provider

import (
	"context"
	"fmt"

	"nekocode/bot/provider/anthropic"
	"nekocode/bot/provider/openai"
	"nekocode/bot/provider/types"
)

// LLM is the contract every concrete provider must satisfy. Both
// anthropic.Client and openai.Client implement this interface (verified by
// the compile-time checks below).
type LLM interface {
	Chat(ctx context.Context, messages []types.Message, tools []types.ToolDef) (*types.Response, error)
	ChatStream(ctx context.Context, messages []types.Message, tools []types.ToolDef) (<-chan types.StreamToken, <-chan error)
}

// RequestClient accepts per-request overrides without mutating client defaults.
// Existing third-party implementations of LLM need no new methods.
type RequestClient interface {
	LLM
	ChatWithOptions(context.Context, []types.Message, []types.ToolDef, types.RequestOptions) (*types.Response, error)
	ChatStreamWithOptions(context.Context, []types.Message, []types.ToolDef, types.RequestOptions) (<-chan types.StreamToken, <-chan error)
}

func SupportsRequestOptions(client LLM) bool {
	_, ok := client.(RequestClient)
	return ok
}

// ChatStream applies overrides only when supported. Never silently ignore a
// requested override or temporarily mutate a legacy provider's global state.
func ChatStream(client LLM, ctx context.Context, messages []types.Message, tools []types.ToolDef, options types.RequestOptions) (<-chan types.StreamToken, <-chan error) {
	if scoped, ok := client.(RequestClient); ok {
		return scoped.ChatStreamWithOptions(ctx, messages, tools, options)
	}
	if options == (types.RequestOptions{}) {
		return client.ChatStream(ctx, messages, tools)
	}
	tokens, errs := make(chan types.StreamToken), make(chan error, 1)
	close(tokens)
	errs <- fmt.Errorf("provider does not support per-request options")
	close(errs)
	return tokens, errs
}

// ReasoningSettingsFor returns an optional provider's model contract. Test and
// third-party providers that do not expose one retain the safe no-replay
// default.
func ReasoningSettingsFor(llm LLM) types.ReasoningSettings {
	configured, ok := llm.(interface {
		ReasoningSettings() types.ReasoningSettings
	})
	if !ok {
		return types.ReasoningSettings{}
	}
	return configured.ReasoningSettings()
}

// Compile-time checks: both concrete clients must satisfy LLM.
var (
	_ LLM = (*anthropic.Client)(nil)
	_ LLM = (*openai.Client)(nil)
)

// Config identifies one model endpoint.
type Config struct {
	APIKey    string
	BaseURL   string
	Model     string
	Protocol  string
	Reasoning types.ReasoningSettings
	// Vision enables native image input: user-message Images are encoded as
	// image content parts. When false, the wire layer strips Images and the
	// placeholder text plus image_attachments envelope remain the only image
	// trace, so a non-vision model never receives a request it cannot parse.
	// Flash/compaction clients must always pass false.
	Vision    bool
	MaxTokens int
}

// New creates an LLM client. Protocol may be "openai" or "anthropic".
func New(config Config) LLM {
	switch config.Protocol {
	case "anthropic":
		client := anthropic.New(config.APIKey, config.BaseURL, config.Model)
		client.SetReasoningSettings(config.Reasoning)
		client.SetVision(config.Vision)
		if config.MaxTokens > 0 {
			client.SetMaxTokens(config.MaxTokens)
		}
		return client
	default:
		client := openai.New(config.APIKey, config.BaseURL, config.Model)
		client.SetReasoningSettings(config.Reasoning)
		client.SetVision(config.Vision)
		if config.MaxTokens > 0 {
			client.SetMaxTokens(config.MaxTokens)
		}
		return client
	}
}
