package runstore

import (
	"testing"
	"time"

	"nekocode/protocol"
	"nekocode/runtime/internal/core"
)

func TestRunStoreBuildsRunView(t *testing.T) {
	store := NewRunStore(0)
	runID := core.RunID("run_1")
	now := time.Now()

	store.Record(core.Event{
		RunID: runID,
		Type:  core.EventInputAccepted,
		Time:  now,
		Payload: core.MessagePayload{
			Content: "edit README",
			Source:  core.SourceRef{Kind: "telegram", ID: "chat"},
			Sender:  core.SenderRef{Username: "alice"},
		},
	})
	store.Record(core.Event{
		RunID: runID,
		Type:  core.EventToolStarted,
		Time:  now.Add(time.Second),
		Payload: core.ToolPayload{
			ToolName: "edit",
			Args:     `{"path":"README.md"}`,
		},
	})
	diff := "--- a/README.md\n+++ b/README.md\n@@\n-old\n+new"
	store.Record(core.Event{
		RunID: runID,
		Type:  core.EventToolPreview,
		Time:  now.Add(2 * time.Second),
		Payload: core.ToolPayload{
			ToolName: "edit",
			Preview:  diff,
		},
	})
	patch := "*** Begin Patch\n*** Update File: README.md\n@@\n-old\n+new\n*** End Patch"
	store.Record(core.Event{
		RunID: runID,
		Type:  core.EventToolStarted,
		Time:  now.Add(2400 * time.Millisecond),
		Payload: core.ToolPayload{
			ToolName: "apply_patch",
		},
	})
	store.Record(core.Event{
		RunID: runID,
		Type:  core.EventToolPreview,
		Time:  now.Add(2500 * time.Millisecond),
		Payload: core.ToolPayload{
			ToolName: "apply_patch",
			Preview:  patch,
		},
	})
	review := "Findings\nSeverity: high\nMissing runtime artifact coverage."
	store.Record(core.Event{
		RunID: runID,
		Type:  core.EventToolCompleted,
		Time:  now.Add(2800 * time.Millisecond),
		Payload: core.ToolPayload{
			ToolName: "review",
			Output:   review,
		},
	})
	store.Record(core.Event{
		RunID: runID,
		Type:  core.EventRunDone,
		Time:  now.Add(3 * time.Second),
		Payload: core.RunResult{
			Output: "done",
		},
	})

	view, ok := store.Lookup(runID)
	if !ok {
		t.Fatal("run snapshot missing")
	}
	if view.Status != core.RunDone {
		t.Fatalf("status = %s, want %s", view.Status, core.RunDone)
	}
	if view.Input != "edit README" || view.Source.Kind != "telegram" || view.Sender.Username != "alice" {
		t.Fatalf("input/source/sender not captured: %#v", view)
	}
	if len(view.Tools) != 3 || view.Tools[0].Preview != diff || view.Tools[1].Preview != patch || view.Tools[2].Output != review {
		t.Fatalf("tool preview not captured: %#v", view.Tools)
	}
	if view.Output != "done" || view.FinishedAt == nil {
		t.Fatalf("done output not captured: %#v", view)
	}

}

func TestRunStoreCopiesViews(t *testing.T) {
	store := NewRunStore(0)
	store.Record(core.Event{
		RunID:   "run_1",
		Type:    core.EventToolStarted,
		Time:    time.Now(),
		Payload: core.ToolPayload{ToolName: "shell"},
	})
	first, ok := store.Lookup("run_1")
	if !ok {
		t.Fatal("run snapshot missing")
	}
	first.Tools[0].Name = "changed"
	second, _ := store.Lookup("run_1")
	if second.Tools[0].Name != "shell" {
		t.Fatalf("Lookup returned mutable slice: %#v", second.Tools)
	}
}

func TestRunStoreMatchesParallelToolsByCallID(t *testing.T) {
	store := NewRunStore(0)
	now := time.Now()
	for _, payload := range []core.ToolPayload{
		{CallID: "call_a", ToolName: "read", Args: "a"},
		{CallID: "call_b", ToolName: "read", Args: "b"},
	} {
		store.Record(core.Event{RunID: "run_1", Type: core.EventToolStarted, Time: now, Payload: payload})
	}
	store.Record(core.Event{
		RunID: "run_1", Type: core.EventToolCompleted, Time: now.Add(time.Second),
		Payload: core.ToolPayload{CallID: "call_a", ToolName: "read", Output: "output-a"},
	})
	store.Record(core.Event{
		RunID: "run_1", Type: core.EventToolCompleted, Time: now.Add(2 * time.Second),
		Payload: core.ToolPayload{CallID: "call_b", ToolName: "read", Output: "output-b"},
	})

	view, _ := store.Lookup("run_1")
	if len(view.Tools) != 2 ||
		view.Tools[0].CallID != "call_a" || view.Tools[0].Output != "output-a" ||
		view.Tools[1].CallID != "call_b" || view.Tools[1].Output != "output-b" {
		t.Fatalf("parallel tools paired incorrectly: %#v", view.Tools)
	}
}

func TestRunStoreProjectsRecoverableRunState(t *testing.T) {
	store := NewRunStore(0)
	now := time.Now()
	store.Record(core.Event{
		RunID: "run_1", Type: core.EventAssistantDelta, Time: now,
		Payload: core.DeltaPayload{Delta: "answer"},
	})
	store.Record(core.Event{
		RunID: "run_1", Type: core.EventReasoningDelta, Time: now,
		Payload: core.DeltaPayload{Delta: "thinking"},
	})
	todos := []protocol.TodoItem{{Content: "inspect", Status: "in_progress"}}
	store.Record(core.Event{RunID: "run_1", Type: core.EventTodosUpdated, Time: now, Payload: todos})
	store.Record(core.Event{
		RunID: "run_1", Type: core.EventSubAgentStarted, Time: now,
		Payload: core.SubAgentPayload{ID: "sub_1", Type: "explore", Profile: "explore", Skills: []string{"check"}, Color: 2},
	})

	view, _ := store.Lookup("run_1")
	if view.Output != "answer" || view.Reasoning != "thinking" || len(view.Todos) != 1 {
		t.Fatalf("stream state not projected: %#v", view)
	}
	if len(view.SubAgents) != 1 || !view.SubAgents[0].Active {
		t.Fatalf("subagent state not projected: %#v", view.SubAgents)
	}
	if view.SubAgents[0].Profile != "explore" || len(view.SubAgents[0].Skills) != 1 || view.SubAgents[0].Skills[0] != "check" {
		t.Fatalf("subagent composition not projected: %#v", view.SubAgents[0])
	}

	store.Record(core.Event{
		RunID: "run_1", Type: core.EventSubAgentEnded, Time: now.Add(time.Second),
		Payload: core.SubAgentPayload{ID: "sub_1"},
	})
	view, _ = store.Lookup("run_1")
	if view.SubAgents[0].Active || view.SubAgents[0].FinishedAt == nil {
		t.Fatalf("subagent end not projected: %#v", view.SubAgents[0])
	}
}

func TestRunStoreProjectsCommandOutput(t *testing.T) {
	store := NewRunStore(0)
	now := time.Now()
	store.Record(core.Event{
		RunID: "run_1", Type: core.EventSystemMessage, Time: now,
		Payload: core.MessagePayload{Content: "command result"},
	})
	store.Record(core.Event{
		RunID: "run_1", Type: core.EventRunDone, Time: now.Add(time.Second),
		Payload: core.RunResult{},
	})

	view, _ := store.Lookup("run_1")
	if view.Output != "command result" {
		t.Fatalf("command output = %q", view.Output)
	}
}

func TestRunStoreDeepCopiesInteractionViews(t *testing.T) {
	store := NewRunStore(0)
	args := map[string]any{"nested": map[string]any{"value": "original"}}
	metadata := map[string]any{"items": []any{"original"}}
	approval := &protocol.ApprovalContext{WritePaths: []string{"/original"}}
	store.Record(core.Event{
		RunID: "run_1", Type: core.EventApprovalRequested, Time: time.Now(),
		Payload: core.ApprovalView{
			ID: "approval_1", Args: args, Metadata: metadata, Approval: approval,
		},
	})
	options := []protocol.QuestionOption{{Label: "original"}}
	store.Record(core.Event{
		RunID: "run_1", Type: core.EventQuestionRequested, Time: time.Now(),
		Payload: core.QuestionView{
			ID: "question_1",
			Questions: []protocol.QuestionItem{{
				Question: "choose", Options: options,
			}},
		},
	})

	args["nested"].(map[string]any)["value"] = "changed before lookup"
	metadata["items"].([]any)[0] = "changed before lookup"
	approval.WritePaths[0] = "/changed-before-lookup"
	options[0].Label = "changed before lookup"
	first, _ := store.Lookup("run_1")
	first.Approvals[0].Args["nested"].(map[string]any)["value"] = "changed"
	first.Approvals[0].Metadata["items"].([]any)[0] = "changed"
	first.Approvals[0].Approval.WritePaths[0] = "/changed"
	first.Questions[0].Questions[0].Options[0].Label = "changed"

	second, _ := store.Lookup("run_1")
	if second.Approvals[0].Args["nested"].(map[string]any)["value"] != "original" ||
		second.Approvals[0].Metadata["items"].([]any)[0] != "original" ||
		second.Approvals[0].Approval.WritePaths[0] != "/original" ||
		second.Questions[0].Questions[0].Options[0].Label != "original" {
		t.Fatalf("snapshot internals were mutated: %#v", second)
	}
}

func TestCanonicalAssistantMessagesPreserveSnapshot(t *testing.T) {
	message := func(text string) core.Event {
		return core.Event{Type: core.EventAssistantMessage, Payload: core.MessagePayload{Content: text}}
	}
	delta := func(text string) core.Event {
		return core.Event{Type: core.EventAssistantDelta, Payload: core.DeltaPayload{Delta: text}}
	}
	system := func(text string) core.Event {
		return core.Event{Type: core.EventSystemMessage, Payload: core.MessagePayload{Content: text}}
	}
	for _, tc := range []struct {
		name   string
		events []core.Event
		want   string
	}{
		{"complete_only", []core.Event{message("answer")}, "answer"},
		{"correct_preview", []core.Event{delta("wrong"), message("right")}, "right"},
		{"stream_no_duplicate", []core.Event{delta("hello"), delta(" world"), message("hello world")}, "hello world"},
		{"multiple_messages", []core.Event{message("first"), delta("draft"), message("second")}, "firstsecond"},
		{"tool_boundary", []core.Event{delta("progress"), {Type: core.EventToolStarted, Payload: core.ToolPayload{CallID: "tool", ToolName: "read"}}, delta("draft"), message("answer")}, "progressanswer"},
		{"subagent_not_boundary", []core.Event{delta("pre"), {Type: core.EventToolStarted, Payload: core.ToolPayload{CallID: "tool", SubAgentID: "child"}}, delta("view"), message("answer")}, "answer"},
		{"empty_canonical", []core.Event{delta("draft"), message("")}, ""},
		{"system_interleaving", []core.Event{delta("pre"), system("notice"), delta("view"), message("answer")}, "answer\nnotice"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := NewRunStore(1)
			for _, event := range tc.events {
				event.RunID = "run"
				s.Record(event)
			}
			s.Record(core.Event{RunID: "run", Type: core.EventRunCancelled, Payload: core.RunResult{Error: "cancelled"}})
			got, _ := s.Lookup("run")
			if got.Output != tc.want {
				t.Fatalf("output=%q want=%q", got.Output, tc.want)
			}
		})
	}
}
