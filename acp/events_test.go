package acp

import (
	"testing"

	controlruntime "nekocode/runtime"
)

func TestEventUpdate(t *testing.T) {
	tests := []struct {
		name  string
		event controlruntime.Event
		kinds []string
	}{
		{"system", controlruntime.Event{Type: controlruntime.EventSystemMessage, Payload: controlruntime.MessagePayload{Content: "ready"}}, []string{"agent_message_chunk"}},
		{"speculative text skipped", controlruntime.Event{Type: controlruntime.EventAssistantDelta, Payload: controlruntime.DeltaPayload{Delta: "hi"}}, nil},
		{"thought", controlruntime.Event{Type: controlruntime.EventReasoningDelta, Payload: controlruntime.DeltaPayload{Delta: "hmm"}}, []string{"agent_thought_chunk"}},
		{"tool", controlruntime.Event{ID: "event-1", Type: controlruntime.EventToolStarted, Payload: controlruntime.ToolPayload{CallID: "call-1", ToolName: "read_file", Args: `{"path":"a.go"}`}}, []string{"tool_call"}},
		{"preview", controlruntime.Event{ID: "event-2", Type: controlruntime.EventToolPreview, Payload: controlruntime.ToolPayload{CallID: "call-1", ToolName: "read_file", Preview: "reading"}}, []string{"tool_call_update"}},
		{"plan", controlruntime.Event{Type: controlruntime.EventTodosUpdated, Payload: []controlruntime.TodoItem{{Content: "ship", Status: "completed"}}}, []string{"plan"}},
		{"usage", controlruntime.Event{Type: controlruntime.EventMetricsUpdated, Payload: controlruntime.MetricsSnapshot{ContextTokens: 4200, ContextBudget: 128000}}, []string{"usage_update"}},
		{"usage empty skipped", controlruntime.Event{Type: controlruntime.EventMetricsUpdated, Payload: controlruntime.MetricsSnapshot{}}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			updates, ok := eventUpdate(tt.event)
			if ok != (len(tt.kinds) != 0) || len(updates) != len(tt.kinds) {
				t.Fatalf("updates = %#v, ok = %v", updates, ok)
			}
			for i, update := range updates {
				if update["sessionUpdate"] != tt.kinds[i] {
					t.Fatalf("update[%d] = %#v, want %s", i, update, tt.kinds[i])
				}
			}
		})
	}
}

func TestBlockedToolEmitsToolCall(t *testing.T) {
	event := controlruntime.Event{
		ID:   "event-3",
		Type: controlruntime.EventToolBlocked,
		Payload: controlruntime.ToolPayload{
			CallID: "call-9", ToolName: "shell", Args: `{"command":"rm -rf /"}`, Output: "denied by rule",
		},
	}
	updates, ok := eventUpdate(event)
	if !ok || len(updates) != 2 {
		t.Fatalf("updates = %#v, ok = %v", updates, ok)
	}
	if updates[0]["sessionUpdate"] != "tool_call" || updates[0]["toolCallId"] != "call-9" || updates[0]["status"] != "pending" {
		t.Fatalf("tool_call update = %#v", updates[0])
	}
	if updates[1]["sessionUpdate"] != "tool_call_update" || updates[1]["toolCallId"] != "call-9" || updates[1]["status"] != "failed" {
		t.Fatalf("tool_call_update = %#v", updates[1])
	}
}
