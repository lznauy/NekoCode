package llmstream

import (
	"context"
	"errors"
	"strings"
	"testing"

	"nekocode/bot/provider/types"
)

type partialRetryClient struct {
	fakeStreamClient
	calls int
}

func (f *partialRetryClient) ChatStream(context.Context, []types.Message, []types.ToolDef) (<-chan types.StreamToken, <-chan error) {
	tokens := make(chan types.StreamToken, 2)
	errs := make(chan error, 1)
	f.calls++
	if f.calls == 1 {
		tokens <- types.StreamToken{Content: "partial"}
		errs <- errors.New("connection reset")
	} else {
		tokens <- types.StreamToken{Content: "Let me check."}
		tokens <- types.StreamToken{ToolCallDelta: &types.ToolCallDelta{Index: 0, ID: "c", Name: "read", Arguments: `{}`}}
		errs <- nil
	}
	close(tokens)
	close(errs)
	return tokens, errs
}

func TestRetryAfterPartialOutputResetsText(t *testing.T) {
	client := &partialRetryClient{}
	resets := 0
	visible := ""
	result, err := CallLLMWithRetry(context.Background(), client, func() LLMCallOptions {
		return LLMCallOptions{Ctx: context.Background(), Callbacks: StreamCallbacks{OnText: func(d string) { visible += d }, OnTextReset: func() { resets++; visible = "" }}}
	})
	if err != nil {
		t.Fatal(err)
	}
	if resets != 1 || result.Text != "Let me check." || visible != result.Text || len(result.ToolCalls) != 1 {
		t.Fatalf("resets=%d result=%+v", resets, result)
	}
}

type failedThinkingRetryClient struct{ dsmlRetryClient }

func (f *failedThinkingRetryClient) ChatStreamWithOptions(ctx context.Context, messages []types.Message, tools []types.ToolDef, _ types.RequestOptions) (<-chan types.StreamToken, <-chan error) {
	return f.ChatStream(ctx, messages, tools)
}

func (f *failedThinkingRetryClient) ChatStream(context.Context, []types.Message, []types.ToolDef) (<-chan types.StreamToken, <-chan error) {
	tokens, errs := make(chan types.StreamToken, 1), make(chan error, 1)
	if f.calls == 0 {
		tokens <- types.StreamToken{ReasoningContent: "thinking"}
		errs <- nil
	} else {
		tokens <- types.StreamToken{Content: "discarded retry"}
		errs <- errors.New("terminal error")
	}
	f.calls++
	close(tokens)
	close(errs)
	return tokens, errs
}

func TestFailedThinkingRetryRetractsDiscardedOutput(t *testing.T) {
	client := &failedThinkingRetryClient{}
	visible := ""
	result, err := CallLLMWithRetry(context.Background(), client, func() LLMCallOptions {
		return LLMCallOptions{Ctx: context.Background(), Callbacks: StreamCallbacks{
			OnText: func(d string) { visible += d }, OnTextReset: func() { visible = "" },
		}}
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "" || visible != "" {
		t.Fatalf("discarded result still visible: result=%+v visible=%q", result, visible)
	}
}

// dsmlRetryClient models the observed provider behavior: it never emits a
// native tool call, so the thinking-disabled retry in CallLLMWithRetry fires,
// and the retry answers with the requested call wrapped in DSML text.
type dsmlRetryClient struct {
	calls int
}

func (f *dsmlRetryClient) ChatWithOptions(ctx context.Context, messages []types.Message, tools []types.ToolDef, _ types.RequestOptions) (*types.Response, error) {
	return f.Chat(ctx, messages, tools)
}
func (f *dsmlRetryClient) ChatStreamWithOptions(ctx context.Context, messages []types.Message, tools []types.ToolDef, _ types.RequestOptions) (<-chan types.StreamToken, <-chan error) {
	return f.ChatStream(ctx, messages, tools)
}

func (f *dsmlRetryClient) Chat(context.Context, []types.Message, []types.ToolDef) (*types.Response, error) {
	return nil, nil
}
func (f *dsmlRetryClient) ChatStream(context.Context, []types.Message, []types.ToolDef) (<-chan types.StreamToken, <-chan error) {
	tokens := make(chan types.StreamToken, 4)
	switch f.calls {
	case 0:
		tokens <- types.StreamToken{ReasoningContent: "thinking only"}
	default:
		tokens <- types.StreamToken{Content: `<｜｜DSML｜｜ calls> <｜｜DSML｜｜ invoke name="edit"> </｜｜DSML｜｜ invoke> </｜｜DSML｜｜ calls>`}
	}
	f.calls++
	close(tokens)
	return tokens, func() <-chan error {
		errs := make(chan error, 1)
		errs <- nil
		close(errs)
		return errs
	}()
}
func (f *dsmlRetryClient) SetMaxTokens(int)         {}
func (f *dsmlRetryClient) GetMaxTokens() int        { return 0 }
func (f *dsmlRetryClient) SetDisableThinking(bool)  {}
func (f *dsmlRetryClient) GetDisableThinking() bool { return false }

// ReasoningSettings declares the thinking switch the reasoning-only retry needs.
func (f *dsmlRetryClient) ReasoningSettings() types.ReasoningSettings {
	return types.ReasoningSettings{ThinkingMode: "enabled"}
}

func TestCallLLMWithRetryKeepsDSMLFromEmptyRetry(t *testing.T) {
	client := &dsmlRetryClient{}
	result, err := CallLLMWithRetry(context.Background(), client, func() LLMCallOptions {
		return LLMCallOptions{Ctx: context.Background()}
	})
	if err != nil {
		t.Fatalf("call failed: %v", err)
	}
	if client.calls != 2 {
		t.Fatalf("calls = %d, want reasoning-only retry", client.calls)
	}
	if result == nil || result.Text == "" {
		t.Fatalf("DSML text was dropped before the agent could classify it: %+v", result)
	}
	if !strings.Contains(result.Text, "DSML") {
		t.Fatalf("text = %q", result.Text)
	}
}

func TestConsumeStreamPreservesContentBeforeToolCall(t *testing.T) {
	var streamed string
	resets := 0
	cb := StreamCallbacks{
		OnText:      func(d string) { streamed += d },
		OnTextReset: func() { resets++; streamed = "" },
	}

	// First stream of the call: content before the tool call is a preamble.
	preamble := make(chan types.StreamToken, 2)
	preamble <- types.StreamToken{Content: "Let me check. "}
	preamble <- types.StreamToken{ToolCallDelta: &types.ToolCallDelta{Index: 0, ID: "call-1", Name: "read", Arguments: `{}`}}
	close(preamble)
	stream := &StreamResult{}
	if err := ConsumeStream(preamble, stream, cb, nil); err != nil {
		t.Fatal(err)
	}
	if stream.TextBuf.String() != "Let me check. " || resets != 0 {
		t.Fatalf("first-stream preamble: text=%q resets=%d", stream.TextBuf.String(), resets)
	}

	// Protocol-looking text is classified by the agent, not by transport.
	retry := make(chan types.StreamToken, 2)
	retry <- types.StreamToken{Content: `<｜｜DSML｜｜ calls>`}
	retry <- types.StreamToken{ToolCallDelta: &types.ToolCallDelta{Index: 0, ID: "call-2", Name: "edit", Arguments: `{"path":"a.go"}`}}
	close(retry)
	stream = &StreamResult{}
	if err := ConsumeStream(retry, stream, cb, nil); err != nil {
		t.Fatal(err)
	}
	if stream.TextBuf.String() != `<｜｜DSML｜｜ calls>` {
		t.Fatalf("text changed in transport: %q", stream.TextBuf.String())
	}
	if resets != 0 {
		t.Fatalf("unexpected transport reset: %d", resets)
	}
	if got := len(stream.CollectToolCalls()); got != 1 {
		t.Fatalf("tool calls = %d, want 1", got)
	}
}
