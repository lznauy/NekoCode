package policy

import (
	"nekocode/protocol"
	"testing"
)

func TestRecordToolsPublishesConcreteToolOutcome(t *testing.T) {
	p := New()
	var got ToolFacts
	p.Register(Hook{
		Name: "capture", Point: PostToolUse,
		On: func(state State) *Result {
			got = state.Facts().Tool
			return nil
		},
	})
	p.RecordTools([]ToolResult{{Name: "shell", Args: map[string]any{"command": "false"}, Error: "exit 1"}})

	if got.Name != "shell" || !got.Error {
		t.Fatalf("tool facts = %+v", got)
	}
}

func TestFailedAndBlockedEditsDoNotCountAsModifications(t *testing.T) {
	p := New()
	p.RecordTools([]ToolResult{
		{Name: "edit", Args: map[string]any{"path": "x.go"}, Error: "anchor not found"},
		{Name: "write", Args: map[string]any{"path": "y.go"}, Blocked: true},
	})

	if got := p.Snapshot().ModifiedFiles; len(got) != 0 {
		t.Fatalf("modified files = %+v, want none", got)
	}
}

func TestPolicySnapshotRestoreDoesNotTrustHistoricalReads(t *testing.T) {
	p := New()
	p.RecordTool(ToolResult{Name: "read", Args: map[string]any{"path": "main.go"}})
	snapshot := p.Snapshot()

	restored := New()
	restored.Restore(snapshot)
	if restored.ledger.WasRead("main.go") {
		t.Fatal("restored historical read became current-run authorization")
	}
}

func TestBeforeToolHasNoHeuristicQuota(t *testing.T) {
	p := New()
	for i := 0; i < 10; i++ {
		if results := p.BeforeTool(ToolRequest{Name: "read"}); len(results) != 0 {
			t.Fatalf("read %d unexpectedly blocked: %+v", i+1, results)
		}
	}
}

func TestProcessFailureRemainsDistinctFromToolError(t *testing.T) {
	p := New()
	var got ToolFacts
	p.Register(Hook{Name: "capture", Point: PostToolUse, On: func(s State) *Result { got = s.Facts().Tool; return nil }})
	p.RecordTools([]ToolResult{{Name: "shell", Process: protocol.ProcessObservation{State: "exited", ExitCodeKnown: true, ExitCode: 7}}})
	if got.Error || !got.Process.FailedExit() || got.Process.ExitCode != 7 {
		t.Fatalf("unexpected post-tool facts: %+v", got)
	}
	failures := p.Snapshot().ToolErrors
	if len(failures) != 1 || failures[0].Process.ExitCode != 7 || failures[0].Error != "" {
		t.Fatalf("unexpected ledger: %+v", failures)
	}
}
