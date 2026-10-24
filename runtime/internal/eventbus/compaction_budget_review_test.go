package eventbus

import (
	"strings"
	"testing"

	"nekocode/protocol"
	"nekocode/runtime/internal/core"
)

func TestOversizedCompactionSummaryDoesNotBypassHistoryBudget(t *testing.T) {
	b := NewEventBus()
	defer b.Close()
	summary := strings.Repeat("x", defaultEventHistoryBytes+1)
	b.Publish(core.Event{Type: core.EventCompaction, Payload: protocol.CompactionEvent{
		Status: protocol.CompactionCompleted, Trigger: protocol.CompactionAuto, Summary: summary,
	}})
	if len(b.History(core.EventFilter{})) != 0 {
		t.Fatal("oversized completed compaction retained in bounded event history")
	}
}

func TestOversizedSubAgentOutputDoesNotBypassHistoryBudget(t *testing.T) {
	b := NewEventBus()
	defer b.Close()
	b.Publish(core.Event{Type: core.EventSubAgentOutput, Payload: protocol.SubAgentOutput{
		Text: strings.Repeat("x", defaultEventHistoryBytes+1),
	}})
	if len(b.History(core.EventFilter{})) != 0 {
		t.Fatal("oversized subagent output retained in bounded event history")
	}
}

func TestStructuredControlPayloadCountsTowardHistoryBudget(t *testing.T) {
	large := strings.Repeat("x", defaultEventHistoryBytes+1)
	for name, payload := range map[string]any{
		"tool_input": core.ToolPayload{Input: []byte(`"` + large + `"`)},
		"approval":   core.ApprovalView{Args: map[string]any{"value": large}},
		"question":   core.QuestionView{Questions: []protocol.QuestionItem{{Question: large}}},
	} {
		t.Run(name, func(t *testing.T) {
			b := NewEventBus()
			defer b.Close()
			b.Publish(core.Event{Payload: payload})
			if len(b.History(core.EventFilter{})) != 0 {
				t.Fatal("structured payload bypassed history byte budget")
			}
		})
	}
}
