package core

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"nekocode/bot/contextmgr"
	toolcore "nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/refbridge"
	"nekocode/bot/provider/types"
	"nekocode/protocol"
)

func TestEvidenceQuerySurvivesSaveRestoreWithoutReturningPaths(t *testing.T) {
	b := newPersistTestBot(t)
	evidence := protocol.ToolEvidence{Schema: 1, Executions: []protocol.ExecutionReceipt{{ID: uuid.NewString(), Tool: "shell", Status: "error", Execution: protocol.ExecutionIdentity{RunID: "run"}, Artifact: protocol.ToolOutputArtifact{Path: "/private/receipt-path"}}}}
	b.ctxMgr.AddToolResultsBatch([]contextmgr.ToolResultMsg{{ToolName: "shell", Message: types.Message{ToolCallID: "old-call", Content: "display", ToolEvidence: evidence}}})
	ctx := referenceActorContext(t, b)
	page, err := b.readContextReferences(ctx, refbridge.Query{Kind: refbridge.EvidenceKind, Limit: 20})
	if err != nil || len(page.Evidence) != 1 {
		t.Fatalf("%+v %v", page, err)
	}
	id := page.Evidence[0].ID
	for range 2 {
		if err := b.saveSession(); err != nil {
			t.Fatal(err)
		}
		if err := b.ResumeSession(b.CurrentSessionID()); err != nil {
			t.Fatal(err)
		}
		result := b.ag.Executor().ExecuteBatch(t.Context(), []toolcore.ToolCallItem{{ID: "inspect", Name: "evidence", Args: map[string]any{"action": "inspect", "id": id}}})[0]
		if result.Error != "" || strings.Contains(result.Output, "/private/") {
			t.Fatalf("unsafe evidence output: %+v", result)
		}
		var inspected refbridge.Page
		if err := json.Unmarshal([]byte(result.Output), &inspected); err != nil || inspected.Status != "recorded" || inspected.Evidence[0].ReceiptID != evidence.Executions[0].ID {
			t.Fatalf("wrong restored evidence: %+v %v", inspected, err)
		}
	}
	if _, err := b.NewSession(); err != nil {
		t.Fatal(err)
	}
	inspected, err := b.readContextReferences(referenceActorContext(t, b), refbridge.Query{Kind: refbridge.EvidenceKind, Limit: 20, ID: id})
	if err != nil || inspected.Status != "missing" || len(inspected.Evidence) != 0 {
		t.Fatal("old session evidence resolved")
	}
}
