package agent

import (
	"context"
	"testing"

	"nekocode/bot/contextmgr"
	"nekocode/bot/provider/types"
	"nekocode/bot/reasoning"
)

type fakeLLM struct {
	tokens []types.StreamToken
	calls  int
}

func (f *fakeLLM) Chat(context.Context, []types.Message, []types.ToolDef) (*types.Response, error) {
	panic("Chat should not be called by model")
}

func (f *fakeLLM) ChatStream(ctx context.Context, messages []types.Message, tools []types.ToolDef) (<-chan types.StreamToken, <-chan error) {
	f.calls++
	tokenCh := make(chan types.StreamToken, len(f.tokens))
	errCh := make(chan error, 1)
	for _, token := range f.tokens {
		tokenCh <- token
	}
	close(tokenCh)
	errCh <- nil
	close(errCh)
	return tokenCh, errCh
}

func (f *fakeLLM) SetMaxTokens(int) {}

func (f *fakeLLM) GetMaxTokens() int { return 0 }

func (f *fakeLLM) SetDisableThinking(bool) {}

func (f *fakeLLM) GetDisableThinking() bool { return false }

// failingLLM simulates a misconfigured model: the stream errors immediately
// on every call, so no assistant content ever reaches the context.
type failingLLM struct {
	fakeLLM
	calls int
}

func (f *failingLLM) ChatStream(context.Context, []types.Message, []types.ToolDef) (<-chan types.StreamToken, <-chan error) {
	f.calls++
	tokenCh := make(chan types.StreamToken)
	errCh := make(chan error, 1)
	errCh <- context.DeadlineExceeded
	close(tokenCh)
	close(errCh)
	return tokenCh, errCh
}

func newTestAgentWithLLM(tokens ...types.StreamToken) (*Agent, *fakeLLM) {
	a := newTestAgent()
	llm := &fakeLLM{tokens: tokens}
	a.deps.llmClient = llm
	return a, llm
}

func TestReasonCommandSkipsLLM(t *testing.T) {
	a, llm := newTestAgentWithLLM(types.StreamToken{Content: "should not be used"})

	result := a.modelRunner.reason("/status")

	if llm.calls != 0 {
		t.Fatalf("LLM calls = %d, want 0", llm.calls)
	}
	if result.Action != actionChat || result.Thought != "User entered a command" {
		t.Fatalf("unexpected command result: %+v", result)
	}
}

func TestReasonTextResponseStreamsAndClassifiesChat(t *testing.T) {
	a, _ := newTestAgentWithLLM(
		types.StreamToken{ReasoningContent: "thinking"},
		types.StreamToken{Content: "hello"},
		types.StreamToken{Content: " world"},
	)
	var streamedText, streamedReasoning string
	a.stream.text = func(delta string) { streamedText += delta }
	a.stream.reasoning = func(delta string) { streamedReasoning += delta }

	result := a.modelRunner.reason("hi")

	if result.ActionInput != "hello world" {
		t.Fatalf("ActionInput = %q, want hello world", result.ActionInput)
	}
	if result.ReasoningContent != "thinking" {
		t.Fatalf("response reasoning = %q, want thinking", result.ReasoningContent)
	}
	if streamedText != "hello world" {
		t.Fatalf("streamed text = %q, want hello world", streamedText)
	}
	if streamedReasoning != "thinking" {
		t.Fatalf("streamed reasoning = %q, want thinking", streamedReasoning)
	}
}

func TestReasonToolCallRecordsAssistantToolCall(t *testing.T) {
	a, _ := newTestAgentWithLLM(
		types.StreamToken{Content: "checking"},
		types.StreamToken{ToolCallDelta: &types.ToolCallDelta{Index: 0, ID: "call-1", Name: "read", Arguments: `{"path":"main.go"}`}},
	)
	before := a.deps.ctxMgr.Len()

	result := a.modelRunner.reason("read main")

	if len(result.ToolCalls) != 1 {
		t.Fatalf("tool calls = %d, want 1", len(result.ToolCalls))
	}
	if result.ToolCalls[0].Name != "read" || result.ToolCalls[0].Args["path"] != "main.go" {
		t.Fatalf("unexpected tool call: %+v", result.ToolCalls[0])
	}
	if got := a.deps.ctxMgr.Len(); got != before+1 {
		t.Fatalf("context length = %d, want %d", got, before+1)
	}
}

func TestReasonDoesNotReusePreviousCallReasoning(t *testing.T) {
	a, llm := newTestAgentWithLLM(
		types.StreamToken{ReasoningContent: "first reasoning"},
		types.StreamToken{ReasoningSignature: "first signature"},
		types.StreamToken{ToolCallDelta: &types.ToolCallDelta{Index: 0, ID: "call-1", Name: "read", Arguments: `{}`}},
	)
	a.deps.ctxMgr.ConfigureModel(contextmgr.ModelContext{
		Reasoning: types.ReasoningSettings{Replay: reasoning.ReplaySigned},
	})
	a.modelRunner.reason("first")

	llm.tokens = []types.StreamToken{
		{ToolCallDelta: &types.ToolCallDelta{Index: 0, ID: "call-2", Name: "read", Arguments: `{}`}},
	}
	a.modelRunner.reason("second")

	messages := a.deps.ctxMgr.Build()
	last := messages[len(messages)-1]
	if last.ReasoningContent != "" || last.ReasoningSignature != "" {
		t.Fatalf("second call reused reasoning artifact: %+v", last)
	}
}

func TestSynthesizeRecordsAssistantResponse(t *testing.T) {
	a, _ := newTestAgentWithLLM(types.StreamToken{Content: "final answer"})
	before := a.deps.ctxMgr.Len()

	output := a.modelRunner.synthesize()

	if output != "final answer" {
		t.Fatalf("output = %q, want final answer", output)
	}
	if got := a.deps.ctxMgr.Len(); got != before+1 {
		t.Fatalf("context length = %d, want %d", got, before+1)
	}
}

func TestIsGarbledToolCall(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{"<invoke name=\"read\"></invoke>", true},
		{"{\"tool_calls\":[{}]}", true},
		{"normal answer", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := isGarbledToolCall(tc.text); got != tc.want {
			t.Fatalf("isGarbledToolCall(%q) = %v, want %v", tc.text, got, tc.want)
		}
	}
}
