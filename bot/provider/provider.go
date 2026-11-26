// Package provider constructs the built-in model clients.
package provider

import (
	"context"

	"nekocode/bot/provider/anthropic"
	"nekocode/bot/provider/llm"
	"nekocode/bot/provider/openai"
	"nekocode/bot/provider/types"
)

// LLM is the streaming model contract accepted by the harness.
type LLM = llm.Client

// RequestClient supports per-request overrides on a shared model client.
type RequestClient = llm.RequestClient

func SupportsRequestOptions(client LLM) bool { return llm.SupportsRequestOptions(client) }

// ChatStream applies per-request overrides when the client supports them.
func ChatStream(client LLM, ctx context.Context, messages []types.Message, tools []types.ToolDef, options types.RequestOptions) (<-chan types.StreamToken, <-chan error) {
	return llm.ChatStream(client, ctx, messages, tools, options)
}

// ReasoningSettingsFor returns the optional reasoning configuration of a client.
func ReasoningSettingsFor(client LLM) types.ReasoningSettings {
	return llm.ReasoningSettingsFor(client)
}

// MaxOutputTokensFor returns the configured output limit, or zero if unavailable.
func MaxOutputTokensFor(client LLM) int { return llm.MaxOutputTokensFor(client) }

// The built-in clients support the full per-request contract.
var (
	_ RequestClient = (*anthropic.Client)(nil)
	_ RequestClient = (*openai.Client)(nil)
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
