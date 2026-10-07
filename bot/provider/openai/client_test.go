package openai

import (
	"encoding/json"
	"strings"
	"testing"

	"nekocode/bot/provider/types"
	"nekocode/bot/reasoning"
)

func TestBuildBodyOmitsInternalToolErrorFlag(t *testing.T) {
	c := New("", "", "test-model")
	body := c.buildBody([]types.Message{
		{Role: "tool", Content: "command failed", ToolCallID: "tc1", IsError: true},
	}, nil, false)
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if strings.Contains(s, "is_error") {
		t.Fatalf("request body leaks internal is_error flag: %s", s)
	}
	if !strings.Contains(s, `"tool_call_id":"tc1"`) {
		t.Fatalf("request body missing tool_call_id: %s", s)
	}
}

func TestBuildBodyUsesProviderDefaultTemperatureUnlessConfigured(t *testing.T) {
	c := New("", "", "test-model")
	body := c.buildBody(nil, nil, false)
	if _, exists := body["temperature"]; exists {
		t.Fatalf("default request forced temperature: %+v", body)
	}
	c.Temperature = 0.3
	if got := c.buildBody(nil, nil, false)["temperature"]; got != 0.3 {
		t.Fatalf("configured temperature = %#v, want 0.3", got)
	}
}

func TestBuildBodyIncludesConfiguredReasoningEffort(t *testing.T) {
	c := New("", "", "test-model")
	c.SetReasoningSettings(types.ReasoningSettings{Requested: "high", Effort: "high", ThinkingMode: "enabled"})
	body := c.buildBody(nil, nil, false)
	if got := body["reasoning_effort"]; got != "high" {
		t.Fatalf("reasoning_effort = %#v, want high", got)
	}
	if got := body["thinking"]; got == nil {
		t.Fatal("configured reasoning effort did not enable thinking")
	}
}

func TestBuildBodyDoesNotGuessThinkingModeFromProtocol(t *testing.T) {
	c := New("", "", "gpt-5")
	c.SetReasoningSettings(types.ReasoningSettings{Requested: "high", Effort: "high"})
	body := c.buildBody(nil, nil, false)
	if body["reasoning_effort"] != "high" {
		t.Fatalf("reasoning_effort = %#v", body["reasoning_effort"])
	}
	if _, ok := body["thinking"]; ok {
		t.Fatalf("model without a thinking toggle emitted one: %+v", body)
	}
}

func TestBuildBodyLeavesAutoReasoningToProvider(t *testing.T) {
	body := New("", "", "test-model").buildBody(nil, nil, false)
	if _, ok := body["reasoning_effort"]; ok {
		t.Fatalf("auto reasoning emitted reasoning_effort: %+v", body)
	}
	if _, ok := body["thinking"]; ok {
		t.Fatalf("auto reasoning emitted thinking override: %+v", body)
	}
}

func TestBuildBodyUsesDeepSeekThinkingToolContract(t *testing.T) {
	c := New("", "", "deepseek-v4-flash")
	c.SetReasoningSettings(types.ReasoningSettings{
		Effort: "high", ThinkingMode: "enabled", Replay: reasoning.ReplayToolCalls,
	})
	body := c.buildBody([]types.Message{{
		Role: "assistant", ToolCalls: []types.ToolCall{{ID: "call-1"}},
	}}, []types.ToolDef{{Type: "function"}}, false)
	if _, exists := body["tool_choice"]; exists {
		t.Fatalf("DeepSeek thinking request emitted unsupported tool_choice: %+v", body)
	}
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"content":""`) {
		t.Fatalf("tool-call assistant content must be non-null: %s", data)
	}
}

// Volatile layers must keep their tail positions: the provider's prefix
// cache matches byte-for-byte from the front, so hoisting a per-turn hint
// ahead of the history would cold-start the cache on every turn.
func TestToAPIMessagesPreservesOrderAndPositions(t *testing.T) {
	c := New("", "", "test")
	got := c.toAPIMessages([]types.Message{
		{Role: "system", Content: "stable"},
		{Role: "user", Content: "request"},
		{Role: "system", Content: "runtime"},
		{Role: "assistant", Content: "answer"},
		{Role: "system", Content: "current hint"},
	}, types.ReasoningSettings{})
	if len(got) != 5 {
		t.Fatalf("messages = %d, want all 5 in original order: %+v", len(got), got)
	}
	want := []struct{ role, content string }{
		{"system", "stable"},
		{"user", "request"},
		{"system", "runtime"},
		{"assistant", "answer"},
		{"system", "current hint"},
	}
	for i, w := range want {
		if got[i].Role != w.role || got[i].Content != w.content {
			t.Fatalf("message %d = %+v, want %s %q", i, got[i], w.role, w.content)
		}
	}
}

func TestToAPIMessagesReplaysOnlyRequiredReasoning(t *testing.T) {
	c := New("", "", "test")
	settings := types.ReasoningSettings{Replay: reasoning.ReplayToolCalls}
	got := c.toAPIMessages([]types.Message{
		{Role: "assistant", Content: "answer", ReasoningContent: "private reasoning"},
		{Role: "assistant", ReasoningContent: "tool reasoning", ToolCalls: []types.ToolCall{{ID: "call-1"}}},
		{Role: "assistant", ToolCalls: []types.ToolCall{{ID: "call-2"}}},
	}, settings)
	if got[0].ReasoningContent != nil {
		t.Fatal("plain assistant reasoning must stay out of the provider request")
	}
	if got[1].ReasoningContent == nil || *got[1].ReasoningContent != "tool reasoning" {
		t.Fatalf("tool-call reasoning was not replayed: %+v", got[1])
	}
	if got[2].ReasoningContent == nil || *got[2].ReasoningContent != "" {
		t.Fatalf("required empty reasoning_content field missing: %+v", got[2])
	}

	got = c.toAPIMessages([]types.Message{{
		Role: "assistant", ReasoningContent: "provider reasoning", ToolCalls: []types.ToolCall{{ID: "call-1"}},
	}}, types.ReasoningSettings{})
	if got[0].ReasoningContent != nil {
		t.Fatal("providers without a replay contract must not receive reasoning_content")
	}
}

func TestEndpointJoinsBaseURL(t *testing.T) {
	c := New("", "https://api.example.com/openai/v1/", "test-model")
	if got := c.endpoint("/chat/completions"); got != "https://api.example.com/openai/v1/chat/completions" {
		t.Fatalf("endpoint = %q", got)
	}
}

func TestEndpointAddsMissingVersion(t *testing.T) {
	c := New("", "https://api.longcat.chat/openai", "test-model")
	if got := c.endpoint("chat/completions"); got != "https://api.longcat.chat/openai/v1/chat/completions" {
		t.Fatalf("endpoint = %q", got)
	}
}
