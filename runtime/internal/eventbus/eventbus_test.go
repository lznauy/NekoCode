package eventbus

import (
	"context"
	"fmt"
	"testing"
	"time"

	"nekocode/protocol"
	"nekocode/runtime/internal/core"
	"nekocode/runtime/internal/runstore"
)

func TestCompactionBacklogDropsDeltasButKeepsFullSummary(t *testing.T) {
	s := &subscriber{wake: make(chan struct{}, 1)}
	for i := 0; i < defaultSubscriberQueueLimit*2; i++ {
		s.enqueue(core.Event{Type: core.EventCompaction, Payload: protocol.CompactionEvent{ID: "compact", Status: "delta", Delta: "chunk"}})
	}
	if len(s.queue) != defaultSubscriberQueueLimit {
		t.Fatalf("unbounded delta queue: %d", len(s.queue))
	}
	s.enqueue(core.Event{Type: core.EventCompaction, Payload: protocol.CompactionEvent{ID: "compact", Status: "completed", Summary: "full summary"}})
	last := s.queue[len(s.queue)-1].Payload.(protocol.CompactionEvent)
	if len(s.queue) != defaultSubscriberQueueLimit || last.Summary != "full summary" {
		t.Fatalf("terminal summary was dropped: %+v", last)
	}
}

func TestEventBusFiltersByRunAndType(t *testing.T) {
	bus := NewEventBus()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events, err := bus.Subscribe(ctx, core.EventFilter{
		RunID: "run_1",
		Types: []core.EventType{core.EventRunDone},
	})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	bus.Publish(core.Event{RunID: "run_2", Type: core.EventRunDone})
	bus.Publish(core.Event{RunID: "run_1", Type: core.EventRunStarted})
	want := bus.Publish(core.Event{RunID: "run_1", Type: core.EventRunDone})

	select {
	case got := <-events:
		if got.ID != want.ID {
			t.Fatalf("event id = %q, want %q", got.ID, want.ID)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for filtered event")
	}
}

func TestEventBusSubscribeReplayIncludesHistoryAndLiveEvents(t *testing.T) {
	bus := NewEventBus()
	historical := bus.Publish(core.Event{RunID: "run_1", Type: core.EventSystemMessage, Payload: core.MessagePayload{Content: "old"}})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, err := bus.SubscribeReplay(ctx, core.EventFilter{RunID: "run_1"})
	if err != nil {
		t.Fatalf("SubscribeReplay: %v", err)
	}

	live := bus.Publish(core.Event{RunID: "run_1", Type: core.EventRunDone, Payload: core.RunResult{Output: "new"}})

	gotHistorical := <-events
	if gotHistorical.ID != historical.ID {
		t.Fatalf("first replay id = %q, want %q", gotHistorical.ID, historical.ID)
	}
	gotLive := <-events
	if gotLive.ID != live.ID {
		t.Fatalf("live id = %q, want %q", gotLive.ID, live.ID)
	}
}

func TestEventBusImportHistoryAdvancesEventID(t *testing.T) {
	bus := NewEventBus()
	bus.ImportHistory([]core.Event{{ID: "evt_41", RunID: "run_1", Type: core.EventRunStarted}})
	ev := bus.Publish(core.Event{RunID: "run_1", Type: core.EventRunDone})
	if ev.ID != "evt_42" {
		t.Fatalf("event id after import = %q, want evt_42", ev.ID)
	}
}

func TestEventBusImportHistoryDropsCompactionDeltas(t *testing.T) {
	bus := NewEventBus()
	events := []core.Event{{ID: "evt_1", Type: core.EventRunStarted}}
	for i := 0; i < defaultEventHistoryLimit+10; i++ {
		events = append(events, core.Event{
			ID:   fmt.Sprintf("evt_%d", i+2),
			Type: core.EventCompaction,
			Payload: protocol.CompactionEvent{
				ID: "compact", Status: protocol.CompactionDelta, Delta: "chunk",
			},
		})
	}
	events = append(events, core.Event{
		ID: "evt_2000", Type: core.EventCompaction,
		Payload: protocol.CompactionEvent{ID: "compact", Status: protocol.CompactionCompleted, Summary: "full"},
	})

	bus.ImportHistory(events)
	history := bus.History(core.EventFilter{})
	if len(history) != 2 || history[0].Type != core.EventRunStarted {
		t.Fatalf("imported history retained transient deltas or lost boundary events: %+v", history)
	}
	if payload, ok := history[1].Payload.(protocol.CompactionEvent); !ok || payload.Status != protocol.CompactionCompleted {
		t.Fatalf("terminal compaction event missing: %#v", history[1])
	}
}

func TestEventBusDoesNotDropApprovalResolvedWhenSubscriberBacklogged(t *testing.T) {
	bus := NewEventBus()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, err := bus.Subscribe(ctx, core.EventFilter{})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	for range 256 {
		bus.Publish(core.Event{RunID: "run_1", Type: core.EventToolStarted})
	}
	resolved := bus.Publish(core.Event{RunID: "run_1", Type: core.EventApprovalResolved})

	for {
		select {
		case got := <-events:
			if got.ID == resolved.ID {
				return
			}
		case <-time.After(time.Second):
			t.Fatal("approval_resolved was dropped from a backlogged subscriber")
		}
	}
}

func TestEventBusPreservesCriticalBacklogOrder(t *testing.T) {
	bus := NewEventBus()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, err := bus.Subscribe(ctx, core.EventFilter{})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	published := make([]core.Event, 0, defaultSubscriberBuffer+1)
	for range defaultSubscriberBuffer {
		published = append(published, bus.Publish(core.Event{
			RunID: "run_1",
			Type:  core.EventApprovalRequested,
		}))
	}
	published = append(published, bus.Publish(core.Event{
		RunID: "run_1",
		Type:  core.EventRunDone,
	}))

	for i, want := range published {
		select {
		case got := <-events:
			if got.ID != want.ID {
				t.Fatalf("event %d = %q, want %q", i, got.ID, want.ID)
			}
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for event %d", i)
		}
	}
}

func TestEventBusDoesNotDropSessionChangedWhenSubscriberBacklogged(t *testing.T) {
	bus := NewEventBus()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, err := bus.Subscribe(ctx, core.EventFilter{})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	for range 256 {
		bus.Publish(core.Event{RunID: "run_1", Type: core.EventAssistantDelta})
	}
	changed := bus.Publish(core.Event{
		Type:    core.EventSessionChanged,
		Payload: core.SessionPayload{ID: "session_2"},
	})

	for {
		select {
		case got := <-events:
			if got.ID != changed.ID {
				continue
			}
			payload, ok := got.Payload.(core.SessionPayload)
			if !ok || payload.ID != "session_2" {
				t.Fatalf("session payload = %#v", got.Payload)
			}
			return
		case <-time.After(time.Second):
			t.Fatal("session_changed was dropped from a backlogged subscriber")
		}
	}
}

func TestEventBusObserverReceivesPublishedEvent(t *testing.T) {
	bus := NewEventBus()
	store := runstore.NewRunStore(0)
	bus.AddObserver(store.Record)

	bus.Publish(core.Event{
		RunID:   "run_1",
		Type:    core.EventRunDone,
		Payload: core.RunResult{Output: "ok"},
	})

	view, ok := store.Lookup("run_1")
	if !ok {
		t.Fatal("observer did not record event")
	}
	if view.Status != core.RunDone || view.Output != "ok" {
		t.Fatalf("view = %#v", view)
	}
}

func TestEventBusObserverCanQueryHistory(t *testing.T) {
	bus := NewEventBus()
	observed := make(chan int, 1)
	bus.AddObserver(func(core.Event) {
		observed <- len(bus.History(core.EventFilter{RunID: "run_1"}))
	})

	published := make(chan struct{})
	go func() {
		defer close(published)
		bus.Publish(core.Event{RunID: "run_1", Type: core.EventRunDone})
	}()

	select {
	case count := <-observed:
		if count != 1 {
			t.Fatalf("history count = %d, want 1", count)
		}
	case <-time.After(time.Second):
		t.Fatal("observer could not query history")
	}
	select {
	case <-published:
	case <-time.After(time.Second):
		t.Fatal("publish did not return")
	}
}

func TestEventBusRejectsSubscribeAfterClose(t *testing.T) {
	bus := NewEventBus()
	bus.Close()

	if _, err := bus.Subscribe(context.Background(), core.EventFilter{}); err == nil {
		t.Fatal("Subscribe after Close succeeded")
	}
}

func TestEventBusCloseWaitsForObservers(t *testing.T) {
	bus := NewEventBus()
	started := make(chan struct{})
	release := make(chan struct{})
	bus.AddObserver(func(core.Event) {
		close(started)
		<-release
	})

	go bus.Publish(core.Event{Type: core.EventRunStarted})
	<-started
	closed := make(chan struct{})
	go func() {
		bus.Close()
		close(closed)
	}()

	select {
	case <-closed:
		t.Fatal("Close returned while observer was still running")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("Close did not return after observer completed")
	}
}
