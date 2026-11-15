package contextmgr

import (
	"fmt"
	"nekocode/bot/provider/types"
	"strings"
	"testing"
)

func TestRuntimeEventHistoryCompactsWithoutAssistant(t *testing.T) {
	for _, withUser := range []bool{false, true} {
		t.Run(fmt.Sprint(withUser), func(t *testing.T) {
			called := false
			manager := New(Config{SystemPrompt: "stable", ContextWindow: 6000, Summarizer: func(messages []types.Message, _ string) (string, error) {
				called = true
				if len(messages) == 0 {
					t.Error("empty summary input")
				}
				return "<summary>Completed notifications, the user's original task and outstanding work remain available.</summary>", nil
			}})
			if withUser {
				manager.Add("user", "original task")
			}
			for i := 0; i < 20; i++ {
				manager.Add("user", fmt.Sprintf("event %d: %s", i, strings.Repeat("notification ", 300)), types.MessageSourceRuntimeEvent)
			}
			compacted, err := manager.AutoCompactIfNeeded(t.Context())
			if err != nil || !compacted || !called {
				t.Fatalf("compacted=%v called=%v err=%v", compacted, called, err)
			}
			snapshot := manager.Snapshot()
			if snapshot.Archive == "" || len(snapshot.Messages) == 0 || !strings.Contains(snapshot.Messages[len(snapshot.Messages)-1].Content, "event 19:") {
				t.Fatal("summary or latest event lost")
			}
			if _, err := manager.PrepareRequest(t.Context(), ModelRequest{}); err != nil {
				t.Fatalf("compacted history still cannot be admitted: %v", err)
			}
			for _, message := range snapshot.Messages {
				if message.Role == "assistant" {
					t.Fatal("compaction invented assistant history")
				}
				if message.Source == types.MessageSourceRuntimeEvent && message.IsConversationUser() {
					t.Fatal("event promoted to user authority")
				}
			}
		})
	}
}

func TestTwoRuntimeEventsCanCompactOlderOversizedEvent(t *testing.T) {
	manager := New(Config{ContextWindow: 6000, Summarizer: func([]types.Message, string) (string, error) {
		return "<summary>Older event details are retained in this archive with their remaining follow-up work.</summary>", nil
	}})
	manager.Add("user", strings.Repeat("older notification ", 3000), types.MessageSourceRuntimeEvent)
	manager.Add("user", "latest notification", types.MessageSourceRuntimeEvent)
	compacted, err := manager.AutoCompactIfNeeded(t.Context())
	if err != nil || !compacted {
		t.Fatalf("two-event history compacted=%v err=%v", compacted, err)
	}
	if got := manager.Snapshot().Messages; len(got) != 1 || got[0].Content != "latest notification" {
		t.Fatalf("recent event changed: %+v", got)
	}
}
