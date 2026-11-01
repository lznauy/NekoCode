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

func TestToolEvidenceCodecAndLegacyCompatibility(t *testing.T) {
	evidence := protocol.ToolEvidence{Schema: 1, Executions: []protocol.ExecutionReceipt{{ID: "physical", Tool: "shell", Status: "error", Process: protocol.ProcessObservation{State: "exited", ExitCodeKnown: true, ExitCode: 2}}}, ReturnedHash: "delivery"}
	data, err := json.Marshal(ToolPayload{ToolName: "shell", CallID: "same-call", Evidence: evidence})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeEventPayload(EventToolCompleted, data)
	if err != nil {
		t.Fatal(err)
	}
	got := decoded.(ToolPayload)
	if len(got.Evidence.Executions) != 1 || got.Evidence.Executions[0].Process.ExitCode != 2 || got.Evidence.ReturnedHash != "delivery" {
		t.Fatalf("evidence lost: %+v", got)
	}
	// Older consumers ignore the optional field; older producers omit it.
	var legacy struct {
		ToolName string `json:"tool_name"`
		CallID   string `json:"call_id"`
	}
	if err := json.Unmarshal(data, &legacy); err != nil || legacy.CallID != "same-call" {
		t.Fatalf("legacy consumer: %+v %v", legacy, err)
	}
	decoded, err = DecodeEventPayload(EventToolCompleted, json.RawMessage(`{"tool_name":"shell","call_id":"old"}`))
	if err != nil || decoded.(ToolPayload).Evidence.Schema != 0 {
		t.Fatalf("legacy event: %+v %v", decoded, err)
	}
	oldData, _ := json.Marshal(decoded)
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(oldData, &fields); err != nil {
		t.Fatal(err)
	}
	if _, exists := fields["evidence"]; exists {
		t.Fatal("empty evidence changed legacy wire shape")
	}
}
