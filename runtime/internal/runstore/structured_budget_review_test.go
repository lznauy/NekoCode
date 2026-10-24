package runstore

import (
	"strings"
	"testing"

	"nekocode/protocol"
	"nekocode/runtime/internal/core"
)

func TestStructuredArchiveDetailsRespectByteBudget(t *testing.T) {
	large := strings.Repeat("x", DefaultBytes+1)
	for name, snapshot := range map[string]core.RunSnapshot{
		"todo":     {Todos: []protocol.TodoItem{{Content: large}}},
		"question": {Questions: []core.QuestionView{{Questions: []protocol.QuestionItem{{Question: large}}}}},
		"approval": {Approvals: []core.ApprovalView{{Args: map[string]any{"value": large}}}},
		"subagent": {SubAgents: []core.SubAgentView{{Skills: []string{large}}}},
	} {
		t.Run(name, func(t *testing.T) {
			store := NewRunStore(100)
			snapshot.ID, snapshot.Status = "archive", core.RunDone
			store.CacheRestored(snapshot)
			if _, ok := store.Lookup(snapshot.ID); ok {
				t.Fatal("oversized structured archive details retained in bounded run cache")
			}
		})
	}
}

func TestCompletedStructuredRunCanBeEvicted(t *testing.T) {
	s := NewRunStore(100)
	s.Record(core.Event{RunID: "old", Type: core.EventQuestionRequested, Payload: core.QuestionView{ID: "q", Questions: []protocol.QuestionItem{{Question: strings.Repeat("x", DefaultBytes+1)}}}})
	s.Record(core.Event{RunID: "old", Type: core.EventRunDone})
	if _, ok := s.Lookup("old"); !ok {
		t.Fatal("latest run must remain pinned")
	}
	s.Record(core.Event{RunID: "new", Type: core.EventRunStarted})
	if _, ok := s.Lookup("old"); ok {
		t.Fatal("completed structured run escaped eviction after new run started")
	}
}
