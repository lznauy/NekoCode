package llmstream

import (
	"context"
	"errors"
	"testing"
	"time"

	"nekocode/bot/calllog"
	"nekocode/bot/provider/types"
)

type fakeStreamClient struct {
	tokens []types.StreamToken
}

func (f *fakeStreamClient) Chat(context.Context, []types.Message, []types.ToolDef) (*types.Response, error) {
	return nil, nil
}
func (f *fakeStreamClient) ChatStream(context.Context, []types.Message, []types.ToolDef) (<-chan types.StreamToken, <-chan error) {
	tokens := make(chan types.StreamToken, len(f.tokens))
	errs := make(chan error)
	for _, token := range f.tokens {
		tokens <- token
	}
	close(tokens)
	close(errs)
	return tokens, errs
}
func (f *fakeStreamClient) SetMaxTokens(int)         {}
func (f *fakeStreamClient) GetMaxTokens() int        { return 0 }
func (f *fakeStreamClient) SetDisableThinking(bool)  {}
func (f *fakeStreamClient) GetDisableThinking() bool { return false }

func captureRecord(t *testing.T) *calllog.Record {
	t.Helper()
	var got calllog.Record
	origWrite := writeRecord
	writeRecord = func(rec calllog.Record) { got = rec }
	t.Cleanup(func() { writeRecord = origWrite })
	return &got
}

func TestWriteCallRecordAssemblesEvidence(t *testing.T) {
	got := captureRecord(t)

	start := time.Now().Add(-2 * time.Second)
	stream := &StreamResult{
		Usage:        types.StreamUsage{PromptTokens: 100, CacheHitTokens: 10, CacheMissTokens: 90, CompletionTokens: 5, ReasoningTokens: 2, CacheUsageReported: true},
		FirstTokenAt: start.Add(300 * time.Millisecond),
		Request: &types.RequestMeta{
			Model: "m", Protocol: "openai", BaseURL: "https://x",
			RequestedEffort: "high", EffectiveEffort: "high",
		},
	}
	opts := LLMCallOptions{
		Source:    "main",
		SessionID: "session-a",
		Diagnostics: func() calllog.PrefixDiag {
			return calllog.PrefixDiag{
				ChangedParts: []string{"tail/provider"},
				SystemHash:   "s", ToolsHash: "t", HistoryCount: 3, HistoryHash: "h",
			}
		},
	}
	writeCallRecord(opts, stream, start, nil)

	if got.Source != "main" || got.SessionID != "session-a" || got.Model != "m" || got.Protocol != "openai" {
		t.Errorf("request evidence = %+v", got)
	}
	if got.RequestedEffort != "high" || got.EffectiveEffort != "high" {
		t.Errorf("reasoning evidence = %+v", got)
	}
	if got.TotalTokens != 105 || got.InTokens != 100 || got.CachedTokens != 10 || got.NewTokens != 90 || got.OutTokens != 5 || got.ReasoningTokens != 2 || got.Usage == "" {
		t.Errorf("usage evidence = %+v", got)
	}
	if got.TTFTMs != 300 || got.DurMs < 1000 {
		t.Errorf("timing = ttft %dms dur %dms", got.TTFTMs, got.DurMs)
	}
	if got.PrefixDiag == nil || len(got.PrefixDiag.ChangedParts) != 1 || got.PrefixDiag.HistoryCount != 3 || got.Err != "" {
		t.Errorf("diag = %+v", got.PrefixDiag)
	}
}

func TestWriteCallRecordOnFailureKeepsErr(t *testing.T) {
	got := captureRecord(t)

	writeCallRecord(LLMCallOptions{}, &StreamResult{}, time.Now(), errors.New("stream idle timeout"))

	if got.Err != "error: *errors.errorString" {
		t.Errorf("err = %q", got.Err)
	}
	if got.Source != "unknown" {
		t.Errorf("source default = %q", got.Source)
	}
}

func TestCallLLMReportsFinalPerCallUsage(t *testing.T) {
	client := &fakeStreamClient{tokens: []types.StreamToken{{Usage: &types.StreamUsage{
		PromptTokens: 100, CompletionTokens: 12, ReasoningTokens: 2,
		CacheHitTokens: 90, CacheMissTokens: 10, CacheUsageReported: true,
	}}}}
	var got types.StreamUsage
	_, err := CallLLM(client, LLMCallOptions{
		Ctx:       context.Background(),
		Callbacks: StreamCallbacks{OnUsage: func(usage types.StreamUsage) { got = usage }},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.TotalTokens != 112 || got.PromptTokens != 100 || got.CacheHitTokens != 90 || got.CacheMissTokens != 10 || got.CompletionTokens != 12 || got.ReasoningTokens != 2 {
		t.Fatalf("recorded call usage = %+v", got)
	}
}

func TestCallLLMPreservesReasoningSignature(t *testing.T) {
	client := &fakeStreamClient{tokens: []types.StreamToken{
		{ReasoningContent: "inspect first"},
		{ReasoningSignature: "sig"},
	}}
	result, err := CallLLM(client, LLMCallOptions{Ctx: context.Background()})
	if err != nil {
		t.Fatal(err)
	}
	if result.Reasoning != "inspect first" || result.ReasoningSignature != "sig" {
		t.Fatalf("reasoning round trip = %+v", result)
	}
}
