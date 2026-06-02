package llm

import (
	"nekocode/llm/anthropic"
	"nekocode/llm/openai"
	"nekocode/llm/types"
)

// NewClient creates an LLM client using the OpenAI-compatible protocol.
func NewClient(provider, apiKey, baseURL, model string) types.LLM {
	return NewClientWithProtocol(provider, apiKey, baseURL, model, "openai")
}

// NewClientWithProtocol creates an LLM client with explicit protocol selection.
// protocol: "openai" or "anthropic".
func NewClientWithProtocol(provider, apiKey, baseURL, model, protocol string) types.LLM {
	switch protocol {
	case "anthropic":
		return anthropic.New(apiKey, baseURL, model)
	default:
		c := openai.New(apiKey, baseURL, model)
		c.SetDisableThinking(true)
		return c
	}
}
