package eventbus

import (
	"context"
	"nekocode/runtime/internal/core"
	"strings"
	"testing"
	"time"
)

func TestOversizedEventDeliveredWithoutRetainingHistory(t *testing.T) {
	b := NewEventBus()
	defer b.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := b.Subscribe(ctx, core.EventFilter{})
	if err != nil {
		t.Fatal(err)
	}
	content := strings.Repeat("x", defaultEventHistoryBytes+1)
	b.Publish(core.Event{Type: core.EventAssistantMessage, Payload: core.MessagePayload{Content: content}})
	select {
	case ev := <-ch:
		if ev.Payload.(core.MessagePayload).Content != content {
			t.Fatal("live delivery truncated")
		}
	case <-time.After(time.Second):
		t.Fatal("live delivery lost")
	}
	if len(b.History(core.EventFilter{})) != 0 {
		t.Fatal("oversized event retained")
	}
	for i := 0; i < 5; i++ {
		b.Publish(core.Event{Type: core.EventAssistantDelta, Payload: core.DeltaPayload{Delta: strings.Repeat("y", 3<<20)}})
	}
	if b.historyBytes > defaultEventHistoryBytes || len(b.History(core.EventFilter{})) > 2 {
		t.Fatal("history byte budget exceeded")
	}
}
