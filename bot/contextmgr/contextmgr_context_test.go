package contextmgr

import (
	"strings"
	"testing"

	"nekocode/bot/provider/types"
	"nekocode/bot/reasoning"
	"nekocode/protocol"
)

func newContextManager() *Manager {
	return New(Config{SystemPrompt: "test prompt"})
}

func TestBuild_PassesNormal(t *testing.T) {
	m := newContextManager()
	m.Add("user", "hello")
	m.AddAssistant(types.Message{Content: "reply"})

	msgs := m.Build()
	if len(msgs) < 2 {
		t.Errorf("expected at least 2 messages in build output, got %d", len(msgs))
	}
}

func TestBuild_OrphanToolsDropped(t *testing.T) {
	m := newContextManager()
	m.AddToolResultsBatch([]ToolResultMsg{
		{Message: types.Message{Content: "orphan content", ToolCallID: "orphan-id"}, ToolName: "read"},
	})

	msgs := m.Build()
	for _, msg := range msgs {
		if msg.Role == "tool" && msg.ToolCallID == "orphan-id" {
			t.Error("orphan tool result should have been filtered out")
		}
	}
}

func TestBuild_AssistantWithToolCalls(t *testing.T) {
	m := newContextManager()
	m.AddAssistant(types.Message{Content: "I'll read", ToolCalls: []types.ToolCall{
		{ID: "tc1", Type: "function", Function: types.FunctionCall{Name: "read", Arguments: "{}"}},
	}})
	m.AddToolResultsBatch([]ToolResultMsg{
		{Message: types.Message{Content: "file content", ToolCallID: "tc1"}, ToolName: "read"},
	})

	msgs := m.Build()
	foundAsst, foundTool := false, false
	for _, msg := range msgs {
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			foundAsst = true
		}
		if msg.Role == "tool" && msg.ToolCallID == "tc1" {
			foundTool = true
		}
	}
	if !foundAsst || !foundTool {
		t.Error("valid assistant->tool chain should be preserved")
	}
}

func TestBuildRequestProjectsReasoningWithoutMutatingSession(t *testing.T) {
	m := New(Config{Reasoning: types.ReasoningSettings{Replay: reasoning.ReplayToolCalls}})
	m.AddAssistant(types.Message{Content: "answer", ReasoningContent: "private"})
	m.AddAssistant(types.Message{ReasoningContent: "tool reasoning", ToolCalls: []types.ToolCall{{ID: "call-1"}}})
	m.AddToolResultsBatch([]ToolResultMsg{{Message: types.Message{ToolCallID: "call-1", Content: "result"}, ToolName: "read"}})

	request := m.BuildRequest(ModelRequest{})
	if request[1].ReasoningContent != "" || request[2].ReasoningContent != "tool reasoning" {
		t.Fatalf("request reasoning projection = %+v", request)
	}
	stored := m.Snapshot().Messages
	if stored[0].ReasoningContent != "private" || stored[1].ReasoningContent != "tool reasoning" {
		t.Fatalf("session reasoning was mutated: %+v", stored)
	}
}

func TestBuild_EmptyContent(t *testing.T) {
	m := newContextManager()
	m.state.ctx.Messages = append(m.state.ctx.Messages, types.Message{Role: "assistant", Content: ""})

	msgs := m.Build()
	for _, msg := range msgs {
		if msg.Role == "assistant" && msg.Content == "" {
			t.Error("empty assistant messages should get '.' placeholder")
		}
	}
}

func TestBuild_IncludesArchiveAndActiveHistory(t *testing.T) {
	m := newContextManager()
	m.state.ctx.Archive = "summary of old context"
	m.state.ctx.Messages = []types.Message{
		{Role: "user", Content: "visible user"},
		{Role: "assistant", Content: "visible assistant"},
	}

	msgs := m.Build()
	foundArchive := false
	for _, msg := range msgs {
		if strings.Contains(msg.Content, `"kind":"archive"`) && strings.Contains(msg.Content, "summary of old context") {
			foundArchive = true
		}
	}
	if !foundArchive {
		t.Fatal("Build should include archive summary after compaction")
	}
	if !containsContent(msgs, "visible user") || !containsContent(msgs, "visible assistant") {
		t.Fatalf("Build should include visible history, got %+v", msgs)
	}
}

func TestBuild_EmptyState(t *testing.T) {
	msgs := newContextManager().Build()
	if len(msgs) == 0 {
		t.Error("Build should return at least system prompt")
	}
}

func TestBuildRequestAppendsRuntimePromptWithoutRewritingHistory(t *testing.T) {
	value := "runtime-one"
	m := New(Config{SystemPrompt: "test prompt", RuntimePrompt: func() string { return value }})
	m.Add("user", "hello")

	first := m.BuildRequest(ModelRequest{})
	if !containsSubstring(first, "runtime-one") {
		t.Fatalf("first build missing runtime prompt: %+v", first)
	}
	value = "runtime-two"
	second := m.BuildRequest(ModelRequest{})
	if !containsSubstring(second, "runtime-one") || !containsSubstring(second, "runtime-two") {
		t.Fatalf("runtime prompt was not refreshed: %+v", second)
	}
	if len(second) != len(first)+1 {
		t.Fatalf("changed runtime context must append one message: first=%d second=%d", len(first), len(second))
	}
	for i := range first {
		if first[i].Role != second[i].Role || first[i].Content != second[i].Content {
			t.Fatalf("request prefix rewritten at %d:\nfirst=%+v\nsecond=%+v", i, first[i], second[i])
		}
	}
	if strings.Contains(m.Snapshot().SystemPrompt, "runtime-two") {
		t.Fatal("runtime prompt leaked into session snapshot")
	}

	snap := m.Snapshot()
	m.Restore(snap)
	if msgs := m.BuildRequest(ModelRequest{}); !containsSubstring(msgs, "runtime-two") {
		t.Fatalf("runtime provider did not survive restore: %+v", msgs)
	}
}

func TestBuildRequestPlacesTaggedRuntimeContextAfterHistory(t *testing.T) {
	m := New(Config{SystemPrompt: "test prompt", RuntimePrompt: func() string { return "runtime" }})
	m.state.ctx.Memory = "memory"
	m.state.ctx.Archive = "archive"
	m.Add("user", "request")

	msgs := m.BuildRequest(ModelRequest{})
	want := []string{
		"test prompt",
		historicalDataContract,
		`{"context_data":{"content":"memory","kind":"memory"}}`,
		`{"context_data":{"content":"archive","kind":"archive"}}`,
		"request",
		renderRuntimeContext("runtime", "", ""),
	}
	if len(msgs) != len(want) {
		t.Fatalf("Build() returned %d messages, want %d: %+v", len(msgs), len(want), msgs)
	}
	for i, content := range want {
		if msgs[i].Content != content {
			t.Fatalf("message %d = %q, want %q", i, msgs[i].Content, content)
		}
	}
}

func TestBuildRequestAppendsHintsWithoutRepeatingRuntimeState(t *testing.T) {
	m := New(Config{SystemPrompt: "stable", RuntimePrompt: func() string { return "environment-secret" }})
	m.Add("user", "request")
	first := m.BuildRequest(ModelRequest{})

	m.SetHints("<runtime_policy_hints>only this hint</runtime_policy_hints>")
	second := m.BuildRequest(ModelRequest{})
	if len(second) != len(first)+1 {
		t.Fatalf("hint should append exactly one message: first=%d second=%d", len(first), len(second))
	}
	tail := second[len(second)-1]
	if tail.Role != "user" || tail.Source != types.MessageSourceHint {
		t.Fatalf("hint message routing = %+v", tail)
	}
	if strings.Contains(tail.Content, "environment-secret") || strings.Contains(tail.Content, "runtime_context") {
		t.Fatalf("hint message repeated unrelated runtime state: %s", tail.Content)
	}

	m.SetHints("")
	third := m.BuildRequest(ModelRequest{})
	if len(third) != len(second) {
		t.Fatalf("clearing a hint must not append an empty snapshot: second=%d third=%d", len(second), len(third))
	}
}

func TestBuildRequestKeepsPerRequestPolicyHintIsolated(t *testing.T) {
	m := New(Config{SystemPrompt: "stable", RuntimePrompt: func() string { return "environment-secret" }})
	m.Add("user", "request")
	first := m.BuildRequest(ModelRequest{})
	second := m.BuildRequest(ModelRequest{PolicyHints: "<runtime_policy_hints>policy only</runtime_policy_hints>"})

	if len(second) != len(first)+1 {
		t.Fatalf("policy hint should append exactly one message: first=%d second=%d", len(first), len(second))
	}
	tail := second[len(second)-1]
	if tail.Source != types.MessageSourceHint || strings.Contains(tail.Content, "environment-secret") || strings.Contains(tail.Content, "runtime_context") {
		t.Fatalf("per-request hint was not isolated: %+v", tail)
	}
}

func TestBuildRequestReappendsActiveHintAfterTruncate(t *testing.T) {
	m := New(Config{SystemPrompt: "stable"})
	m.Add("user", "first request")
	m.SetHints("one-shot hint")
	m.BuildRequest(ModelRequest{})
	m.SetHints("")
	m.BuildRequest(ModelRequest{})
	m.AddAssistant(types.Message{Content: "first response"})

	beforeRetry := m.Len()
	m.SetHints("one-shot hint")
	m.BuildRequest(ModelRequest{})
	m.TruncateTo(beforeRetry)

	m.BuildRequest(ModelRequest{})
	if got := m.Len(); got != beforeRetry+1 {
		t.Fatalf("active one-shot hint missing after truncate: messages=%d, want %d", got, beforeRetry+1)
	}
}

func TestBuildRequestDoesNotRepeatUnchangedRuntimeContext(t *testing.T) {
	m := New(Config{SystemPrompt: "stable", RuntimePrompt: func() string { return "runtime" }})
	m.Add("user", "request")
	first := m.BuildRequest(ModelRequest{})
	second := m.BuildRequest(ModelRequest{})
	if len(second) != len(first) {
		t.Fatalf("unchanged runtime context repeated: first=%d second=%d", len(first), len(second))
	}
}

func TestBuildRequestExplicitlyClearsFieldsWithEnvironmentPresent(t *testing.T) {
	m := New(Config{SystemPrompt: "stable", RuntimePrompt: func() string { return "<environment>cwd</environment>" }})
	m.SetTodos([]protocol.TodoItem{{Content: "verify", Status: "pending"}})
	m.SetRuntimePolicy("<plan-mode>plan only</plan-mode>")
	first := m.BuildRequest(ModelRequest{})
	m.SetTodos(nil)
	m.SetRuntimePolicy("")
	second := m.BuildRequest(ModelRequest{})
	tail := second[len(second)-1].Content
	for _, want := range []string{`todo_state state="empty"`, `runtime_policy_state state="inactive"`, "supersedes"} {
		if !strings.Contains(tail, want) {
			t.Fatalf("cleared runtime snapshot missing %q: %s", want, tail)
		}
	}
	if strings.Contains(tail, "verify") || strings.Contains(tail, "plan only") {
		t.Fatalf("cleared runtime snapshot retained stale state: %s", tail)
	}
	for i := range first {
		if first[i].Role != second[i].Role || first[i].Content != second[i].Content {
			t.Fatalf("clearing runtime state rewrote prefix at %d", i)
		}
	}
}

func containsContent(msgs []types.Message, content string) bool {
	for _, msg := range msgs {
		if msg.Content == content {
			return true
		}
	}
	return false
}

func containsSubstring(msgs []types.Message, content string) bool {
	for _, msg := range msgs {
		if strings.Contains(msg.Content, content) {
			return true
		}
	}
	return false
}

func TestConfigureModel(t *testing.T) {
	m := newContextManager()

	m.ConfigureModel(ModelContext{Window: 10000})
	if m.state.contextWindow != 10000 {
		t.Errorf("budget = %d, want 10000", m.state.contextWindow)
	}
	m.ConfigureModel(ModelContext{Window: 0})
	m.ConfigureModel(ModelContext{Window: -1})
	if m.state.contextWindow != 10000 {
		t.Errorf("non-positive budget should not change value: got %d", m.state.contextWindow)
	}
}

func TestAllTasksDone_Empty(t *testing.T) {
	if !newContextManager().Status().TasksDone {
		t.Error("empty todos should be 'done'")
	}
}

func TestLenAndStatus(t *testing.T) {
	m := newContextManager()
	m.Add("user", "hello")

	if n := m.Len(); n != 1 {
		t.Errorf("Len = %d, want 1", n)
	}
	if status := m.Status(); status.Tokens <= 0 {
		t.Error("tokens should be > 0 after adding messages")
	}
}

func TestTokenUsage(t *testing.T) {
	m := newContextManager()
	m.ConfigureModel(ModelContext{Window: 10000})
	if budget := m.Status().Budget; budget != 10000 {
		t.Errorf("budget = %d, want 10000", budget)
	}
}
