package refbridge

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/provider/types"
	"nekocode/protocol"
)

func evidenceRecord(actor protocol.ExecutionIdentity, n int) types.Message {
	receipts := make([]protocol.ExecutionReceipt, n)
	for i := range receipts {
		receipts[i] = protocol.ExecutionReceipt{ID: uuid.NewString(), Execution: actor, Tool: "shell", Status: "success", Artifact: protocol.ToolOutputArtifact{Path: "/private/never-return"}}
	}
	return types.Message{Role: "tool", ToolEvidence: protocol.ToolEvidence{Schema: 1, Executions: receipts, ReturnedHash: "returned", VisibleHash: "visible"}}
}

func evidenceContext(t *testing.T, actor protocol.ExecutionIdentity) context.Context {
	return protocol.WithExecutionIdentity(core.WithOutputStore(t.Context(), core.NewOutputStore(t.TempDir())), actor)
}

func TestEvidencePagingCapsReceiptsAndNeverSkipsRetryPairs(t *testing.T) {
	ctx := evidenceContext(t, protocol.ExecutionIdentity{})
	var tail []types.Message
	for range 23 {
		tail = append(tail, evidenceRecord(protocol.ExecutionIdentity{}, 2))
	}
	seen := map[string]bool{}
	before := 0
	for {
		page, err := (Source{}).Read(ctx, "parent", Query{Kind: EvidenceKind, Limit: 20, Before: before}, 0, tail)
		if err != nil || page.Scanned > 20 || len(page.Evidence) > 20 {
			t.Fatalf("page=%+v err=%v", page, err)
		}
		for _, item := range page.Evidence {
			if seen[item.ReceiptID] {
				t.Fatal("cursor repeated receipt")
			}
			seen[item.ReceiptID] = true
		}
		if !page.HasMore {
			break
		}
		if page.Before == 0 || page.Before == before {
			t.Fatal("cursor failed to advance")
		}
		before = page.Before
	}
	if len(seen) != 46 {
		t.Fatalf("lost retry receipts: %d", len(seen))
	}
}

func TestEvidenceInspectionIsScopedAndStripsAllArtifactPaths(t *testing.T) {
	actor := protocol.ExecutionIdentity{TaskID: "child", AttemptID: "current"}
	ctx := evidenceContext(t, actor)
	own := evidenceRecord(protocol.ExecutionIdentity{TaskID: "child", AttemptID: "previous"}, 2)
	tail := []types.Message{own, evidenceRecord(protocol.ExecutionIdentity{}, 1), evidenceRecord(protocol.ExecutionIdentity{TaskID: "sibling", AttemptID: "a"}, 1)}
	page, err := (Source{}).Read(ctx, "parent/subagents/child", Query{Kind: EvidenceKind, Limit: 20}, 0, tail)
	if err != nil || len(page.Evidence) != 2 {
		t.Fatalf("foreign evidence exposed: %+v %v", page, err)
	}
	id := page.Evidence[0].ID
	for _, scope := range []string{"parent/subagents/child", "other/subagents/child"} {
		inspected, err := (Source{}).Read(ctx, scope, Query{Kind: EvidenceKind, Limit: 20, ID: id}, 0, tail)
		if err != nil {
			t.Fatal(err)
		}
		if scope == "parent/subagents/child" {
			if inspected.Status != "recorded" || inspected.Evidence[0].Detail == nil {
				t.Fatal("own prior-attempt record unavailable")
			}
			data, _ := json.Marshal(inspected)
			if strings.Contains(string(data), "/private/") {
				t.Fatal("receipt returned artifact path")
			}
			if own.ToolEvidence.Executions[0].Artifact.Path == "" {
				t.Fatal("redaction modified authoritative receipt")
			}
		} else if len(inspected.Evidence) != 0 || inspected.Status == "recorded" {
			t.Fatal("cross-parent alias resolved")
		}
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := (Source{}).Read(canceled, "parent", Query{Kind: EvidenceKind, Limit: 20}, 0, tail); !errors.Is(err, context.Canceled) {
		t.Fatal("cancel ignored")
	}
	if _, err := (Source{}).Read(t.Context(), "parent", Query{Kind: EvidenceKind, Limit: 20}, 0, tail); err == nil {
		t.Fatal("unbound actor queried evidence")
	}
}

func TestEvidenceSparsePagesWatermarkAndSourceErrors(t *testing.T) {
	ctx := evidenceContext(t, protocol.ExecutionIdentity{})
	history := []types.Message{evidenceRecord(protocol.ExecutionIdentity{}, 1)}
	for range 25 {
		history = append(history, types.Message{Role: "user", Content: "ordinary"})
	}
	for split := 0; split <= len(history); split++ {
		source := Source{Record: func(_ context.Context, sequence, through int) (types.Message, bool, error) {
			if through != split || sequence > through {
				t.Fatal("durable query escaped acknowledged watermark")
			}
			return history[sequence-1], true, nil
		}}
		first, err := source.Read(ctx, "parent", Query{Kind: EvidenceKind, Limit: 20}, split, history[split:])
		if err != nil || len(first.Evidence) != 0 || !first.HasMore {
			t.Fatalf("sparse first page=%+v %v", first, err)
		}
		second, err := source.Read(ctx, "parent", Query{Kind: EvidenceKind, Limit: 20, Before: first.Before}, split, history[split:])
		if err != nil || len(second.Evidence) != 1 || second.HasMore {
			t.Fatalf("sparse older page=%+v %v", second, err)
		}
	}
	source := Source{Record: func(context.Context, int, int) (types.Message, bool, error) {
		return types.Message{}, false, errors.New("private-path-secret")
	}}
	if _, err := source.Read(ctx, "parent", Query{Kind: EvidenceKind, Limit: 20}, 1, nil); err == nil || strings.Contains(err.Error(), "private-path-secret") {
		t.Fatal("source error not redacted", err)
	}
}

func TestEvidenceSoftByteBudgetKeepsDeferredSourceReachable(t *testing.T) {
	ctx := evidenceContext(t, protocol.ExecutionIdentity{})
	older := evidenceRecord(protocol.ExecutionIdentity{}, 1)
	older.Content = strings.Repeat("x", 70*1024)
	newer := evidenceRecord(protocol.ExecutionIdentity{}, 1)
	tail := []types.Message{older, newer}
	first, err := (Source{}).Read(ctx, "parent", Query{Kind: EvidenceKind, Limit: 20}, 0, tail)
	if err != nil || len(first.Evidence) != 1 || first.Scanned != 2 || first.Before != 2 || !first.HasMore {
		t.Fatalf("wrong byte boundary: %+v %v", first, err)
	}
	next, err := (Source{}).Read(ctx, "parent", Query{Kind: EvidenceKind, Limit: 20, Before: first.Before}, 0, tail)
	if err != nil || len(next.Evidence) != 1 || next.Evidence[0].ReceiptID != older.ToolEvidence.Executions[0].ID || next.HasMore {
		t.Fatalf("lost oversized first record: %+v %v", next, err)
	}
	// New records cannot move an already returned exclusive cursor.
	tail = append(tail, evidenceRecord(protocol.ExecutionIdentity{}, 1))
	stable, err := (Source{}).Read(ctx, "parent", Query{Kind: EvidenceKind, Limit: 20, Before: first.Before}, 0, tail)
	if err != nil || stable.Evidence[0].ID != next.Evidence[0].ID {
		t.Fatal("append shifted existing cursor")
	}
}
