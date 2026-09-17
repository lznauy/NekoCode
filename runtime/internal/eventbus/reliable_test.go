package eventbus

import (
	"testing"

	"nekocode/runtime/internal/core"
)

func TestReliableOverflowClosesInsteadOfSilentlyDropping(t *testing.T) {
	s := &subscriber{filter: core.EventFilter{Reliable: true}, wake: make(chan struct{}, 1), stop: make(chan struct{})}
	for i := 0; i <= defaultSubscriberQueueLimit; i++ {
		s.enqueue(core.Event{Type: core.EventAssistantDelta, Payload: core.DeltaPayload{Delta: "text"}})
	}
	select {
	case <-s.stop:
	default:
		t.Fatal("reliable subscriber silently dropped an event")
	}
	s.enqueue(core.Event{Type: core.EventRunDone})
	if len(s.queue) != 0 {
		t.Fatal("failed stream accepted a terminal event")
	}
}

func TestReliableRetainsToolAndTextEventsBelowLimit(t *testing.T) {
	s := &subscriber{filter: core.EventFilter{Reliable: true}, wake: make(chan struct{}, 1), stop: make(chan struct{})}
	for _, typ := range []core.EventType{core.EventAssistantDelta, core.EventAssistantMessage, core.EventToolStarted, core.EventToolCompleted, core.EventRunDone} {
		s.enqueue(core.Event{Type: typ})
	}
	if len(s.queue) != 5 {
		t.Fatalf("queue length = %d", len(s.queue))
	}
	select {
	case <-s.stop:
		t.Fatal("stream closed prematurely")
	default:
	}
}

func TestRunSummarySurvivesOrdinarySubscriberOverflow(t *testing.T) {
	s := &subscriber{wake: make(chan struct{}, 1)}
	for i := 0; i < defaultSubscriberQueueLimit; i++ {
		s.enqueue(core.Event{Type: core.EventAssistantDelta})
	}
	s.enqueue(core.Event{Type: core.EventRunSummary})
	s.enqueue(core.Event{Type: core.EventRunDone})
	var terminal []core.EventType
	for _, event := range s.queue {
		if event.Type == core.EventRunSummary || event.Type == core.EventRunDone {
			terminal = append(terminal, event.Type)
		}
	}
	if len(terminal) != 2 || terminal[0] != core.EventRunSummary || terminal[1] != core.EventRunDone {
		t.Fatal(terminal)
	}
}
