package runstore

import (
	"nekocode/runtime/internal/core"
	"testing"
)

func TestAssistantResetPreservesSystemMessages(t *testing.T) {
	s := NewRunStore(10)
	s.Record(core.Event{RunID: "run_1", Type: core.EventAssistantDelta, Payload: core.DeltaPayload{Delta: "injected prefix"}})
	s.Record(core.Event{RunID: "run_1", Type: core.EventSystemMessage, Payload: core.MessagePayload{Content: "system notice"}})
	s.Record(core.Event{RunID: "run_1", Type: core.EventAssistantReset})
	s.Record(core.Event{RunID: "run_1", Type: core.EventRunFailed, Payload: core.RunResult{Error: "failed"}})
	run, _ := s.Lookup("run_1")
	if run.Output != "\nsystem notice" {
		t.Fatalf("output = %q", run.Output)
	}
}
