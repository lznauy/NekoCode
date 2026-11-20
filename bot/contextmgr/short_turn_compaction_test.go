package contextmgr

import (
	"fmt"
	"nekocode/bot/provider/types"
	"strings"
	"testing"
)

func TestCompactsLongHistoryWithFewUserTurns(t *testing.T) {
	for _, turns := range []int{1, 2, 3} {
		t.Run(fmt.Sprint(turns), func(t *testing.T) {
			called := false
			s := newReplacementCompactor(func([]types.Message, string) (string, error) {
				called = true
				return "<summary>The user's original task and the completed investigation are preserved here.</summary>", nil
			}, 80)
			var history []types.Message
			for i := 0; i < turns; i++ {
				history = append(history, types.Message{Role: "user", Content: "task"})
				for j := 0; j < 8; j++ {
					id := fmt.Sprintf("call-%d-%d", i, j)
					history = append(history,
						types.Message{Role: "assistant", ToolCalls: []types.ToolCall{{ID: id}}},
						types.Message{Role: "tool", ToolCallID: id, Content: strings.Repeat("evidence ", 500)},
						types.Message{Role: "user", Source: types.MessageSourceRuntimeEvent, Content: "notification"})
				}
			}
			archive, recent, trimmed, err := s.summarize(history, "", 6000, nil, nil)
			if err != nil || !called || trimmed <= 0 || archive == "" {
				t.Fatalf("called=%v trimmed=%d err=%v", called, trimmed, err)
			}
			calls := map[string]bool{}
			for _, msg := range recent {
				for _, call := range msg.ToolCalls {
					calls[call.ID] = true
				}
				if msg.Role == "tool" && !calls[msg.ToolCallID] {
					t.Fatalf("orphaned result %s", msg.ToolCallID)
				}
			}
		})
	}
}

func TestShortTurnCompactionPreservesParallelToolExchange(t *testing.T) {
	history := []types.Message{
		{Role: "user", Content: "original task"},
		{Role: "assistant", Content: strings.Repeat("older work ", 3000)},
		{Role: "assistant", ToolCalls: []types.ToolCall{{ID: "a"}, {ID: "b"}}},
		{Role: "tool", ToolCallID: "a", Content: "first result"},
		{Role: "user", Source: types.MessageSourceRuntimeEvent, Content: "interrupt"},
		{Role: "tool", ToolCallID: "b", Content: "second result"},
	}
	s := newReplacementCompactor(nil, 80)
	start := s.recentStart(history, 6000)
	if start != 2 {
		t.Fatalf("boundary=%d, want complete parallel exchange at 2", start)
	}
	// Unanswered tool calls must also remain in active history.
	start = s.recentStart(history[:5], 6000)
	if start != 2 {
		t.Fatalf("pending exchange boundary=%d, want 2", start)
	}
}

func TestShortHistoryRetainsUserTurns(t *testing.T) {
	s := newReplacementCompactor(nil, 80)
	history := []types.Message{{Role: "user", Content: "task"}, {Role: "assistant", Content: "working"}, {Role: "user", Source: types.MessageSourceRuntimeEvent, Content: "notification"}}
	if start := s.recentStart(history, 6000); start != 0 {
		t.Fatalf("short history boundary=%d", start)
	}
}

func TestPrepareRequestCompactsSingleLongUserTurn(t *testing.T) {
	called := false
	m := New(Config{SystemPrompt: "system", ContextWindow: 16000, Summarizer: func([]types.Message, string) (string, error) {
		called = true
		return "<summary>Original delegated task, completed evidence, and outstanding work are retained.</summary>", nil
	}})
	m.Add("user", "delegated task")
	for i := 0; i < 20; i++ {
		m.AddAssistant(types.Message{Content: strings.Repeat("completed analysis ", 800)})
		m.Add("user", "notification", types.MessageSourceRuntimeEvent)
	}
	messages, err := m.PrepareRequest(t.Context(), ModelRequest{})
	if err != nil || !called || len(messages) == 0 {
		t.Fatalf("called=%v messages=%d err=%v", called, len(messages), err)
	}
}
