package core

import (
	"context"
	"fmt"

	toolcore "nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/refbridge"
	"nekocode/protocol"
)

func (b *Bot) readContextReferences(ctx context.Context, q refbridge.Query) (refbridge.Page, error) {
	if toolcore.OutputStoreFromContext(ctx) == nil {
		return refbridge.Page{}, fmt.Errorf("reference actor unavailable")
	}
	return b.contextReferences(ctx, b.sess.CurrentID(), q)
}

// contextReferences binds history, cursor and scope under the existing history
// lock. No new reference index or file permission is created.
func (b *Bot) contextReferences(ctx context.Context, sessionID string, q refbridge.Query) (refbridge.Page, error) {
	if err := ctx.Err(); err != nil {
		return refbridge.Page{}, err
	}
	if protocol.ExecutionFromContext(ctx).TaskID != "" {
		return refbridge.Page{}, fmt.Errorf("context_refs is limited to the main session actor")
	}
	b.historyMu.RLock()
	defer b.historyMu.RUnlock()
	if sessionID == "" || b.sess.CurrentID() != sessionID {
		return refbridge.Page{}, fmt.Errorf("reference scope unavailable")
	}
	// Output ownership also rejects legacy/standalone children with no TaskID.
	if store := toolcore.OutputStoreFromContext(ctx); store != nil && store != b.sessionOutput.store {
		return refbridge.Page{}, fmt.Errorf("reference actor unavailable")
	}
	if q.TaskID != "" || q.AttemptID != "" {
		if q.Kind != refbridge.EvidenceKind || q.TaskID == "" || q.AttemptID == "" || toolcore.OutputStoreFromContext(ctx) == nil {
			return refbridge.Page{}, fmt.Errorf("invalid attempt evidence query")
		}
		return b.readChildEvidence(ctx, sessionID, q)
	}
	base, tail, lazy := b.ctxMgr.TranscriptTail()
	if !lazy {
		base = 0
	}
	source := refbridge.Source{Page: b.sess.ReferencePage, Record: b.sess.ReferenceRecord, Skill: b.referenceSkill}
	return source.Read(ctx, sessionID, q, base, tail)
}

func (b *Bot) referenceSkill(name string) (string, bool) {
	if b.ext == nil {
		return "", false
	}
	skill, ok := b.ext.Skill(name)
	return skill.Context, ok
}

func (b *Bot) modelContextReferences(ctx context.Context, sessionID string) refbridge.Page {
	page, err := b.contextReferences(ctx, sessionID, refbridge.Query{Limit: refbridge.MaxPageRecords})
	if err != nil {
		return refbridge.Page{SessionID: sessionID, Unavailable: true}
	}
	return page
}
