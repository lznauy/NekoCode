package contextmgr

import (
	"context"
	"testing"

	"nekocode/bot/calllog"
	"nekocode/bot/provider/types"
)

type usageTestClient struct {
	response *types.Response
	err      error
}

func (c *usageTestClient) Chat(context.Context, []types.Message, []types.ToolDef) (*types.Response, error) {
	return c.response, c.err
}

func (c *usageTestClient) ChatStream(context.Context, []types.Message, []types.ToolDef) (<-chan types.StreamToken, <-chan error) {
	tokens := make(chan types.StreamToken, 2)
	errs := make(chan error, 1)
	if c.response != nil {
		if len(c.response.Choices) > 0 {
			tokens <- types.StreamToken{Content: c.response.Choices[0].Message.Content}
		}
		tokens <- types.StreamToken{Usage: &c.response.Usage}
	}
	if c.err != nil {
		errs <- c.err
	}
	close(tokens)
	close(errs)
	return tokens, errs
}

func (*usageTestClient) SetMaxTokens(int)         {}
func (*usageTestClient) GetMaxTokens() int        { return 0 }
func (*usageTestClient) SetDisableThinking(bool)  {}
func (*usageTestClient) GetDisableThinking() bool { return true }
func (*usageTestClient) RequestMeta() types.RequestMeta {
	return types.RequestMeta{Model: "compact-model", Protocol: "openai", BaseURL: "https://secret@example.com/v1?key=secret"}
}

func TestNewBuildsLayeredContext(t *testing.T) {
	manager := New(Config{SystemPrompt: "system"})
	manager.Add("user", "hello")

	messages := manager.Build()
	if len(messages) != 2 || messages[0].Content != "system" || messages[1].Content != "hello" {
		t.Fatalf("Build() = %#v", messages)
	}
}

func TestSummarizerRecordsStreamingUsage(t *testing.T) {
	client := &usageTestClient{response: &types.Response{
		Choices: []types.Choice{{Message: types.Message{Content: "a complete summary"}}},
		Usage: types.StreamUsage{
			PromptTokens: 100, CompletionTokens: 20, ReasoningTokens: 5,
			CacheHitTokens: 80, CacheMissTokens: 20, CacheUsageReported: true,
		},
	}}
	m := New(Config{})
	m.SetSessionIDProvider(func() string { return "session-compaction" })
	var recordedUsage types.StreamUsage
	m.SetLLMUsageRecorder(func(usage types.StreamUsage) { recordedUsage = usage })
	var recordedCall calllog.Record
	originalWrite := writeCompactionRecord
	writeCompactionRecord = func(rec calllog.Record) { recordedCall = rec }
	t.Cleanup(func() { writeCompactionRecord = originalWrite })

	summary, err := m.makeSummarizer(context.Background(), client)(nil, "")
	if err != nil || summary != "a complete summary" {
		t.Fatalf("summary = %q, err = %v", summary, err)
	}
	if recordedUsage.TotalTokens != 120 || recordedUsage.ReasoningTokens != 5 {
		t.Fatalf("run usage = %+v", recordedUsage)
	}
	if recordedCall.Source != "compaction" || recordedCall.SessionID != "session-compaction" || recordedCall.Model != "compact-model" || recordedCall.TotalTokens != 120 || recordedCall.ReasoningTokens != 5 {
		t.Fatalf("calllog = %+v", recordedCall)
	}
	if recordedCall.BaseURL != "https://example.com" {
		t.Fatalf("calllog base URL was not sanitized: %q", recordedCall.BaseURL)
	}
}

func TestSummarizerHandlesNilResponse(t *testing.T) {
	m := New(Config{})
	originalWrite := writeCompactionRecord
	writeCompactionRecord = func(calllog.Record) {}
	t.Cleanup(func() { writeCompactionRecord = originalWrite })

	if _, err := m.makeSummarizer(context.Background(), &usageTestClient{})(nil, ""); err == nil {
		t.Fatal("nil response should return an error")
	}
}
