package eventbus

import (
	"context"
	"nekocode/runtime/internal/core"
	"testing"
	"time"
)

func TestSubscriberReplayFiltersAndContinuesWithLiveEvents(t *testing.T) {
	b := NewEventBus()
	defer b.Close()
	for i := 0; i < 5; i++ {
		b.Publish(core.Event{RunID: "other", Type: core.EventRunDone})
	}
	original := b.History(core.EventFilter{})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	ch, err := b.SubscribeReplayEvents(ctx, core.EventFilter{RunID: "target", After: 4, Types: []core.EventType{core.EventRunDone}}, []core.Event{
		{ID: "evt_3", RunID: "target", Type: core.EventRunDone},
		{ID: "evt_5", RunID: "target", Type: core.EventAssistantDelta},
		{ID: "evt_5", RunID: "target", Type: core.EventRunDone},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(b.History(core.EventFilter{})) != len(original) {
		t.Fatal("replay mutated shared cache")
	}
	live := b.Publish(core.Event{RunID: "target", Type: core.EventRunDone})
	for _, want := range []uint64{5, live.Sequence} {
		select {
		case ev, ok := <-ch:
			if !ok || ev.Sequence != want {
				t.Fatalf("replay sequence=%d open=%v want=%d", ev.Sequence, ok, want)
			}
		case <-ctx.Done():
			t.Fatal("replay lost historical or live event")
		}
	}
}
