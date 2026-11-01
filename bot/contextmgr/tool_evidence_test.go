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
