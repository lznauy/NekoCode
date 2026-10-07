package provider

import (
	"context"

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
	SetMaxTokens(n int)
	GetMaxTokens() int
	SetDisableThinking(disable bool)
	GetDisableThinking() bool
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
	Vision bool
}

// New creates an LLM client. Protocol may be "openai" or "anthropic".
func New(config Config) LLM {
	switch config.Protocol {
	case "anthropic":
		client := anthropic.New(config.APIKey, config.BaseURL, config.Model)
		client.SetReasoningSettings(config.Reasoning)
		client.SetVision(config.Vision)
		return client
	default:
		client := openai.New(config.APIKey, config.BaseURL, config.Model)
		client.SetReasoningSettings(config.Reasoning)
		client.SetVision(config.Vision)
		return client
	}
}
