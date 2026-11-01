package core

import (
	"context"
	"encoding/json"
	"fmt"

	"nekocode/bot/extension/tool/runtime/refbridge"
	"nekocode/bot/session"
	"nekocode/protocol"
)

// Caller holds historyMu and has authenticated the current main actor/store.
func (b *Bot) readChildEvidence(ctx context.Context, parent string, q refbridge.Query) (refbridge.Page, error) {
	source, first, last, err := session.SubagentEvidenceSource(ctx, parent, q.TaskID, q.AttemptID)
	if cancelErr := ctx.Err(); cancelErr != nil {
		return refbridge.Page{}, cancelErr
	}
	if err != nil {
		return refbridge.Page{}, fmt.Errorf("child evidence unavailable")
	}
	scope := parent + "/subagents/" + q.TaskID
	return source.ReadAttemptEvidence(ctx, scope, q, first, last, protocol.ExecutionIdentity{TaskID: q.TaskID, AttemptID: q.AttemptID})
}

// This is a bounded projection for the parent, never a second evidence store.
func (b *Bot) handoffEvidence(ctx context.Context, parent, task, attempt string, ids []string) string {
	type association struct {
		ID              string `json:"id"`
		Status          string `json:"status"`
		ReceiptID       string `json:"receipt_id,omitempty"`
		ExecutionStatus string `json:"execution_status,omitempty"`
	}
	results := make([]association, 0, len(ids))
	for _, id := range ids {
		item := association{ID: id, Status: "unknown"}
		page, err := b.contextReferences(ctx, parent, refbridge.Query{Kind: refbridge.EvidenceKind, TaskID: task, AttemptID: attempt, ID: id, Limit: 1})
		switch {
		case err != nil:
			item.Status = "error"
		case page.Status == "wrong_scope":
			item.Status = "wrong_scope"
		case page.Status == "recorded" && len(page.Evidence) == 1:
			item.Status = "resolved"
			item.ReceiptID = page.Evidence[0].ReceiptID
			item.ExecutionStatus = page.Evidence[0].Status
		}
		results = append(results, item)
	}
	data, _ := json.Marshal(struct {
		TaskID       string        `json:"task_id"`
		AttemptID    string        `json:"attempt_id"`
		Associations []association `json:"associations"`
	}{task, attempt, results})
	return "\n\n[Execution evidence associations — association only, not task verification]\n" + string(data)
}
