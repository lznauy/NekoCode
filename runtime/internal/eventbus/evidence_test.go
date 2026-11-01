package eventbus

import (
	"testing"

	"nekocode/protocol"
	"nekocode/runtime/internal/core"
)

func evidenceEvent() core.Event {
	return core.Event{Type: core.EventToolCompleted, Payload: core.ToolPayload{Evidence: protocol.ToolEvidence{Schema: 1, Executions: []protocol.ExecutionReceipt{{ID: "physical", Tool: "shell", Status: "error"}}}}}
}
func mutateReceipt(event core.Event) {
	event.Payload.(core.ToolPayload).Evidence.Executions[0].ID = "mutated"
}
func assertReceipt(t *testing.T, event core.Event) {
	t.Helper()
	if got := event.Payload.(core.ToolPayload).Evidence.Executions[0].ID; got != "physical" {
		t.Errorf("shared receipt mutated: %s", got)
	}
}

func TestEvidenceDetachedForObserversHistoryAndSubscribers(t *testing.T) {
	bus := NewEventBus()
	defer bus.Close()
	first, err := bus.Subscribe(t.Context(), core.EventFilter{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := bus.Subscribe(t.Context(), core.EventFilter{})
	if err != nil {
		t.Fatal(err)
	}
	bus.AddOrderedObserver(mutateReceipt)
	bus.AddOrderedObserver(func(event core.Event) { assertReceipt(t, event) })
	bus.AddObserver(mutateReceipt)
	bus.AddObserver(func(event core.Event) { assertReceipt(t, event) })
	original := evidenceEvent()
	published := bus.Publish(original)
	assertReceipt(t, original)
	assertReceipt(t, published)
	mutateReceipt(original)
	mutateReceipt(published)
	mutateReceipt(<-first)
	assertReceipt(t, <-second)
	history := bus.History(core.EventFilter{})
	assertReceipt(t, history[0])
	mutateReceipt(history[0])
	assertReceipt(t, bus.History(core.EventFilter{})[0])
	replay, err := bus.SubscribeReplay(t.Context(), core.EventFilter{})
	if err != nil {
		t.Fatal(err)
	}
	mutateReceipt(<-replay)
	assertReceipt(t, bus.History(core.EventFilter{})[0])
}

func TestEvidenceDetachedForImportedAndRecordedReplay(t *testing.T) {
	bus := NewEventBus()
	defer bus.Close()
	original := evidenceEvent()
	bus.ImportHistory([]core.Event{original})
	mutateReceipt(original)
	assertReceipt(t, bus.History(core.EventFilter{})[0])
	recorded := []core.Event{evidenceEvent()}
	replay, err := bus.SubscribeReplayEvents(t.Context(), core.EventFilter{}, recorded)
	if err != nil {
		t.Fatal(err)
	}
	event := <-replay
	mutateReceipt(event)
	assertReceipt(t, recorded[0])
}
