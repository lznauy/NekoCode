package contextmgr

import (
	"strings"
	"testing"
	"unicode/utf8"

	"nekocode/bot/contextmgr/token"
	"nekocode/bot/provider/types"
)

func newHistoryManager() *Manager {
	return New(Config{SystemPrompt: "test prompt"})
}

// The 4 MiB cut must land on a rune boundary: a split multi-byte rune would be
// re-serialized as U+FFFD and corrupt the transcript record.
func TestCapTranscriptContentCutsOnRuneBoundary(t *testing.T) {
	// Repeat a 3-byte rune so the limit cannot fall on a boundary by luck.
	head := strings.Repeat("世", transcriptContentLimit/3)
	// Pad to make the byte length straddle the limit inside a rune.
	content := head + "世世"
	if len(content) <= transcriptContentLimit {
		t.Fatalf("test content must exceed the limit: %d", len(content))
	}
	capped := capTranscriptContent(content)
	body := strings.TrimSuffix(capped, "\n... [transcript record capped at 4 MiB]")
	if !utf8.ValidString(body) {
		t.Fatalf("capped content is not valid UTF-8")
	}
	if len(body) >= len(content) {
		t.Fatalf("capped content was not truncated: %d", len(body))
	}
}

func TestAdd(t *testing.T) {
	m := newHistoryManager()
	m.Add("user", "hello")
	m.Add("assistant", "world")

	if n := len(m.Snapshot().Messages); n != 2 {
		t.Errorf("len = %d, want 2", n)
	}
}

func TestAddAssistantResponse(t *testing.T) {
	m := newHistoryManager()
	m.AddAssistant(types.Message{Content: "response text", ReasoningContent: "thinking...", ReasoningSignature: "sig"})
	snapshot := m.Snapshot()
	if n := len(snapshot.Messages); n != 1 {
		t.Errorf("len = %d, want 1", n)
	}
	if got := snapshot.Messages[0]; got.Role != "assistant" || got.ReasoningContent != "thinking..." || got.ReasoningSignature != "sig" {
		t.Fatalf("stored assistant = %+v", got)
	}
}

func TestAddAssistantToolCall(t *testing.T) {
	m := newHistoryManager()
	m.AddAssistant(types.Message{Content: "let me check", ToolCalls: []types.ToolCall{
		{ID: "tc1", Type: "function", Function: types.FunctionCall{Name: "read", Arguments: `{}`}},
	}})
	if n := len(m.Snapshot().Messages); n != 1 {
		t.Errorf("len = %d, want 1", n)
	}
}

func TestAddToolResultsBatch(t *testing.T) {
	m := newHistoryManager()
	m.AddToolResultsBatch([]ToolResultMsg{
		{Message: types.Message{Content: "result1", ToolCallID: "tc1"}, ToolName: "read"},
		{Message: types.Message{Content: "result2", ToolCallID: "tc2"}, ToolName: "grep"},
	})
	if n := len(m.Snapshot().Messages); n != 2 {
		t.Errorf("len = %d, want 2", n)
	}
}

func TestAddToolResult_NoToolCallID(t *testing.T) {
	m := newHistoryManager()
	m.AddToolResultsBatch([]ToolResultMsg{
		{Message: types.Message{Content: "orphan result", ToolCallID: ""}, ToolName: "unknown"},
	})
	if n := len(m.Snapshot().Messages); n != 1 {
		t.Errorf("len = %d, want 1", n)
	}
	if role := m.Snapshot().Messages[0].Role; role != "user" {
		t.Errorf("role = %q, want user", role)
	}
}

func TestTruncateTo(t *testing.T) {
	m := newHistoryManager()
	for i := 0; i < 10; i++ {
		m.Add("user", "msg")
	}
	m.TruncateTo(5)
	if n := len(m.Snapshot().Messages); n != 5 {
		t.Errorf("len = %d, want 5", n)
	}
}

func TestTruncateTo_Negative(t *testing.T) {
	m := newHistoryManager()
	m.Add("user", "hello")
	m.TruncateTo(-1)
	if n := len(m.Snapshot().Messages); n != 0 {
		t.Errorf("negative truncate clamps to 0: got %d, want 0", n)
	}
}

func TestTruncateTo_Beyond(t *testing.T) {
	m := newHistoryManager()
	m.Add("user", "hello")
	m.TruncateTo(100)
	if n := len(m.Snapshot().Messages); n != 1 {
		t.Errorf("beyond-length truncate should keep all: got %d", n)
	}
}

func TestRemoveMessages(t *testing.T) {
	m := newHistoryManager()
	for i := 0; i < 5; i++ {
		m.Add("user", "msg")
	}
	m.RemoveMessages(1, 2)
	if n := len(m.Snapshot().Messages); n != 3 {
		t.Errorf("len = %d, want 3", n)
	}
}

func TestRemoveMessages_InvalidRange(t *testing.T) {
	m := newHistoryManager()
	m.Add("user", "hello")
	m.RemoveMessages(0, 10)
	m.RemoveMessages(3, 1)
	m.RemoveMessages(-1, 0)
	if n := len(m.Snapshot().Messages); n != 1 {
		t.Errorf("invalid ranges should not modify: got %d", n)
	}
}

func TestClear(t *testing.T) {
	m := newHistoryManager()
	m.Add("user", "hello")
	m.Add("assistant", "world")
	m.state.ctx.Archive = "old summary"
	m.state.ctx.Hints = "old hints"
	m.Reset()
	snapshot := m.Snapshot()
	if n := len(snapshot.Messages); n != 0 {
		t.Errorf("after Clear: len = %d, want 0", n)
	}
	if snapshot.Archive != "" || snapshot.Hints != "" || snapshot.Tracker != (token.State{}) {
		t.Fatalf("Clear retained conversation state: %#v", snapshot)
	}
}
