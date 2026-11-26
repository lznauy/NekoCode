// Package llm defines the model client contract used by the harness.
package llm

import (
	"context"
	"fmt"

	"nekocode/bot/provider/types"
)

// Client is a model backend that supports single responses and streaming.
type Client interface {
	Chat(ctx context.Context, messages []types.Message, tools []types.ToolDef) (*types.Response, error)
	// ChatStream closes the token channel when production ends. The error
	// channel may be nil, close empty, or send one terminal result in either
	// order relative to token closure. Producers must observe ctx cancellation.
	ChatStream(ctx context.Context, messages []types.Message, tools []types.ToolDef) (<-chan types.StreamToken, <-chan error)
}

// RequestClient accepts per-request overrides without mutating client defaults.
// Existing third-party implementations of Client need no new methods.
type RequestClient interface {
	Client
	ChatWithOptions(context.Context, []types.Message, []types.ToolDef, types.RequestOptions) (*types.Response, error)
	ChatStreamWithOptions(context.Context, []types.Message, []types.ToolDef, types.RequestOptions) (<-chan types.StreamToken, <-chan error)
}

func SupportsRequestOptions(client Client) bool {
	_, ok := client.(RequestClient)
	return ok
}

// ChatStream applies overrides only when supported. Never silently ignore a
// requested override or temporarily mutate a legacy provider's global state.
func ChatStream(client Client, ctx context.Context, messages []types.Message, tools []types.ToolDef, options types.RequestOptions) (<-chan types.StreamToken, <-chan error) {
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
func ReasoningSettingsFor(client Client) types.ReasoningSettings {
	configured, ok := client.(interface {
		ReasoningSettings() types.ReasoningSettings
	})
	if !ok {
		return types.ReasoningSettings{}
	}
	return configured.ReasoningSettings()
}

// MaxOutputTokensFor reads optional model metadata without changing the client.
func MaxOutputTokensFor(client Client) int {
	if configured, ok := client.(interface{ GetMaxTokens() int }); ok {
		return max(0, configured.GetMaxTokens())
	}
	return 0
}
