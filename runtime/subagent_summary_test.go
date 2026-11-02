package runtime

import (
	"context"
	"testing"

	"nekocode/protocol"
	"nekocode/runtime/internal/recording"
)

func TestSubagentSummaryRecordingKeepsAttemptIdentity(t *testing.T) {
	want := protocol.RunSummary{StepCount: 7, StopReason: "step_limit"}
	rt := New(RunnerFunc(func(_ context.Context, _ string, host RunHost) (string, error) {
		host.Step(protocol.StepEvent{Action: protocol.StepActionSubAgentStart, SubAgentID: "child", SubAgentAttemptID: "attempt", SubAgentAttemptSeq: 2, SubAgentStatus: "running"})
		host.Step(protocol.StepEvent{Action: protocol.StepActionSubAgentEnd, SubAgentID: "child", SubAgentAttemptID: "attempt", SubAgentAttemptSeq: 2, SubAgentStatus: "partial", Summary: &want})
		host.Step(protocol.StepEvent{Action: protocol.StepActionRunSummary, Summary: &protocol.RunSummary{StepCount: 1, StopReason: "completed"}})
		return "parent done", nil
	}), ServiceGroups{})
	defer rt.Close()
	if err := rt.EnableEventRecording(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	id, err := rt.StartRun(t.Context(), Input{Text: "inspect"})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.WaitRun(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	snapshot, ok := rt.LookupRun(id)
	if !ok || len(snapshot.SubAgents) != 1 || snapshot.SubAgents[0].Summary != want {
		t.Fatalf("snapshot=%+v", snapshot)
	}
	if got := liveAttemptSummary(snapshot.SubAgents[0]); got.Summary != want || got.ID != "attempt" {
		t.Fatalf("attempt=%+v", got)
	}
	rt.mu.Lock()
	path := rt.archivedRuns[id].Path
	rt.mu.Unlock()
	var child SubAgentPayload
	var main RunSummary
	if err := recording.WalkRecordedEventFile(path, func(event Event) {
		if event.Type == EventSubAgentEnded {
			child = event.Payload.(SubAgentPayload)
		}
		if event.Type == EventRunSummary {
			main = event.Payload.(RunSummary)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if child.ID != "child" || child.AttemptID != "attempt" || child.AttemptSeq != 2 || child.Summary != want {
		t.Fatalf("recorded=%+v", child)
	}
	if main.StopReason != "completed" || main.StepCount != 1 {
		t.Fatalf("parent summary overwritten: %+v", main)
	}
}

func TestNonExecutionSurvivesExistingEventRecordingCodec(t *testing.T) {
	decision := protocol.ToolNonExecution{Schema: 1, ID: "decision", Execution: protocol.ExecutionIdentity{RunID: "run", TaskID: "child", AttemptID: "attempt"}, CallID: "call", Tool: "write", Stage: "governance", Outcome: "blocked", ReasonHash: "hash"}
	rt := New(RunnerFunc(func(_ context.Context, _ string, host RunHost) (string, error) {
		host.Step(protocol.StepEvent{Action: protocol.StepActionToolBlocked, ToolName: "write", CallID: "call", SubAgentID: "child", SubAgentAttemptID: "attempt", Output: "blocked", NonExecution: decision})
		return "done", nil
	}), ServiceGroups{})
	defer rt.Close()
	if err := rt.EnableEventRecording(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	id, err := rt.StartRun(t.Context(), Input{Text: "run"})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.WaitRun(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	rt.mu.Lock()
	path := rt.archivedRuns[id].Path
	rt.mu.Unlock()
	var got ToolPayload
	if err := recording.WalkRecordedEventFile(path, func(event Event) {
		if event.Type == EventToolBlocked {
			got = event.Payload.(ToolPayload)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if got.NonExecution != decision || got.Evidence.Schema != 0 || got.SubAgentID != "child" || got.AttemptID != "attempt" {
		t.Fatalf("recorded=%+v", got)
	}
}
