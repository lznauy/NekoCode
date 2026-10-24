package eventbus

import (
	"context"
	"testing"
	"time"

	"nekocode/runtime/internal/core"
)

func TestHistoryWrapImportAndReplay(t *testing.T) {
	bus := NewEventBus()
	bus.historyLimit = 3
	defer bus.Close()
	for i := 0; i < 8; i++ {
		bus.Publish(core.Event{Type: core.EventAssistantDelta})
	}
	before := bus.History(core.EventFilter{})
	bus.ImportHistory([]core.Event{{ID: "evt_20", Type: core.EventRunDone}, {ID: "evt_21", Type: core.EventRunDone}})
	history := bus.History(core.EventFilter{})
	for i, want := range []uint64{8, 20, 21} {
		if len(history) != 3 || history[i].Sequence != want {
			t.Fatalf("history = %+v", history)
		}
	}
	if before[0].Sequence != 6 {
		t.Fatalf("previous history changed: %+v", before)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	events, err := bus.SubscribeReplay(ctx, core.EventFilter{After: 8, Types: []core.EventType{core.EventRunDone}})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []uint64{20, 21} {
		select {
		case ev := <-events:
			if ev.Sequence != want {
				t.Fatalf("replay = %d, want %d", ev.Sequence, want)
			}
		case <-ctx.Done():
			t.Fatal("replay timed out")
		}
	}
	ev := bus.Publish(core.Event{Type: core.EventRunDone})
	if ev.Sequence != 22 {
		t.Fatalf("next sequence = %d", ev.Sequence)
	}
}

func BenchmarkPublishFullHistory(b *testing.B) {
	bus := NewEventBus()
	defer bus.Close()
	ev := core.Event{Type: core.EventAssistantDelta, Payload: core.DeltaPayload{Delta: "abcdefghijklmnop"}}
	for i := 0; i < defaultEventHistoryLimit; i++ {
		bus.Publish(ev)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bus.Publish(ev)
	}
}
