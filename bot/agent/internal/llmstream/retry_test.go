package llmstream

import (
	"context"
	"testing"

	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/provider/types"
)

// glmFakeClient simulates a thinking-switch model whose first call streams
// reasoning only (empty content) and whose retry returns a real answer.
type glmFakeClient struct {
	fakeStreamClient
	calls           int
	thinkingOff     bool
	responses       [][]types.StreamToken
	requestDisabled []bool
}

func (f *glmFakeClient) ChatWithOptions(ctx context.Context, messages []types.Message, tools []types.ToolDef, _ types.RequestOptions) (*types.Response, error) {
	return f.Chat(ctx, messages, tools)
}

func (f *glmFakeClient) ChatStreamWithOptions(ctx context.Context, messages []types.Message, tools []types.ToolDef, options types.RequestOptions) (<-chan types.StreamToken, <-chan error) {
	disabled := f.thinkingOff
	if options.DisableThinking != nil {
		disabled = *options.DisableThinking
	}
	f.requestDisabled = append(f.requestDisabled, disabled)
	return f.ChatStream(ctx, messages, tools)
}

func (f *glmFakeClient) ChatStream(context.Context, []types.Message, []types.ToolDef) (<-chan types.StreamToken, <-chan error) {
	tokens := make(chan types.StreamToken, len(f.responses[f.calls]))
	for _, token := range f.responses[f.calls] {
		tokens <- token
	}
	f.calls++
	close(tokens)
	errs := make(chan error)
	close(errs)
	return tokens, errs
}

func (f *glmFakeClient) SetDisableThinking(disable bool) { f.thinkingOff = disable }
func (f *glmFakeClient) GetDisableThinking() bool        { return f.thinkingOff }

// thinkingMode satisfies the optional ReasoningSettings probe like the real
// openai client does for GLM profiles.
func (f *glmFakeClient) ReasoningSettings() types.ReasoningSettings {
	return types.ReasoningSettings{ThinkingMode: "enabled", Disabled: f.thinkingOff}
}

// TestCallLLMWithRetryRecoversReasoningOnlyEmptyResponse covers the GLM
// failure mode: a stream that completes with reasoning but no content. The
// wrapper must retry once with thinking disabled and return the retry's
// answer.
func TestCallLLMWithRetryRecoversReasoningOnlyEmptyResponse(t *testing.T) {
	client := &glmFakeClient{responses: [][]types.StreamToken{
		{
			{ReasoningContent: "让我想想这张图……我看不到图片内容"},
			{FinishReason: "stop"},
		},
		{
			{Content: "这是 Moo日记 的启动页截图。"},
			{FinishReason: "stop"},
		},
	}}
	result, err := CallLLMWithRetry(context.Background(), client, func() LLMCallOptions {
		return LLMCallOptions{Ctx: context.Background()}
	})
	if err != nil {
		t.Fatal(err)
	}
	if client.calls != 2 {
		t.Fatalf("calls = %d, want 2 (original + thinking-disabled retry)", client.calls)
	}
	if result == nil || result.Text != "这是 Moo日记 的启动页截图。" {
		t.Fatalf("retry result = %#v", result)
	}
	if client.thinkingOff {
		t.Fatal("thinking must be restored after the retry")
	}
	if len(client.requestDisabled) != 2 || client.requestDisabled[0] || !client.requestDisabled[1] {
		t.Fatalf("request flags=%v", client.requestDisabled)
	}
}

// A model without a thinking switch never takes the retry path even when
// the response is reasoning-only.
func TestCallLLMWithRetrySkipsNonThinkingModel(t *testing.T) {
	plain := &fakeStreamClient{tokens: []types.StreamToken{
		{ReasoningContent: "…"},
		{FinishReason: "stop"},
	}}
	if retryEmptyContent(plain, &LLMCallResult{Reasoning: "…"}) {
		t.Fatal("model without a thinking switch must not take the retry path")
	}

	// Negative cases: content present, tool calls present, no reasoning,
	// thinking already disabled.
	glm := &glmFakeClient{}
	if retryEmptyContent(glm, &LLMCallResult{Text: "有正文", Reasoning: "…"}) {
		t.Fatal("non-empty Text must not retry")
	}
	if retryEmptyContent(glm, &LLMCallResult{Reasoning: "…", ToolCalls: []core.ToolCallItem{{}}}) {
		t.Fatal("tool-call responses are not empty")
	}
	if retryEmptyContent(glm, &LLMCallResult{}) {
		t.Fatal("no reasoning means a different failure, not the retry path")
	}
	glm.thinkingOff = true
	if retryEmptyContent(glm, &LLMCallResult{Reasoning: "…"}) {
		t.Fatal("already thinking-disabled must not retry again")
	}
	if retryEmptyContent(glm, nil) {
		t.Fatal("nil result must never retry")
	}
}
