package session

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"nekocode/bot/contextmgr"
	"nekocode/bot/provider/types"
	"nekocode/protocol"
)

func TestSubagentEvidenceSourceUsesCommittedAttemptWithoutWritingHistory(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	child, err := OpenSubagent("parent", "child", "/workspace", false)
	if err != nil {
		t.Fatal(err)
	}
	defer child.Close()
	if err := child.Begin("explore", "first", nil); err != nil {
		t.Fatal(err)
	}
	firstID := child.Snapshot().Subagent.CurrentAttempt().ID
	history := make([]types.Message, 60)
	for i := range history {
		history[i] = types.Message{Role: "user", Content: "ordinary history"}
	}
	history[1] = types.Message{Role: "tool", ToolCallID: "first", ToolEvidence: protocol.ToolEvidence{Schema: 1, Executions: []protocol.ExecutionReceipt{{ID: uuid.NewString(), Tool: "read", Execution: protocol.ExecutionIdentity{TaskID: "child", AttemptID: firstID}}}}}
	if err := child.Checkpoint(contextmgr.ManagerSnapshot{Messages: history[:1], Transcript: history}, "completed", "first result"); err != nil {
		t.Fatal(err)
	}
	if err := child.Begin("explore", "second", nil); err != nil {
		t.Fatal(err)
	}
	secondID := child.Snapshot().Subagent.CurrentAttempt().ID
	second := types.Message{Role: "tool", ToolCallID: "second", ToolEvidence: protocol.ToolEvidence{Schema: 1, Executions: []protocol.ExecutionReceipt{{ID: uuid.NewString(), Tool: "read", Execution: protocol.ExecutionIdentity{TaskID: "child", AttemptID: secondID}}}}}
	if err := child.Checkpoint(contextmgr.ManagerSnapshot{Messages: history[:1], TranscriptBase: 60, TranscriptLazy: true, Transcript: []types.Message{second}}, "running", ""); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(child.root, "child")
	// Simulate append-before-metadata commit. The parent must not recover it.
	file, err := os.OpenFile(filepath.Join(dir, transcriptFileName), os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.NewEncoder(file).Encode(transcriptRecord{Seq: 62, Message: second}); err != nil {
		t.Fatal(err)
	}
	file.Close()
	idx := filepath.Join(dir, "transcript.idx")
	if err := os.Remove(idx); err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	original, _ := os.ReadFile(filepath.Join(dir, transcriptFileName))
	source, first, last, err := SubagentEvidenceSource(t.Context(), "parent", "child", firstID)
	if err != nil || first != 1 || last != 60 {
		t.Fatalf("range=%d..%d %v", first, last, err)
	}
	msg, found, err := source.Record(t.Context(), 2, last)
	if err != nil || !found || msg.ToolEvidence.Executions[0].Execution.AttemptID != firstID {
		t.Fatalf("record=%+v %v %v", msg, found, err)
	}
	for _, sequence := range []int{61, 62} {
		if _, found, err := source.Record(t.Context(), sequence, last); err != nil || found {
			t.Fatal("attempt escaped committed range", sequence, err)
		}
	}
	_, first, last, err = SubagentEvidenceSource(t.Context(), "parent", "child", secondID)
	if err != nil || first != 61 || last != 61 {
		t.Fatalf("second range=%d..%d %v", first, last, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	during := &cancelReferenceRebuild{Context: ctx, cancel: cancel, checks: 25}
	if _, _, err := source.Record(during, 60, 60); !errors.Is(err, context.Canceled) {
		t.Fatal("cold history scan ignored cancellation", err)
	}
	after, _ := os.ReadFile(filepath.Join(dir, transcriptFileName))
	if string(after) != string(original) {
		t.Fatal("query repaired uncommitted source")
	}
	if _, err := os.Stat(idx); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("parent query wrote writer index", err)
	}
	if _, _, _, err := SubagentEvidenceSource(t.Context(), "other-parent", "child", firstID); err == nil {
		t.Fatal("foreign parent source allowed")
	}
	if _, _, _, err := SubagentEvidenceSource(t.Context(), "parent", "../child", firstID); err == nil {
		t.Fatal("path selector accepted")
	}
	if _, _, _, err := SubagentEvidenceSource(t.Context(), "parent", "child", "unknown"); err == nil {
		t.Fatal("unknown attempt allowed")
	}
}
