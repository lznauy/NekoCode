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
	"nekocode/bot/session"
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

func TestParentEvidenceQueryPinsTaskAttemptAndHandoffAssociations(t *testing.T) {
	b := newPersistTestBot(t)
	child, err := session.OpenSubagent(b.CurrentSessionID(), "child", b.cwd, false)
	if err != nil {
		t.Fatal(err)
	}
	defer child.Close()
	makeRecord := func(attempt string) types.Message {
		return types.Message{Role: "tool", ToolCallID: "same-call", Content: "returned", ToolEvidence: protocol.ToolEvidence{Schema: 1, Executions: []protocol.ExecutionReceipt{{ID: uuid.NewString(), Tool: "shell", Status: "error", Execution: protocol.ExecutionIdentity{TaskID: "child", AttemptID: attempt}, Artifact: protocol.ToolOutputArtifact{Path: "/private/child-artifact"}}}}}
	}
	if err := child.Begin("explore", "first", nil); err != nil {
		t.Fatal(err)
	}
	firstAttempt := child.Snapshot().Subagent.CurrentAttempt().ID
	if err := child.Checkpoint(contextmgr.ManagerSnapshot{Transcript: []types.Message{makeRecord(firstAttempt)}}, "completed", "done"); err != nil {
		t.Fatal(err)
	}
	ctx := referenceActorContext(t, b)
	q := refbridge.Query{Kind: refbridge.EvidenceKind, TaskID: "child", AttemptID: firstAttempt, Limit: 20}
	page, err := b.readContextReferences(ctx, q)
	if err != nil || len(page.Evidence) != 1 {
		t.Fatalf("first=%+v %v", page, err)
	}
	firstID := page.Evidence[0].ID
	if err := child.Begin("explore", "second", nil); err != nil {
		t.Fatal(err)
	}
	secondAttempt := child.Snapshot().Subagent.CurrentAttempt().ID
	if err := child.Checkpoint(contextmgr.ManagerSnapshot{TranscriptBase: 1, TranscriptLazy: true, Transcript: []types.Message{makeRecord(secondAttempt)}}, "completed", "done again"); err != nil {
		t.Fatal(err)
	}
	q.AttemptID = secondAttempt
	page, err = b.readContextReferences(ctx, q)
	if err != nil || len(page.Evidence) != 1 || page.Evidence[0].Sequence != 2 || page.HasMore {
		t.Fatalf("second escaped range=%+v %v", page, err)
	}
	secondID := page.Evidence[0].ID
	q.ID = firstID
	wrong, err := b.readContextReferences(ctx, q)
	if err != nil || wrong.Status != "wrong_scope" {
		t.Fatalf("cross attempt=%+v %v", wrong, err)
	}
	q.ID = secondID
	inspected, err := b.readContextReferences(ctx, q)
	if err != nil || inspected.Status != "recorded" {
		t.Fatalf("own attempt=%+v %v", inspected, err)
	}
	data, _ := json.Marshal(inspected)
	if strings.Contains(string(data), "/private/") {
		t.Fatal("parent query exposed child artifact path")
	}
	unknownID := "evidence.2." + strings.Repeat("a", 64)
	associations := b.handoffEvidence(ctx, b.CurrentSessionID(), "child", secondAttempt, []string{secondID, firstID, unknownID})
	for _, want := range []string{`"status":"resolved"`, `"execution_status":"error"`, `"status":"wrong_scope"`, `"status":"unknown"`} {
		if !strings.Contains(associations, want) {
			t.Fatalf("missing %s in %s", want, associations)
		}
	}
	if text := b.handoffEvidence(ctx, b.CurrentSessionID(), "child", "unknown-attempt", []string{secondID}); !strings.Contains(text, `"status":"error"`) {
		t.Fatal("unavailable source not surfaced")
	}
	childCtx := protocol.WithExecutionIdentity(ctx, protocol.ExecutionIdentity{TaskID: "other-child", AttemptID: "a"})
	if _, err := b.readContextReferences(childCtx, q); err == nil {
		t.Fatal("child selected sibling scope")
	}
	badStore := toolcore.WithOutputStore(ctx, toolcore.NewOutputStore(t.TempDir()))
	if _, err := b.readContextReferences(badStore, q); err == nil {
		t.Fatal("foreign executor selected child")
	}
	q.TaskID = "../child"
	if _, err := b.readContextReferences(ctx, q); err == nil || strings.Contains(err.Error(), b.cwd) {
		t.Fatal("path selector accepted or leaked", err)
	}
	parentBeforeSwitch := b.CurrentSessionID()
	if _, err := b.NewSession(); err != nil {
		t.Fatal(err)
	}
	if output := b.handoffEvidence(referenceActorContext(t, b), parentBeforeSwitch, "child", secondAttempt, []string{secondID}); !strings.Contains(output, `"status":"error"`) {
		t.Fatal("association did not retain its original parent scope")
	}
	q.TaskID = "child"
	if _, err := b.readContextReferences(referenceActorContext(t, b), q); err == nil {
		t.Fatal("parent switch retained child authority")
	}
}
