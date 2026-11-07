package core

import "testing"

func TestEveryEventTypeHasOnePayloadContract(t *testing.T) {
	for _, eventType := range []EventType{
		EventRunStarted, EventAssistantReset, EventInteractionModeChanged,
		EventInputAccepted, EventSystemMessage, EventAssistantDelta, EventAssistantMessage,
		EventReasoningDelta, EventPhaseChanged, EventCompaction, EventToolStarted,
		EventToolBlocked, EventToolPreview, EventToolCompleted, EventSubAgentStarted,
		EventSubAgentEnded, EventSubAgentUpdated, EventSubAgentOutput, EventTodosUpdated, EventApprovalRequested,
		EventApprovalResolved, EventQuestionRequested, EventQuestionResolved, EventRunSummary,
		EventRunDone, EventRunFailed, EventRunCancelled, EventSessionChanged,
		EventConnectorStatus, EventMetricsUpdated, EventContextUpdated,
	} {
		if eventType == EventRunStarted || eventType == EventAssistantReset {
			continue
		}
		if _, ok := payloadCodecs[eventType]; !ok {
			t.Errorf("event %q has no payload codec", eventType)
		}
	}
	if len(payloadCodecs) != 29 {
		t.Fatalf("payload codec count = %d, want 29", len(payloadCodecs))
	}
}
