package llm

import (
	"fmt"
	"testing"

	"nekocode/bot/llm/anthropic"
	"nekocode/bot/llm/openai"
)

func TestOpenAIClient(t *testing.T) {
	c := openai.New("key", "", "deepseek-chat")
	if c.APIKey != "key" {
		t.Error("bad API key")
	}
	if c.BaseURL != "https://api.deepseek.com/v1" {
		t.Errorf("bad base URL: %s", c.BaseURL)
	}
}

func TestOpenAIClient_CustomURL(t *testing.T) {
	c := openai.New("key", "https://api.xiaomimimo.com/v1", "mimo-v3")
	if c.BaseURL != "https://api.xiaomimimo.com/v1" {
		t.Errorf("bad base URL: %s", c.BaseURL)
	}
}

func TestOpenAIThinking(t *testing.T) {
	c := openai.New("k", "", "m")
	c.SetDisableThinking(true)
}

func TestAnthropicClient(t *testing.T) {
	c := anthropic.New("key", "https://api.xiaomimimo.com/anthropic/v1", "mimo-v3")
	if c.BaseURL != "https://api.xiaomimimo.com/anthropic/v1" {
		t.Errorf("bad base URL: %s", c.BaseURL)
	}
}

func TestFactory(t *testing.T) {
	if NewClientWithProtocol("deepseek", "k", "", "m", "openai") == nil {
		t.Error("deepseek should return LLM")
	}
	if NewClientWithProtocol("mimo", "k", "https://api.xiaomimimo.com/anthropic/v1", "mimo-v3", "anthropic") == nil {
		t.Error("mimo + anthropic should return LLM")
	}
	if NewClientWithProtocol("mimo", "k", "", "mimo-v3", "openai") == nil {
		t.Error("mimo + openai should return LLM")
	}
}

func TestRetryable(t *testing.T) {
	if !IsRetryable(fmt.Errorf("connection refused")) {
		t.Error("connection refused should be retryable")
	}
}
