package interaction

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	controlruntime "nekocode/runtime"
)

type eventStreamRuntime struct {
	mu        sync.Mutex
	streams   []<-chan controlruntime.Event
	cancelled chan controlruntime.RunID
	current   controlruntime.RunSnapshot
	snapshots []controlruntime.RunSnapshot
}

func (r *eventStreamRuntime) CurrentRun() (controlruntime.RunSnapshot, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.snapshots) > 0 {
		run := r.snapshots[0]
		r.snapshots = r.snapshots[1:]
		return run, run.ID != ""
	}
	return r.current, r.current.ID != ""
}

func (r *eventStreamRuntime) Events(context.Context, controlruntime.EventFilter) (<-chan controlruntime.Event, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.streams) == 0 {
		return nil, errors.New("runtime closed")
	}
	events := r.streams[0]
	r.streams = r.streams[1:]
	return events, nil
}

func (r *eventStreamRuntime) CancelRun(ctx context.Context, id controlruntime.RunID) error {
	select {
	case r.cancelled <- id:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestSubscribeRunEventsRecoversWithoutReplayingLostControls(t *testing.T) {
	first := make(chan controlruntime.Event, 1)
	first <- controlruntime.Event{Type: controlruntime.EventRunStarted, RunID: "old"}
	close(first)
	second := make(chan controlruntime.Event, 3)
	second <- controlruntime.Event{Type: controlruntime.EventRunCancelled, RunID: "old"}
	second <- controlruntime.Event{Type: controlruntime.EventRunStarted, RunID: "new"}
	second <- controlruntime.Event{Type: controlruntime.EventRunDone, RunID: "new"}
	rt := &eventStreamRuntime{streams: []<-chan controlruntime.Event{first, second}, cancelled: make(chan controlruntime.RunID, 1)}
	events, err := SubscribeRunEvents(t.Context(), rt, controlruntime.EventFilter{})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []controlruntime.EventType{controlruntime.EventRunStarted, controlruntime.EventRunFailed, controlruntime.EventRunStarted, controlruntime.EventRunDone} {
		select {
		case ev := <-events:
			if ev.Type != want {
				t.Fatalf("event=%s want=%s", ev.Type, want)
			}
			if ev.Type == controlruntime.EventRunFailed {
				result, ok := ev.Payload.(controlruntime.RunResult)
				if !ok || result.Error == "" || ev.RunID != "old" {
					t.Fatalf("missing interruption details: %+v", ev)
				}
			}
		case <-time.After(2 * time.Second):
			t.Fatal("event recovery timed out")
		}
	}
	select {
	case id := <-rt.cancelled:
		if id != "old" {
			t.Fatalf("cancelled run=%s want=old", id)
		}
	default:
		t.Fatal("interrupted run was not cancelled")
	}
}

func TestSubscribeRunEventsRecoversCurrentIdentityWhenStartWasLost(t *testing.T) {
	first := make(chan controlruntime.Event, 1)
	first <- controlruntime.Event{Type: controlruntime.EventRunStarted, RunID: "old"}
	close(first)
	second := make(chan controlruntime.Event, 2)
	second <- controlruntime.Event{Type: controlruntime.EventRunCancelled, RunID: "active"}
	// A later run may complete before its start reaches an ordinary stream.
	second <- controlruntime.Event{Type: controlruntime.EventRunDone, RunID: "successor"}
	rt := &eventStreamRuntime{streams: []<-chan controlruntime.Event{first, second}, cancelled: make(chan controlruntime.RunID, 1), current: controlruntime.RunSnapshot{ID: "active"}}
	events, err := SubscribeRunEvents(t.Context(), rt, controlruntime.EventFilter{})
	if err != nil {
		t.Fatal(err)
	}
	<-events
	select {
	case ev := <-events:
		if ev.Type != controlruntime.EventRunFailed || ev.RunID != "active" {
			t.Fatalf("failure identity=%+v", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("failure notification timed out")
	}
	select {
	case id := <-rt.cancelled:
		if id != "active" {
			t.Fatalf("cancelled %s instead of active", id)
		}
	default:
		t.Fatal("missing cancellation")
	}
	select {
	case ev := <-events:
		if ev.Type != controlruntime.EventRunDone || ev.RunID != "successor" {
			t.Fatalf("successor terminal was swallowed: %+v", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("successor terminal timed out")
	}
}

func TestSubscribeRunEventsStopsOnCancellationAndShutdown(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		ctx, cancel := context.WithCancel(t.Context())
		first := make(chan controlruntime.Event)
		rt := &eventStreamRuntime{streams: []<-chan controlruntime.Event{first}, cancelled: make(chan controlruntime.RunID, 1)}
		events, err := SubscribeRunEvents(ctx, rt, controlruntime.EventFilter{})
		if err != nil {
			t.Fatal(err)
		}
		if shutdown {
			close(first)
		} else {
			cancel()
		}
		select {
		case _, ok := <-events:
			if ok {
				t.Fatal("shutdown produced a spurious run failure")
			}
		case <-time.After(2 * time.Second):
			t.Fatal("event stream did not close")
		}
		cancel()
		select {
		case <-rt.cancelled:
			t.Fatal("shutdown cancelled an unrelated run")
		default:
		}
	}
}

func TestSubscribeRunEventsDoesNotFailFinishedRuns(t *testing.T) {
	for _, status := range []controlruntime.RunStatus{controlruntime.RunDone, controlruntime.RunFailed, controlruntime.RunCancelled} {
		t.Run(string(status), func(t *testing.T) {
			terminal := controlruntime.EventRunDone
			if status == controlruntime.RunFailed {
				terminal = controlruntime.EventRunFailed
			} else if status == controlruntime.RunCancelled {
				terminal = controlruntime.EventRunCancelled
			}
			first := make(chan controlruntime.Event, 3)
			first <- controlruntime.Event{Type: controlruntime.EventRunStarted, RunID: "finished"}
			first <- controlruntime.Event{Type: terminal, RunID: "finished"}
			first <- controlruntime.Event{Type: controlruntime.EventMetricsUpdated, RunID: "finished"}
			close(first)
			second := make(chan controlruntime.Event, 1)
			second <- controlruntime.Event{Type: controlruntime.EventRunStarted, RunID: "next"}
			rt := &eventStreamRuntime{
				streams:   []<-chan controlruntime.Event{first, second},
				cancelled: make(chan controlruntime.RunID, 1),
				current:   controlruntime.RunSnapshot{ID: "finished", Status: status},
			}
			events, err := SubscribeRunEvents(t.Context(), rt, controlruntime.EventFilter{})
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []controlruntime.EventType{controlruntime.EventRunStarted, terminal, controlruntime.EventMetricsUpdated, controlruntime.EventRunStarted} {
				select {
				case ev := <-events:
					if ev.Type != want {
						t.Fatalf("event = %+v, want %s", ev, want)
					}
				case <-time.After(2 * time.Second):
					t.Fatal("reconnection timed out")
				}
			}
			select {
			case id := <-rt.cancelled:
				t.Fatalf("finished run %s was cancelled", id)
			default:
			}
		})
	}
}

func TestSubscribeRunEventsFinishedSnapshotWithLostTerminal(t *testing.T) {
	for _, tc := range []struct {
		status controlruntime.RunStatus
		typ    controlruntime.EventType
		err    string
	}{
		{controlruntime.RunDone, controlruntime.EventRunDone, ""},
		{controlruntime.RunFailed, controlruntime.EventRunFailed, "provider failed"},
		{controlruntime.RunCancelled, controlruntime.EventRunCancelled, "cancelled"},
	} {
		for _, started := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/started=%v", tc.status, started), func(t *testing.T) {
				first := make(chan controlruntime.Event, 1)
				if started {
					first <- controlruntime.Event{Type: controlruntime.EventRunStarted, RunID: "finished"}
				}
				close(first)
				second := make(chan controlruntime.Event, 1)
				second <- controlruntime.Event{Type: controlruntime.EventRunStarted, RunID: "next"}
				rt := &eventStreamRuntime{
					streams:   []<-chan controlruntime.Event{first, second},
					cancelled: make(chan controlruntime.RunID, 1),
					current:   controlruntime.RunSnapshot{ID: "finished", Status: tc.status, Output: "completed answer", Error: tc.err},
					snapshots: []controlruntime.RunSnapshot{{}},
				}
				events, err := SubscribeRunEvents(t.Context(), rt, controlruntime.EventFilter{})
				if err != nil {
					t.Fatal(err)
				}
				want := []controlruntime.EventType{tc.typ, controlruntime.EventRunStarted}
				if started {
					want = append([]controlruntime.EventType{controlruntime.EventRunStarted}, want...)
				}
				for _, typ := range want {
					select {
					case ev := <-events:
						if ev.Type != typ {
							t.Fatalf("event = %+v, want %s", ev, typ)
						}
						if typ == tc.typ {
							result, ok := ev.Payload.(controlruntime.RunResult)
							if !ok || ev.RunID != "finished" || result.Output != "completed answer" || result.Error != tc.err {
								t.Fatalf("recovered result = %+v", ev)
							}
						}
					case <-time.After(2 * time.Second):
						t.Fatal("reconnection timed out")
					}
				}
				select {
				case id := <-rt.cancelled:
					t.Fatalf("finished run %s was cancelled", id)
				default:
				}
			})
		}
	}
}

func TestSubscribeRunEventsSettlesInterruptedRunOnce(t *testing.T) {
	for _, lateControls := range []bool{false, true} {
		t.Run(fmt.Sprintf("lateControls=%v", lateControls), func(t *testing.T) {
			first := make(chan controlruntime.Event, 1)
			first <- controlruntime.Event{Type: controlruntime.EventRunStarted, RunID: "old"}
			close(first)
			second := make(chan controlruntime.Event, 4)
			if lateControls {
				for _, typ := range []controlruntime.EventType{controlruntime.EventApprovalRequested, controlruntime.EventQuestionRequested, controlruntime.EventMetricsUpdated} {
					second <- controlruntime.Event{Type: typ, RunID: "old"}
				}
			}
			second <- controlruntime.Event{Type: controlruntime.EventRunCancelled, RunID: "old"}
			close(second)
			third := make(chan controlruntime.Event, 1)
			third <- controlruntime.Event{Type: controlruntime.EventRunStarted, RunID: "next"}
			rt := &eventStreamRuntime{
				streams:   []<-chan controlruntime.Event{first, second, third},
				cancelled: make(chan controlruntime.RunID, 2),
				current:   controlruntime.RunSnapshot{ID: "old", Status: controlruntime.RunCancelled},
				snapshots: []controlruntime.RunSnapshot{{}, {ID: "old", Status: controlruntime.RunRunning}},
			}
			events, err := SubscribeRunEvents(t.Context(), rt, controlruntime.EventFilter{})
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []controlruntime.EventType{controlruntime.EventRunStarted, controlruntime.EventRunFailed, controlruntime.EventRunStarted} {
				select {
				case ev := <-events:
					if ev.Type != want {
						t.Fatalf("event = %+v, want %s", ev, want)
					}
				case <-time.After(2 * time.Second):
					t.Fatal("reconnection timed out")
				}
			}
			if len(rt.cancelled) != 1 {
				t.Fatalf("cancellation count = %d, want 1", len(rt.cancelled))
			}
		})
	}
}

func TestSubscribeRunEventsDoesNotReplayPreexistingHistory(t *testing.T) {
	first := make(chan controlruntime.Event)
	close(first)
	second := make(chan controlruntime.Event, 1)
	second <- controlruntime.Event{Type: controlruntime.EventRunStarted, RunID: "next"}
	rt := &eventStreamRuntime{
		streams:   []<-chan controlruntime.Event{first, second},
		cancelled: make(chan controlruntime.RunID, 1),
		current:   controlruntime.RunSnapshot{ID: "history", Status: controlruntime.RunDone, Output: "old answer"},
	}
	events, err := SubscribeRunEvents(t.Context(), rt, controlruntime.EventFilter{})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case ev := <-events:
		if ev.Type != controlruntime.EventRunStarted || ev.RunID != "next" {
			t.Fatalf("preexisting history was replayed: %+v", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("reconnection timed out")
	}
	if len(rt.cancelled) != 0 {
		t.Fatal("preexisting run was cancelled")
	}
}
