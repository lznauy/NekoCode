package model

import (
	"context"
	"testing"

	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/hooks"
	"nekocode/bot/llm/types"
	aggov "nekocode/bot/policy"
	"nekocode/bot/tools"
)

type fakeHost struct {
	ctx       context.Context
	ctxMgr    *ctxmgr.Manager
	llm       *fakeLLM
	registry  *tools.Registry
	gov       *aggov.Manager
	finished  bool
	reason    string
	phases    []string
	text      string
	reasoning string
	tokens    int
}

func newFakeHost(tokens ...types.StreamToken) *fakeHost {
	return &fakeHost{
		ctx:      context.Background(),
		ctxMgr:   ctxmgr.NewSub("test", 128000, nil),
		llm:      &fakeLLM{tokens: tokens},
		registry: tools.NewRegistry(),
		gov:      aggov.NewManager(hooks.NewRegistry()),
	}
}

func (h *fakeHost) Context() context.Context         { return h.ctx }
func (h *fakeHost) ContextManager() *ctxmgr.Manager  { return h.ctxMgr }
func (h *fakeHost) LLM() types.LLM                   { return h.llm }
func (h *fakeHost) ToolRegistry() *tools.Registry    { return h.registry }
func (h *fakeHost) Governance() *aggov.Manager       { return h.gov }
func (h *fakeHost) IsFinished() bool                 { return h.finished }
func (h *fakeHost) LastReason() string               { return h.reason }
func (h *fakeHost) SetLastReason(reason string)      { h.reason = reason }
func (h *fakeHost) Phase(phase string)               { h.phases = append(h.phases, phase) }
func (h *fakeHost) StreamText(delta string)          { h.text += delta }
func (h *fakeHost) StreamReasoning(delta string)     { h.reasoning += delta }
func (h *fakeHost) AddTokens(prompt, completion int) { h.tokens += prompt + completion }

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

func (f *fakeLLM) SetMaxTokens(int)         {}
func (f *fakeLLM) GetMaxTokens() int        { return 0 }
func (f *fakeLLM) SetDisableThinking(bool)  {}
func (f *fakeLLM) GetDisableThinking() bool { return false }

func TestReasonCommandSkipsLLM(t *testing.T) {
	host := newFakeHost(types.StreamToken{Content: "should not be used"})

	result := New(host).Reason("/status")

	if host.llm.calls != 0 {
		t.Fatalf("LLM calls = %d, want 0", host.llm.calls)
	}
	if result.Action != 0 || result.Thought != "User entered a command" {
		t.Fatalf("unexpected command result: %+v", result)
	}
}

func TestReasonTextResponseStreamsAndClassifiesChat(t *testing.T) {
	host := newFakeHost(
		types.StreamToken{ReasoningContent: "thinking"},
		types.StreamToken{Content: "hello"},
		types.StreamToken{Content: " world"},
	)

	result := New(host).Reason("hi")

	if result.ActionInput != "hello world" {
		t.Fatalf("ActionInput = %q, want hello world", result.ActionInput)
	}
	if host.reason != "thinking" {
		t.Fatalf("last reason = %q, want thinking", host.reason)
	}
	if host.text != "hello world" {
		t.Fatalf("streamed text = %q, want hello world", host.text)
	}
	if host.reasoning != "thinking" {
		t.Fatalf("streamed reasoning = %q, want thinking", host.reasoning)
	}
}

func TestReasonToolCallRecordsAssistantToolCall(t *testing.T) {
	host := newFakeHost(
		types.StreamToken{Content: "checking"},
		types.StreamToken{ToolCallDelta: &types.ToolCallDelta{Index: 0, ID: "call-1", Name: "read", Arguments: `{"path":"main.go"}`}},
	)
	before := host.ctxMgr.Len()

	result := New(host).Reason("read main")

	if len(result.ToolCalls) != 1 {
		t.Fatalf("tool calls = %d, want 1", len(result.ToolCalls))
	}
	if result.ToolCalls[0].Name != "read" || result.ToolCalls[0].Args["path"] != "main.go" {
		t.Fatalf("unexpected tool call: %+v", result.ToolCalls[0])
	}
	if got := host.ctxMgr.Len(); got != before+1 {
		t.Fatalf("context length = %d, want %d", got, before+1)
	}
}

func TestSynthesizeRecordsAssistantResponse(t *testing.T) {
	host := newFakeHost(types.StreamToken{Content: "final answer"})
	before := host.ctxMgr.Len()

	output := New(host).Synthesize()

	if output != "final answer" {
		t.Fatalf("output = %q, want final answer", output)
	}
	if got := host.ctxMgr.Len(); got != before+1 {
		t.Fatalf("context length = %d, want %d", got, before+1)
	}
}
