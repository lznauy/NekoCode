package contextmgr

import (
	"testing"

	"nekocode/llm/types"
)

func newManager() *Manager {
	return New(Config{SystemPrompt: "test prompt"})
}

func TestAdd(t *testing.T) {
	m := newManager()
	m.Add("user", "hello")
	m.Add("assistant", "world")

	if n := m.Len(); n != 2 {
		t.Errorf("Len = %d, want 2", n)
	}
	_, tokens, _ := m.Stats()
	if tokens <= 0 {
		t.Error("tokens should be > 0 after adding messages")
	}
}

func TestAddAssistantResponse(t *testing.T) {
	m := newManager()
	m.AddAssistantResponse("response text", "thinking...")
	if n := m.Len(); n != 1 {
		t.Errorf("Len = %d, want 1", n)
	}
}

func TestAddAssistantToolCall(t *testing.T) {
	m := newManager()
	m.AddAssistantToolCall("let me check", "", []types.ToolCall{
		{ID: "tc1", Type: "function", Function: types.FunctionCall{Name: "read", Arguments: `{}`}},
	})
	if n := m.Len(); n != 1 {
		t.Errorf("Len = %d, want 1", n)
	}
}

func TestAddToolResultsBatch(t *testing.T) {
	m := newManager()
	m.AddToolResultsBatch([]ToolResultMsg{
		{Message: types.Message{Content: "result1", ToolCallID: "tc1"}, ToolName: "read"},
		{Message: types.Message{Content: "result2", ToolCallID: "tc2"}, ToolName: "grep"},
	})
	if n := m.Len(); n != 2 {
		t.Errorf("Len = %d, want 2", n)
	}
}

func TestAddToolResult_NoToolCallID(t *testing.T) {
	m := newManager()
	m.AddToolResultsBatch([]ToolResultMsg{
		{Message: types.Message{Content: "orphan result", ToolCallID: ""}, ToolName: "unknown"},
	})
	if n := m.Len(); n != 1 {
		t.Errorf("Len = %d, want 1", n)
	}
}

func TestTruncateTo(t *testing.T) {
	m := newManager()
	for i := 0; i < 10; i++ {
		m.Add("user", "msg")
	}
	m.TruncateTo(5)
	if n := m.Len(); n != 5 {
		t.Errorf("Len = %d, want 5", n)
	}
}

func TestTruncateTo_Negative(t *testing.T) {
	m := newManager()
	m.Add("user", "hello")
	m.TruncateTo(-1) // negative → 0, clears all
	if n := m.Len(); n != 0 {
		t.Errorf("negative truncate clamps to 0: got %d, want 0", n)
	}
}

func TestTruncateTo_Beyond(t *testing.T) {
	m := newManager()
	m.Add("user", "hello")
	m.TruncateTo(100)
	if n := m.Len(); n != 1 {
		t.Errorf("beyond-length truncate should keep all: got %d", n)
	}
}

func TestRemoveMessages(t *testing.T) {
	m := newManager()
	for i := 0; i < 5; i++ {
		m.Add("user", "msg")
	}
	m.RemoveMessages(1, 2) // remove msg 1 and 2
	if n := m.Len(); n != 3 {
		t.Errorf("Len = %d, want 3", n)
	}
}

func TestRemoveMessages_InvalidRange(t *testing.T) {
	m := newManager()
	m.Add("user", "hello")
	m.RemoveMessages(0, 10) // end beyond length
	if n := m.Len(); n != 1 {
		t.Errorf("invalid range should not modify: got %d", n)
	}
	m.RemoveMessages(3, 1) // start > end
	if n := m.Len(); n != 1 {
		t.Errorf("reversed range should not modify: got %d", n)
	}
	m.RemoveMessages(-1, 0) // negative start
	if n := m.Len(); n != 1 {
		t.Errorf("negative start should not modify: got %d", n)
	}
}

func TestClear(t *testing.T) {
	m := newManager()
	m.Add("user", "hello")
	m.Add("assistant", "world")
	m.Clear()
	if n := m.Len(); n != 0 {
		t.Errorf("after Clear: Len = %d, want 0", n)
	}
}

func TestFreshStart(t *testing.T) {
	m := newManager()
	m.Add("user", "hello")
	m.FreshStart()
	if n := m.Len(); n != 0 {
		t.Errorf("after FreshStart: Len = %d, want 0", n)
	}
}

func TestSnapshotRestore(t *testing.T) {
	m := newManager()
	m.Add("user", "hello world")
	m.SetContextWindow(50000)

	snap := m.Snapshot()

	m2 := newManager()
	m2.Restore(snap)

	if n := m2.Len(); n != m.Len() {
		t.Errorf("restored Len = %d, want %d", n, m.Len())
	}
}

func TestSetContextWindow(t *testing.T) {
	m := newManager()
	m.SetContextWindow(10000)
	_, budget := m.TokenUsage()
	if budget != 10000 {
		t.Errorf("budget = %d, want 10000", budget)
	}
	m.SetContextWindow(0) // zero should be ignored
	_, budget = m.TokenUsage()
	if budget != 10000 {
		t.Errorf("zero should not change budget: got %d", budget)
	}
	m.SetContextWindow(-1) // negative should be ignored
	_, budget = m.TokenUsage()
	if budget != 10000 {
		t.Errorf("negative should not change budget: got %d", budget)
	}
}

func TestAllTasksDone_Empty(t *testing.T) {
	m := newManager()
	if !m.AllTasksDone() {
		t.Error("empty todos should be 'done'")
	}
}

func TestAutoCompactIfNeeded(t *testing.T) {
	m := newManager()
	m.SetContextWindow(10000)
	level, err := m.AutoCompactIfNeeded()
	if err != nil {
		t.Errorf("AutoCompactIfNeeded error: %v", err)
	}
	if level < 0 {
		t.Errorf("unexpected level: %d", level)
	}
}

func TestBuild_WithTools(t *testing.T) {
	m := newManager()
	m.SetContextWindow(100000)
	m.Add("user", "hello")
	msgs := m.Build(true)
	if len(msgs) == 0 {
		t.Error("Build should produce messages")
	}
}

func TestBuild_WithoutTools(t *testing.T) {
	m := newManager()
	m.Add("user", "hello")
	msgs := m.Build(false)
	if len(msgs) == 0 {
		t.Error("Build should produce messages even without tools")
	}
}
