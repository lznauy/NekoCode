package core

import (
	"bytes"
	"encoding/json"
	"fmt"

	"nekocode/protocol"
)

type payloadCodec struct {
	decode func(json.RawMessage) (any, error)
	valid  func(any) bool
}

func codec[T any]() payloadCodec {
	return payloadCodec{
		decode: func(data json.RawMessage) (any, error) {
			var value T
			if err := json.Unmarshal(data, &value); err != nil {
				return nil, err
			}
			return value, nil
		},
		valid: func(value any) bool { _, ok := value.(T); return ok },
	}
}

// This table is the single mapping between event kinds and typed payloads.
// Recording replay and transport validation must not maintain separate maps.
var payloadCodecs = map[EventType]payloadCodec{
	EventInputAccepted: codec[MessagePayload](), EventSystemMessage: codec[MessagePayload](), EventAssistantMessage: codec[MessagePayload](),
	EventAssistantDelta: codec[DeltaPayload](), EventReasoningDelta: codec[DeltaPayload](),
	EventPhaseChanged: codec[PhasePayload](), EventCompaction: codec[protocol.CompactionEvent](),
	EventToolStarted: codec[ToolPayload](), EventToolBlocked: codec[ToolPayload](), EventToolPreview: codec[ToolPayload](), EventToolCompleted: codec[ToolPayload](),
	EventSubAgentStarted: codec[SubAgentPayload](), EventSubAgentEnded: codec[SubAgentPayload](), EventSubAgentOutput: codec[protocol.SubAgentOutput](),
	EventTodosUpdated:      codec[[]protocol.TodoItem](),
	EventApprovalRequested: codec[ApprovalView](), EventApprovalResolved: codec[ApprovalView](),
	EventQuestionRequested: codec[QuestionView](), EventQuestionResolved: codec[QuestionView](),
	EventRunSummary: codec[protocol.RunSummary](),
	EventRunDone:    codec[RunResult](), EventRunFailed: codec[RunResult](), EventRunCancelled: codec[RunResult](),
	EventSessionChanged: codec[SessionPayload](), EventConnectorStatus: codec[ConnectorStatusPayload](),
	EventMetricsUpdated: codec[protocol.Metrics](), EventContextUpdated: codec[protocol.ContextSnapshot](),
}

// ValidateEventPayload rejects malformed known payloads without including their
// possibly sensitive contents in errors. Unknown event kinds remain extensible.
func ValidateEventPayload(event Event) error {
	if event.Type == EventRunStarted || event.Type == EventAssistantReset {
		if event.Payload != nil {
			return fmt.Errorf("event %s does not accept a payload", event.Type)
		}
		return nil
	}
	if codec, known := payloadCodecs[event.Type]; known && !codec.valid(event.Payload) {
		return fmt.Errorf("event %s has invalid payload type %T", event.Type, event.Payload)
	}
	if event.Type == EventCompaction {
		value := event.Payload.(protocol.CompactionEvent)
		if !value.Status.Valid() || !value.Trigger.Valid() {
			return fmt.Errorf("invalid compaction event status %q or trigger %q", value.Status, value.Trigger)
		}
	}
	return nil
}

// DecodeEventPayload restores a typed payload. Missing payloads remain nil;
// known payload-bearing events preserve their typed zero value for JSON null.
func DecodeEventPayload(typ EventType, data json.RawMessage) (any, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, nil
	}
	if bytes.Equal(trimmed, []byte("null")) {
		if typ == EventRunStarted || typ == EventAssistantReset {
			return nil, nil
		}
		if _, known := payloadCodecs[typ]; !known {
			return nil, nil
		}
	}
	var value any
	var err error
	if codec, known := payloadCodecs[typ]; known {
		value, err = codec.decode(data)
	} else {
		err = json.Unmarshal(data, &value)
	}
	if err != nil {
		return nil, fmt.Errorf("decode %s payload: %w", typ, err)
	}
	if err := ValidateEventPayload(Event{Type: typ, Payload: value}); err != nil {
		return nil, err
	}
	return value, nil
}
