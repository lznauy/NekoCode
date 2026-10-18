package runtime

import (
	"encoding/json"
	"testing"

	"nekocode/protocol"
)

func TestDecodeEventPayloadPreservesTypedNullForKnownEvents(t *testing.T) {
	for _, eventType := range []EventType{EventTodosUpdated, EventRunDone, EventMetricsUpdated} {
		t.Run(string(eventType), func(t *testing.T) {
			payload, err := DecodeEventPayload(eventType, json.RawMessage("null"))
			if err != nil {
				t.Fatal(err)
			}
			switch eventType {
			case EventTodosUpdated:
				if _, ok := payload.([]protocol.TodoItem); !ok {
					t.Fatalf("payload type = %T, want []protocol.TodoItem", payload)
				}
			case EventRunDone:
				if _, ok := payload.(RunResult); !ok {
					t.Fatalf("payload type = %T, want RunResult", payload)
				}
			case EventMetricsUpdated:
				if _, ok := payload.(protocol.Metrics); !ok {
					t.Fatalf("payload type = %T, want protocol.Metrics", payload)
				}
			}
		})
	}
	for _, eventType := range []EventType{EventRunStarted, EventAssistantReset, EventType("future_event")} {
		payload, err := DecodeEventPayload(eventType, json.RawMessage("null"))
		if err != nil || payload != nil {
			t.Fatalf("%s null payload = %#v, err = %v", eventType, payload, err)
		}
	}
}
