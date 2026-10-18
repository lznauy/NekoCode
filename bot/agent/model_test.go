package agent

import (
	"context"
	"strings"
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
		// Provider-injected DSML wrapper: the live failure mode replayed the
		// fullwidth pipe variant as plain content.
		{"<｜｜DSML｜｜ calls> <｜｜DSML｜｜ invoke name=\"edit\">", true},
		{"<DSML calls> <DSML invoke name=\"edit\">", true},
		{"<parameter name=\"path\" string=\"true\">a.go</parameter>", true},
		// Prose that merely mentions the protocol must not be mistaken for a
		// leaked call. The observed false positive was a long report that
		// described "DSML 前缀处理" twice.
		{"内部事件包含 DSML、tool_calls 等标记吗？", false},
		{"llmstream 的 DSML 前缀处理（provider 在重试流里注入的工具调用包装）", false},
		{"正常回答\n\n### 验证\n\n```\ngo build ./...\n```", false},
		// Naming a tag is not emitting it: no name= follow-up, so prose stays
		// an answer even when it quotes the exact tag spellings.
		{"参数标签写作 `<parameter name=\"path\">`，和 `<invoke` 配对使用。", false},
		{"protocolMarkers 里现在有 \"<invoke\"、\"</invoke\"、\"<parameter\" 这些片段。", false},
		{"真实泄漏时模型会写出 invoke 标签，而不是走原生工具通道。", false},
		{"正文只要出现 tool_use 这个词，旧的子串检测就会误判。", false},
		// Markdown code means the model is showing the syntax, not calling.
		{"```xml\n<invoke name=\"read\"><parameter name=\"path\">a.go</parameter></invoke>\n```", false},
		{"```json\n{\"tool_calls\":[{\"name\":\"read\"}]}\n```", false},
		// Inline code is a quotation too. A report showing the JSON call in
		// backticks used to be classified as a leak because the JSON check
		// inspected the raw text.
		{"调用形如 `{\"tool_calls\":[{\"name\":\"read\",\"arguments\":\"{}\"}]}` 的结构。", false},
		// A real leaked call is a short wrapper, even when it repeats.
		{strings.Repeat("<parameter name=\"path\">x</parameter>", 20), true},
		// A large argument (a write payload) must not hide the leak behind
		// the length bound: the response still opens with a call wrapper.
		{"<invoke name=\"write\"><parameter name=\"content\">" + strings.Repeat("x", 2000), true},
		{strings.Repeat("没有任何标记的正文。", 3000), false},
		{"normal answer", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := isGarbledToolCall(tc.text); got != tc.want {
			t.Fatalf("isGarbledToolCall(%q) = %v, want %v", tc.text, got, tc.want)
		}
	}
}

// garbledPrefix guards the truncated-reply path, where the stream was cut
// before any closing structure arrived. It must still reject text that only
// quotes a tag, and must recognise the fullwidth-pipe DSML variant.
func TestGarbledPrefix(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{"<｜｜DSML｜｜ calls> <｜｜DSML｜｜ invoke name=\"edit\">", true},
		{"<invoke name=\"read\">", true},
		{"<parameter name=\"path\">a.go", true},
		// A truncated answer that merely quotes the tag inline is prose.
		{"正文里提到 `<parameter name=\"path\">` 只是说明。", false},
		// An opening code fence means the model is showing the syntax.
		{"```xml\n<invoke name=\"read\">", false},
		{"正常回答", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := garbledPrefix(tc.text); got != tc.want {
			t.Fatalf("garbledPrefix(%q) = %v, want %v", tc.text, got, tc.want)
		}
	}
}

func TestGarbledToolCallKeepsMarkdownExamples(t *testing.T) {
	for _, text := range []string{
		"~~~xml\n<invoke name=\"read\"></invoke>\n~~~",
		"示例：``<invoke name=\"read\"></invoke>``",
		"```xml\n<invoke name=\"read\"><parameter name=\"x\">`value`</parameter></invoke>\n```",
	} {
		if isGarbledToolCall(text) {
			t.Errorf("quoted answer discarded: %q", text)
		}
	}
}

// A long answer that discusses the protocol used to be discarded as a leaked
// call, which then injected a format-correction hint. It must stay an answer
// all the way through the model runner.
func TestReasonReportMentioningProtocolStaysChat(t *testing.T) {
	a, _ := newTestAgentWithLLM(
		types.StreamToken{Content: "## 改动范围\n\n"},
		types.StreamToken{Content: "llmstream 的 DSML 前缀处理（provider 在重试流里注入的工具调用包装）\n"},
		types.StreamToken{Content: "正文只要出现 tool_use 这个词，旧的子串检测就会误判。"},
	)

	result := a.modelRunner.reason("总结一下改动")

	if result.GarbledToolCall {
		t.Fatal("prose mentioning the protocol must not be classified as a leaked call")
	}
	if result.Action != actionChat {
		t.Fatalf("action = %v, want chat", result.Action)
	}
	if result.ActionInput == "" {
		t.Fatal("ActionInput is empty; the answer was discarded")
	}
}

// The real failure mode must still be detected after streaming.
func TestReasonLeakedCallIsGarbled(t *testing.T) {
	a, _ := newTestAgentWithLLM(
		types.StreamToken{Content: `<invoke name="read">`},
		types.StreamToken{Content: `<parameter name="path">a.go</parameter></invoke>`},
	)
	streamed := ""
	a.BindOutput(Output{Text: func(delta string) { streamed += delta }, ResetText: func() { streamed = "" }})

	result := a.modelRunner.reason("read a.go")

	if !result.GarbledToolCall {
		t.Fatalf("leaked call not detected: %+v", result)
	}
	if result.ActionInput != "" {
		t.Fatalf("garbled text must not become output, got %q", result.ActionInput)
	}
	if streamed != "" {
		t.Fatalf("format-rejected text remains visible: %q", streamed)
	}
}

func TestNativeToolCallsOnlyRetractProtocolText(t *testing.T) {
	for _, text := range []string{"Let me check.", `<｜｜DSML｜｜ calls>`} {
		a, _ := newTestAgentWithLLM(
			types.StreamToken{Content: text},
			types.StreamToken{ToolCallDelta: &types.ToolCallDelta{Index: 0, ID: "c1", Name: "read", Arguments: `{}`}},
		)
		visible := ""
		a.BindOutput(Output{Text: func(d string) { visible += d }, ResetText: func() { visible = "" }})
		result := a.modelRunner.reason("read a.go")
		want := text
		if strings.Contains(text, "DSML") {
			want = ""
		}
		if len(result.ToolCalls) != 1 || result.TextContent != want || visible != want {
			t.Fatalf("text=%q result=%+v visible=%q", text, result, visible)
		}
	}
}

// A leaked call cannot be used as the synthesized final answer either.
func TestSynthesizeRejectsLeakedCall(t *testing.T) {
	a, _ := newTestAgentWithLLM(types.StreamToken{Content: `<invoke name="edit"></invoke>`})

	if got := a.modelRunner.synthesize(); got != fallbackSynthesize {
		t.Fatalf("synthesize = %q, want fallback", got)
	}
}

// A quoted call inside markdown code is an answer, not a leak, so it may be
// used as the final text.
func TestUsableFinalTextAllowsQuotedCall(t *testing.T) {
	if !usableFinalText("```xml\n<invoke name=\"read\"></invoke>\n```") {
		t.Fatal("a code-fenced example must remain a usable final answer")
	}
	if usableFinalText("<invoke name=\"read\"></invoke>") {
		t.Fatal("a bare leaked call must not be a usable final answer")
	}
	if usableFinalText("") {
		t.Fatal("empty text must not be usable")
	}
}

// On a truncated stream the partial text survives only if it is not a leaked
// call; the same detector gates both paths.
func TestFromLLMTruncatedReplyKeepsProse(t *testing.T) {
	truncated := context.DeadlineExceeded

	prose := fromLLM(nil, "正文里提到 tool_use 这个词只是说明。", truncated)
	if prose.IsError {
		t.Fatalf("prose should survive truncation: %+v", prose)
	}
	if prose.ActionInput != "正文里提到 tool_use 这个词只是说明。" {
		t.Fatalf("ActionInput = %q", prose.ActionInput)
	}

	leaked := fromLLM(nil, `<invoke name="read">`, truncated)
	if !leaked.IsError {
		t.Fatalf("truncated leak should not be surfaced as an answer: %+v", leaked)
	}
}
