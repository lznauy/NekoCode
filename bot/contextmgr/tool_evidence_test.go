package contextmgr

import (
	"strings"
	"testing"

	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/provider/types"
	"nekocode/protocol"
)

func TestToolEvidenceLivesOnlyInTranscriptWithInitialVisibleDigest(t *testing.T) {
	for _, deferred := range []bool{false, true} {
		t.Run(map[bool]string{false: "ready", true: "deferred"}[deferred], func(t *testing.T) {
			config := Config{ContextWindow: 10000}
			if deferred {
				config.SystemPrompt = strings.Repeat("x", 100000)
			}
			manager := New(config)
			manager.SetOutputStore(core.NewOutputStore(t.TempDir()))
			evidence := protocol.ToolEvidence{Schema: 1, ReturnedHash: "executor-digest", Executions: []protocol.ExecutionReceipt{{ID: "physical-uuid", Status: "success", OutputHash: "raw-digest"}}}
			manager.AddToolResultsBatch([]ToolResultMsg{{ToolName: "shell", Message: types.Message{ToolCallID: "call", Content: "returned content", ToolEvidence: evidence}}})
			snapshot := manager.Snapshot()
			active := snapshot.Messages[len(snapshot.Messages)-1]
			record := snapshot.Transcript[len(snapshot.Transcript)-1]
			if len(active.ToolEvidence.Executions) != 0 || len(record.ToolEvidence.Executions) != 1 || record.ToolEvidence.Executions[0].ID != "physical-uuid" {
				t.Fatal("evidence not confined to transcript")
			}
			expected := "ready"
			if deferred {
				expected = "deferred"
			}
			if record.ToolEvidence.Admission != expected || record.ToolEvidence.VisibleHash != core.EvidenceHash([]byte(active.Content)) || record.ToolEvidence.ReturnedHash != "executor-digest" {
				t.Fatalf("wrong admission: %+v", record.ToolEvidence)
			}
			evidence.Executions[0].ID = "caller mutation"
			if manager.Snapshot().Transcript[len(snapshot.Transcript)-1].ToolEvidence.Executions[0].ID != "physical-uuid" {
				t.Fatal("transcript retained caller-owned mutable slice")
			}
			restored := New(Config{})
			restored.Restore(snapshot)
			if got := restored.Snapshot().Transcript[len(snapshot.Transcript)-1].ToolEvidence; got.Executions[0].ID != "physical-uuid" || got.VisibleHash != record.ToolEvidence.VisibleHash {
				t.Fatal("restore rewrote execution evidence")
			}
		})
	}
}

func TestEvidenceDoesNotChangeProviderPrefixOrReturnAsActiveHistory(t *testing.T) {
	first := types.Message{Role: "tool", Content: "same model text", ToolCallID: "call"}
	second := first
	second.ToolEvidence = protocol.ToolEvidence{Schema: 1, Executions: []protocol.ExecutionReceipt{{ID: "metadata-only"}}}
	if digestJSON(canonicalMessages([]types.Message{first})) != digestJSON(canonicalMessages([]types.Message{second})) {
		t.Fatal("receipt invalidated provider prefix")
	}
	manager := New(Config{})
	manager.Restore(ManagerSnapshot{Messages: []types.Message{second}})
	snapshot := manager.Snapshot()
	if len(snapshot.Messages[0].ToolEvidence.Executions) != 0 || len(snapshot.Transcript[0].ToolEvidence.Executions) != 1 {
		t.Fatal("legacy/fallback restore lost source or exposed receipt to active context")
	}
}

func TestNonExecutionIsTranscriptOnlyAndSurvivesRestore(t *testing.T) {
	decision := core.NotExecuted(t.Context(), core.ToolCallItem{ID: "call", Name: "write"}, "governance", "blocked").NonExecution
	m := New(Config{})
	m.AddToolResultsBatch([]ToolResultMsg{{ToolName: "write", Message: types.Message{ToolCallID: "call", Content: "blocked", IsError: true, NonExecution: decision}}})
	snap := m.Snapshot()
	if snap.Messages[0].NonExecution.Schema != 0 || snap.Transcript[0].NonExecution != decision || snap.Transcript[0].ToolEvidence.Schema != 0 {
		t.Fatal("non-execution not separated from model/execution facts")
	}
	m.Restore(snap)
	if m.Snapshot().Transcript[0].NonExecution != decision {
		t.Fatal("restore lost decision")
	}
	plain := types.Message{Role: "tool", Content: "blocked", ToolCallID: "call"}
	withDecision := plain
	withDecision.NonExecution = decision
	if digestJSON(canonicalMessages([]types.Message{plain})) != digestJSON(canonicalMessages([]types.Message{withDecision})) {
		t.Fatal("decision changed model prefix")
	}
	m.Restore(ManagerSnapshot{Messages: []types.Message{withDecision}})
	if m.Snapshot().Messages[0].NonExecution.Schema != 0 || m.Snapshot().Transcript[0].NonExecution != decision {
		t.Fatal("legacy restore exposed or dropped local metadata")
	}
}

func TestToolEvidenceSnapshotAndRestoreDoNotShareReceiptSlices(t *testing.T) {
	record := func() types.Message {
		return types.Message{Role: "tool", ToolCallID: "call", Content: "result", ToolEvidence: protocol.ToolEvidence{Schema: 1, Executions: []protocol.ExecutionReceipt{{ID: "original", Status: "error"}}}}
	}
	for _, view := range []string{"snapshot", "tail", "restore", "legacy_restore"} {
		t.Run(view, func(t *testing.T) {
			manager := New(Config{})
			source := record()
			switch view {
			case "snapshot", "tail":
				manager.AddToolResultsBatch([]ToolResultMsg{{ToolName: "shell", Message: source}})
				var exported []types.Message
				if view == "snapshot" {
					exported = manager.Snapshot().Transcript
				} else {
					_, exported, _ = manager.TranscriptTail()
				}
				exported[0].ToolEvidence.Executions[0].ID = "caller-changed"
			case "restore":
				manager.Restore(ManagerSnapshot{Transcript: []types.Message{source}})
				source.ToolEvidence.Executions[0].ID = "caller-changed"
			case "legacy_restore":
				manager.Restore(ManagerSnapshot{Messages: []types.Message{source}})
				source.ToolEvidence.Executions[0].ID = "caller-changed"
			}
			snapshot := manager.Snapshot()
			if got := snapshot.Transcript[0].ToolEvidence.Executions[0].ID; got != "original" {
				t.Fatalf("caller mutated authoritative receipt: %s", got)
			}
		})
	}
}
