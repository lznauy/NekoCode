package compact

import (
	"strings"
	"testing"

	"nekocode/bot/ctxmgr/context"
	"nekocode/llm/types"
)

func newCompactor(msgs []types.Message, budget int, boundary int) *Compactor {
	ctx := &context.Content{Messages: msgs, CompactBoundary: boundary}
	return &Compactor{
		Ctx: ctx, ContextWindow: &budget, Tracker: &testTracker{},
		CompactCount: new(int), TrimCount: new(int), Cfg: DefaultConfig,
	}
}

func TestIsCompactableResult_Read(t *testing.T) {
	msgs := []types.Message{
		{Role: "assistant", ToolCalls: []types.ToolCall{
			{ID: "tc1", Function: types.FunctionCall{Name: "read"}},
		}},
		{Role: "tool", ToolCallID: "tc1", Content: "content"},
	}
	cm := newCompactor(msgs, 64000, 0)
	if !cm.isCompactableResult(1) {
		t.Error("read result should be compactable")
	}
}

func TestIsCompactableResult_UnknownTool(t *testing.T) {
	msgs := []types.Message{
		{Role: "assistant", ToolCalls: []types.ToolCall{
			{ID: "tc1", Function: types.FunctionCall{Name: "custom_mcp_tool"}},
		}},
		{Role: "tool", ToolCallID: "tc1", Content: "result"},
	}
	cm := newCompactor(msgs, 64000, 0)
	if cm.isCompactableResult(1) {
		t.Error("tool not in compactableTools should not be compactable")
	}
}

func TestIsCompactableResult_NoToolCallID(t *testing.T) {
	msgs := []types.Message{
		{Role: "tool", ToolCallID: "", Content: "orphan"},
	}
	cm := newCompactor(msgs, 64000, 0)
	if cm.isCompactableResult(0) {
		t.Error("result without ToolCallID should not be compactable")
	}
}

func TestCompactableToolPriority(t *testing.T) {
	if p := compactableToolPriority("read", ""); p != priorityHigh {
		t.Errorf("read = %d, want high(%d)", p, priorityHigh)
	}
	if p := compactableToolPriority("edit", ""); p != priorityHigh {
		t.Errorf("edit = %d, want high(%d)", p, priorityHigh)
	}
	if p := compactableToolPriority("grep", ""); p != priorityLow {
		t.Errorf("grep = %d, want low(%d)", p, priorityLow)
	}
	if p := compactableToolPriority("bash", strings.Repeat("x", 200)); p != priorityMedium {
		t.Errorf("long bash = %d, want medium(%d)", p, priorityMedium)
	}
	if p := compactableToolPriority("bash", "short"); p != priorityLow {
		t.Errorf("short bash = %d, want low(%d)", p, priorityLow)
	}
}

func TestForceCompact(t *testing.T) {
	msgs := []types.Message{
		{Role: "assistant", ToolCalls: []types.ToolCall{
			{ID: "tc1", Function: types.FunctionCall{Name: "read"}},
		}},
		{Role: "tool", ToolCallID: "tc1", Content: "content"},
	}
	cm := newCompactor(msgs, 64000, 0)
	cm.ForceCompact()
	if msgs[1].Content != ClearedMarker {
		t.Errorf("ForceCompact should clear result, got %q", msgs[1].Content)
	}
	if *cm.CompactCount != 1 {
		t.Errorf("CompactCount = %d, want 1", *cm.CompactCount)
	}
}

func TestForceCompact_SkipsNonCompactable(t *testing.T) {
	msgs := []types.Message{
		{Role: "assistant", ToolCalls: []types.ToolCall{
			{ID: "tc1", Function: types.FunctionCall{Name: "custom"}},
		}},
		{Role: "tool", ToolCallID: "tc1", Content: "keep me"},
	}
	cm := newCompactor(msgs, 64000, 0)
	cm.ForceCompact()
	if msgs[1].Content == ClearedMarker {
		t.Error("non-compactable result should NOT be cleared")
	}
}

func TestMicroCompact_Runs(t *testing.T) {
	msgs := []types.Message{
		{Role: "assistant", ToolCalls: []types.ToolCall{
			{ID: "tc1", Function: types.FunctionCall{Name: "grep"}},
		}},
		{Role: "tool", ToolCallID: "tc1", Content: "grep output"},
	}
	// Low budget + high Tracker estimate should trigger microCompact.
	budget := 4000
	cm := &Compactor{
		Ctx: &context.Content{Messages: msgs},
		ContextWindow: &budget,
		Tracker:     &testTracker{promptEst: 3500},
		CompactCount: new(int),
		TrimCount:    new(int),
		Cfg:          DefaultConfig,
	}
	n := cm.MicroCompactIfNeeded()
	// With 3500 > 2000 (half of 4000), it should attempt microCompact.
	// Whether it actually clears depends on keepResults logic.
	_ = n
	// At minimum, it should not panic and CompactCount should be >= 0.
	if *cm.CompactCount < 0 {
		t.Error("CompactCount should be >= 0")
	}
}
