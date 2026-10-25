package eventbus

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	"nekocode/protocol"
	"nekocode/runtime/internal/core"
	"nekocode/runtime/internal/runstore"
)

func TestOrderedObserversKeepConcurrentProjectionInReplayOrder(t *testing.T) {
	bus := NewEventBus()
	defer bus.Close()
	store := runstore.NewRunStore(0)
	var appliedMu sync.Mutex
	var applied []uint64
	bus.AddOrderedObserver(func(ev core.Event) {
		if ev.Sequence%2 == 1 {
			runtime.Gosched()
		}
		appliedMu.Lock()
		defer appliedMu.Unlock()
		store.Record(ev)
		applied = append(applied, ev.Sequence)
	})
	var workers sync.WaitGroup
	for range 32 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for range 100 {
				bus.Publish(core.Event{RunID: "run", Type: core.EventAssistantDelta, Payload: core.DeltaPayload{Delta: "x"}})
			}
		}()
	}
	workers.Wait()
	for i := 1; i < len(applied); i++ {
		if applied[i] <= applied[i-1] {
			t.Fatalf("projection reordered replay events: %d then %d", applied[i-1], applied[i])
		}
	}
	// UpdatedAt belongs to the final replay event only when the projection
	// observes exactly the same order as subscribers and archived history.
	history := bus.History(core.EventFilter{})
	snapshot, _ := store.Lookup("run")
	if !snapshot.UpdatedAt.Equal(history[len(history)-1].Time) {
		t.Fatal("projection applied an older event after the final replay event")
	}
}

func TestOrderedObserverCanQueryHistory(t *testing.T) {
	bus := NewEventBus()
	defer bus.Close()
	bus.AddOrderedObserver(func(ev core.Event) {
		history := bus.History(core.EventFilter{})
		if len(history) != 1 || history[0].Sequence != ev.Sequence {
			t.Error("ordered observer cannot see its published event")
		}
	})
	bus.Publish(core.Event{Type: core.EventRunStarted})
}

func TestSubscriberWaitsForOrderedProjection(t *testing.T) {
	bus := NewEventBus()
	defer bus.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, err := bus.Subscribe(ctx, core.EventFilter{})
	if err != nil {
		t.Fatal(err)
	}
	started, release, published := make(chan struct{}), make(chan struct{}), make(chan struct{})
	store := runstore.NewRunStore(0)
	bus.AddOrderedObserver(func(ev core.Event) {
		close(started)
		<-release
		store.Record(ev)
	})
	go func() {
		bus.Publish(core.Event{RunID: "run", Type: core.EventRunStarted})
		close(published)
	}()
	<-started
	early := false
	select {
	case <-events:
		early = true
		t.Error("subscriber observed event before ordered projection completed")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	if !early {
		select {
		case <-events:
			if _, ok := store.Lookup("run"); !ok {
				t.Error("event arrived before run projection")
			}
		case <-time.After(time.Second):
			t.Error("subscriber never received committed event")
		}
	}
	<-published
}

func TestReplaySubscriptionWaitsForOrderedProjection(t *testing.T) {
	bus := NewEventBus()
	defer bus.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started, release, published := make(chan struct{}), make(chan struct{}), make(chan struct{})
	bus.AddOrderedObserver(func(core.Event) {
		close(started)
		<-release
	})
	go func() {
		bus.Publish(core.Event{RunID: "run", Type: core.EventRunStarted})
		close(published)
	}()
	<-started
	registered := make(chan (<-chan core.Event), 1)
	go func() {
		events, _ := bus.SubscribeReplay(ctx, core.EventFilter{})
		registered <- events
	}()
	var events <-chan core.Event
	select {
	case events = <-registered:
		t.Error("replay subscription registered before ordered projection completed")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	if events == nil {
		select {
		case events = <-registered:
		case <-time.After(time.Second):
			t.Fatal("replay subscription did not finish")
		}
	}
	<-published
	select {
	case <-events:
	case <-time.After(time.Second):
		t.Fatal("replay did not receive committed event")
	}
	select {
	case <-events:
		t.Error("replay received the same event from history and live delivery")
	case <-time.After(20 * time.Millisecond):
	}
}

func TestOrdinaryObserverCanPublish(t *testing.T) {
	bus := NewEventBus()
	defer bus.Close()
	bus.AddObserver(func(ev core.Event) {
		if ev.Type == core.EventRunStarted {
			bus.Publish(core.Event{Type: core.EventRunDone})
		}
	})
	done := make(chan struct{})
	go func() {
		bus.Publish(core.Event{Type: core.EventRunStarted})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("ordinary observer could not publish recursively")
	}
	if len(bus.History(core.EventFilter{})) != 2 {
		t.Fatal("recursive publication was lost")
	}
}

func TestConcurrentPublishKeepsReplaySequencesOrdered(t *testing.T) {
	bus := NewEventBus()
	defer bus.Close()
	var workers sync.WaitGroup
	for range 32 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for range 1000 {
				bus.Publish(core.Event{Type: core.EventSubAgentUpdated})
			}
		}()
	}
	workers.Wait()
	history := bus.History(core.EventFilter{})
	for i := 1; i < len(history); i++ {
		if history[i].Sequence <= history[i-1].Sequence {
			t.Fatalf("replay cursor would skip late event: sequences %d then %d", history[i-1].Sequence, history[i].Sequence)
		}
	}
}

func TestSubagentLifecycleFloodDoesNotGrowLossyQueue(t *testing.T) {
	s := &subscriber{wake: make(chan struct{}, 1)}
	for i := 0; i < defaultSubscriberQueueLimit*3; i++ {
		s.enqueue(core.Event{Type: core.EventSubAgentEnded, Payload: core.SubAgentPayload{ID: fmt.Sprint(i), Status: "completed"}})
	}
	if len(s.queue) > defaultSubscriberQueueLimit {
		t.Fatalf("unbounded task lifecycle backlog: %d", len(s.queue))
	}
	s.enqueue(core.Event{Type: core.EventRunDone})
	if len(s.queue) > defaultSubscriberQueueLimit || s.queue[len(s.queue)-1].Type != core.EventRunDone {
		t.Fatal("bounded lifecycle backlog lost run completion")
	}
}

func TestMainObserverExcludesOnlySubagentDetails(t *testing.T) {
	filter := core.EventFilter{ExcludeSubagentDetails: true}
	for _, payload := range []any{core.ToolPayload{SubAgentID: "child"}, protocol.SubAgentOutput{ID: "child"}} {
		event := core.Event{Payload: payload}
		if Matches(filter, event) || !Matches(core.EventFilter{}, event) {
			t.Fatalf("unexpected detail filtering: %#v", payload)
		}
	}
	for _, payload := range []any{core.ToolPayload{}, core.SubAgentPayload{ID: "child"}, core.ApprovalView{SubAgentID: "child"}, core.QuestionView{SubAgentID: "child"}} {
		if !Matches(filter, core.Event{Payload: payload}) {
			t.Fatalf("lost primary or actionable event: %#v", payload)
		}
	}
}

func TestSubagentFilterMatchesOnlySelectedTask(t *testing.T) {
	filter := core.EventFilter{SubAgentID: "selected"}
	for _, event := range []core.Event{
		{Type: core.EventSubAgentStarted, Payload: core.SubAgentPayload{ID: "selected"}},
		{Type: core.EventSubAgentUpdated, Payload: core.SubAgentPayload{ID: "selected"}},
		{Type: core.EventToolStarted, Payload: core.ToolPayload{SubAgentID: "selected"}},
		{Type: core.EventSubAgentOutput, Payload: protocol.SubAgentOutput{ID: "selected"}},
		{Type: core.EventQuestionRequested, Payload: core.QuestionView{SubAgentID: "selected"}},
		{Type: core.EventApprovalRequested, Payload: core.ApprovalView{SubAgentID: "selected"}},
	} {
		if !Matches(filter, event) {
			t.Fatalf("missing selected event: %+v", event)
		}
	}
	for _, event := range []core.Event{
		{Type: core.EventSubAgentOutput, Payload: protocol.SubAgentOutput{ID: "other"}},
		{Type: core.EventToolStarted, Payload: core.ToolPayload{}},
		{Type: core.EventAssistantDelta, Payload: core.DeltaPayload{Delta: "main"}},
	} {
		if Matches(filter, event) {
			t.Fatalf("unrelated event leaked: %+v", event)
		}
	}
}

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

func TestAssistantResetSurvivesBacklog(t *testing.T) {
	s := &subscriber{wake: make(chan struct{}, 1)}
	for i := 0; i < defaultSubscriberQueueLimit; i++ {
		s.enqueue(core.Event{Type: core.EventAssistantDelta})
	}
	s.enqueue(core.Event{Type: core.EventAssistantReset})
	for _, ev := range s.queue {
		if ev.Type == core.EventAssistantReset {
			return
		}
	}
	t.Fatal("assistant_reset dropped from saturated subscriber queue")
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
	for _, ordered := range []bool{false, true} {
		t.Run(fmt.Sprintf("ordered=%t", ordered), func(t *testing.T) {
			bus := NewEventBus()
			started := make(chan struct{})
			release := make(chan struct{})
			observe := func(core.Event) {
				close(started)
				<-release
			}
			if ordered {
				bus.AddOrderedObserver(observe)
			} else {
				bus.AddObserver(observe)
			}

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
		})
	}
}
