package compact

import (
	"testing"

	"nekocode/llm/types"
)

func TestSnipHistory_NoOp(t *testing.T) {
	msgs := make([]types.Message, 50)
	for i := range msgs {
		msgs[i] = types.Message{Role: "user", Content: "x"}
	}
	cm := newCompactor(msgs, 64000, 50)
	n := cm.SnipHistory()
	if n != 0 {
		t.Errorf("boundary=50 ≤ 60: should not snip, got %d", n)
	}
}

func TestSnipHistory_Snips(t *testing.T) {
	msgs := make([]types.Message, 100)
	for i := range msgs {
		msgs[i] = types.Message{Role: "user", Content: "x"}
	}
	cm := newCompactor(msgs, 64000, 80)
	n := cm.SnipHistory()
	if n <= 0 {
		t.Error("boundary=80 > 60: should snip")
	}
	if cm.Ctx.CompactBoundary > 80 {
		t.Error("boundary should decrease after snip")
	}
}

func TestSnipHistory_BoundaryMarker(t *testing.T) {
	msgs := make([]types.Message, 80)
	for i := range msgs {
		msgs[i] = types.Message{Role: "user", Content: "x"}
	}
	cm := newCompactor(msgs, 64000, 80)
	cm.SnipHistory()
	found := false
	for _, m := range cm.Ctx.Messages {
		if m.Content == snipeBoundaryMarker {
			found = true
			break
		}
	}
	if !found {
		t.Error("snipe should insert boundary marker")
	}
}
