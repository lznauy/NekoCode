package llmstream

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"nekocode/bot/calllog"
	"nekocode/bot/provider"
	"nekocode/bot/provider/types"
	"nekocode/protocol"
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
		Ctx:       protocol.WithExecutionIdentity(t.Context(), protocol.ExecutionIdentity{RunID: "run", AttemptID: "attempt"}),
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
	if got.Execution.RunID != "run" || got.Execution.AttemptID != "attempt" || got.CallID == "" {
		t.Fatalf("physical model call lost execution identity: %+v", got)
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

	writeCallRecord(LLMCallOptions{}, &StreamResult{}, time.Now(), errors.New("stream idle timeout: no tokens"), "stream")

	if got.Err != "stream idle timeout" || got.FailureStage != "stream" {
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

// idleStreamClient keeps a physical request alive until its request context ends.
type idleStreamClient struct {
	fakeStreamClient
	requestDone chan struct{}
	emitToken   bool
}

func (c *idleStreamClient) ChatStream(ctx context.Context, _ []types.Message, _ []types.ToolDef) (<-chan types.StreamToken, <-chan error) {
	tokens := make(chan types.StreamToken, 1)
	errs := make(chan error, 1)
	if c.emitToken {
		tokens <- types.StreamToken{Content: "late"}
	}
	go func() {
		<-ctx.Done()
		errs <- ctx.Err()
		close(errs)
		close(tokens)
		close(c.requestDone)
	}()
	return tokens, errs
}

func TestAbandoningModelStreamCancelsOnlyCurrentRequest(t *testing.T) {
	for _, stopped := range []bool{false, true} {
		t.Run(fmt.Sprintf("stopped=%v", stopped), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				rec := captureRecord(t)
				client := &idleStreamClient{requestDone: make(chan struct{}), emitToken: stopped}
				_, err := CallLLM(client, LLMCallOptions{Ctx: ctx, CheckDone: func() bool { return stopped }})
				if stopped {
					if !errors.Is(err, context.Canceled) || rec.FailureStage != "cancelled" {
						t.Fatalf("err=%v stage=%s", err, rec.FailureStage)
					}
				} else if err == nil || !strings.Contains(err.Error(), "stream idle timeout") || rec.FailureStage != "stream" {
					t.Fatalf("err=%v stage=%s", err, rec.FailureStage)
				}
				synctest.Wait()
				select {
				case <-client.requestDone:
				default:
					t.Fatal("abandoned provider request is still running")
				}
				if ctx.Err() != nil {
					t.Fatalf("parent run canceled: %v", ctx.Err())
				}
			})
		})
	}
}

type channelModel struct {
	fakeStreamClient
	tokens chan types.StreamToken
	errs   chan error
}

func (m *channelModel) ChatStream(context.Context, []types.Message, []types.ToolDef) (<-chan types.StreamToken, <-chan error) {
	return m.tokens, m.errs
}

func TestCallLLMReadsUnbufferedErrorBeforeTokenClosure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		model := &channelModel{tokens: make(chan types.StreamToken), errs: make(chan error)}
		want := errors.New("provider failed")
		record := captureRecord(t)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go func() {
			defer close(model.tokens)
			defer close(model.errs)
			model.tokens <- types.StreamToken{Content: "partial"}
			select {
			case model.errs <- want:
			case <-ctx.Done():
			}
		}()
		result, err := CallLLM(model, LLMCallOptions{Ctx: ctx})
		cancel()
		if !errors.Is(err, want) || result.Text != "partial" || record.FailureStage != "completion" {
			t.Fatalf("text=%q err=%v stage=%s", result.Text, err, record.FailureStage)
		}
	})
}

func TestCallLLMHonorsCancellationWhileProviderIsSilent(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		record := captureRecord(t)
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		start := time.Now()
		model := &channelModel{tokens: make(chan types.StreamToken), errs: make(chan error)}
		_, err := CallLLM(model, LLMCallOptions{Ctx: ctx})
		if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) != time.Second || record.FailureStage != "cancelled" {
			t.Fatalf("returned after %v with %v, stage=%s", time.Since(start), err, record.FailureStage)
		}
	})
}

func TestLaterStopCannotCompleteTruncatedToolCall(t *testing.T) {
	model := &fakeStreamClient{tokens: []types.StreamToken{
		{ToolCallDelta: &types.ToolCallDelta{ID: "edit", Name: "edit", Arguments: "{}"}},
		{FinishReason: "length"},
		{FinishReason: "stop"},
	}}
	result, err := CallLLM(model, LLMCallOptions{Ctx: t.Context()})
	if !errors.Is(err, protocol.ErrIncompleteResponse) || len(result.ToolCalls) != 0 {
		t.Fatalf("calls=%v, err=%v", result.ToolCalls, err)
	}
}

func TestWithRetryUsesLegacyProviderDefaults(t *testing.T) {
	previous := provider.DefaultRetryConfig
	t.Cleanup(func() { provider.DefaultRetryConfig = previous })
	provider.DefaultRetryConfig = provider.RetryConfig{MaxAttempts: 2}
	failure := errors.New("connection reset")
	calls := 0
	err := WithRetry(t.Context(), func() error {
		calls++
		return failure
	})
	if calls != 2 || !errors.Is(err, failure) {
		t.Fatalf("calls=%d, err=%v", calls, err)
	}
}
