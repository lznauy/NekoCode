package compact

import (
	"testing"

	"nekocode/llm/types"
)

func TestCountMessagesForLastNTurns(t *testing.T) {
	msgs := []types.Message{
		{Role: "user", Content: "q1"}, {Role: "assistant", Content: "a1"},
		{Role: "user", Content: "q2"}, {Role: "assistant", Content: "a2"},
	}
	cm := newCompactor(msgs, 64000, 0)
	if n := cm.countMessagesForLastNTurns(1); n != 2 {
		t.Errorf("1 turn = %d, want 2", n)
	}
	if n := cm.countMessagesForLastNTurns(2); n != 4 {
		t.Errorf("2 turns = %d, want 4", n)
	}
	if n := cm.countMessagesForLastNTurns(0); n != 0 {
		t.Error("0 turns should return 0")
	}
}

func TestTrimOldMessages(t *testing.T) {
	msgs := make([]types.Message, 250)
	for i := range msgs {
		msgs[i] = types.Message{Role: "user", Content: "x"}
	}
	cm := newCompactor(msgs, 64000, 210)
	oldLen := len(cm.Ctx.Messages)
	cm.trimOldMessages()
	if cm.Ctx.CompactBoundary != 200 {
		t.Errorf("boundary should shrink to 200, got %d", cm.Ctx.CompactBoundary)
	}
	if len(cm.Ctx.Messages) >= oldLen {
		t.Error("should trim messages from front")
	}
}

func TestTrimOldMessages_NoOp(t *testing.T) {
	msgs := make([]types.Message, 50)
	for i := range msgs {
		msgs[i] = types.Message{Role: "user", Content: "x"}
	}
	cm := newCompactor(msgs, 64000, 30)
	oldLen := len(cm.Ctx.Messages)
	cm.trimOldMessages()
	if len(cm.Ctx.Messages) != oldLen {
		t.Error("boundary ≤ 200: should not trim")
	}
}
