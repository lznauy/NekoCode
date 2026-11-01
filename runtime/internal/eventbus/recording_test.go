package eventbus

import (
	"errors"
	"testing"
	"time"

	"nekocode/runtime/internal/core"
)

func TestRecordingFailureDecoratesTerminalBeforeEveryConsumer(t *testing.T) {
	for _, kind := range []core.EventType{core.EventRunDone, core.EventRunFailed, core.EventRunCancelled} {
		t.Run(string(kind), func(t *testing.T) {
			bus := NewEventBus()
			defer bus.Close()
			delivered, err := bus.Subscribe(t.Context(), core.EventFilter{})
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			bus.SetRecordingSink(func(event core.Event) error { calls++; return errors.New("write failed") })
			bus.AddOrderedObserver(func(event core.Event) {
				if !event.Payload.(core.RunResult).EvidenceIncomplete {
					t.Error("projection saw unmarked terminal")
				}
			})
			event := bus.Publish(core.Event{Type: kind, Payload: core.RunResult{Output: "actual output"}})
			for _, got := range []core.Event{event, <-delivered, bus.History(core.EventFilter{})[0]} {
				result := got.Payload.(core.RunResult)
				if got.Type != kind || !result.EvidenceIncomplete || result.Output != "actual output" {
					t.Fatalf("terminal changed/lost fact: %+v", got)
				}
			}
			if calls != 1 {
				t.Fatalf("recording calls=%d", calls)
			}
		})
	}
}

func TestCloseWaitsForPendingRecording(t *testing.T) {
	bus := NewEventBus()
	started, release, closed := make(chan struct{}), make(chan struct{}), make(chan struct{})
	bus.SetRecordingSink(func(core.Event) error { close(started); <-release; return errors.New("last frame failed") })
	published := make(chan struct{})
	go func() { bus.Publish(core.Event{Type: core.EventRunDone, Payload: core.RunResult{}}); close(published) }()
	<-started
	go func() { bus.Close(); close(closed) }()
	select {
	case <-closed:
		t.Error("Close bypassed pending recording")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	<-published
	<-closed
	if !bus.History(core.EventFilter{})[0].Payload.(core.RunResult).EvidenceIncomplete {
		t.Fatal("shutdown lost final recording failure")
	}
}
