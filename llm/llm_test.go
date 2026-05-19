package llm

import (
	"fmt"
	"testing"
)

func TestNewAnthropic(t *testing.T) {
	a := NewAnthropic("key", "", "claude-sonnet-4-6")
	if a.APIKey != "key" {
		t.Error("bad API key")
	}
	if a.BaseURL != "https://api.anthropic.com/v1" {
		t.Errorf("bad base URL: %s", a.BaseURL)
	}
	if a.MaxTokens() != 32000 {
		t.Errorf("bad max tokens: %d", a.MaxTokens())
	}
}

func TestNewDeepSeek(t *testing.T) {
	d := NewDeepSeek("key", "", "deepseek-v4-pro")
	if d.BaseURL != "https://api.deepseek.com/v1" {
		t.Errorf("bad base URL: %s", d.BaseURL)
	}
}

func TestAnthropicThinkingBudget(t *testing.T) {
	a := NewAnthropic("k", "", "m")
	a.SetThinkingBudget(8000)
	if a.thinkingType != "enabled" || a.thinkingBudget != 8000 {
		t.Error("thinking budget not set")
	}

	a.SetDisableThinking(true)
	if a.thinkingType != "disabled" {
		t.Error("thinking not disabled")
	}
}

func TestDeepSeekThinking(t *testing.T) {
	d := NewDeepSeek("k", "", "m")
	d.SetDisableThinking(true)
	if !d.disableThinking {
		t.Error("thinking not disabled")
	}

	d.SetThinkingBudget(0)
	if d.disableThinking {
		t.Error("zero budget should enable thinking")
	}

	d.SetThinkingBudget(16000)
	if d.reasoningEffort != "max" {
		t.Errorf("16000 tokens -> max, got %q", d.reasoningEffort)
	}
}

func TestDeepSeekCacheStats(t *testing.T) {
	d := NewDeepSeek("k", "", "m")
	hit, miss := d.CacheStats()
	if hit != 0 || miss != 0 {
		t.Error("initial stats should be zero")
	}
	if d.CacheHitRatio() != 0 {
		t.Error("initial ratio should be zero")
	}
}

func TestFactory(t *testing.T) {
	if _, ok := NewClient("anthropic", "k", "", "m", 0).(*Anthropic); !ok {
		t.Error("not Anthropic")
	}
	if _, ok := NewClient("deepseek", "k", "", "m", 0).(*DeepSeek); !ok {
		t.Error("not DeepSeek")
	}
	if NewClient("unknown", "", "", "", 0) != nil {
		t.Error("unknown should return nil")
	}
}

func TestClone(t *testing.T) {
	orig := NewClient("anthropic", "key", "https://api.example.com", "m", 0)
	clone := Clone("anthropic", "key", "https://api.example.com", "m", 0)
	if orig.MaxTokens() != clone.MaxTokens() {
		t.Error("clone mismatch")
	}
}

func TestRetryable(t *testing.T) {
	if !IsRetryable(fmt.Errorf("connection refused")) {
		t.Error("connection refused should be retryable")
	}
}
