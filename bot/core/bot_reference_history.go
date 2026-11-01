package core

import (
	"context"
	"encoding/json"

	"nekocode/bot/extension/tool/runtime/refbridge"
)

// Caller holds historyMu so durable and pending sources belong to one session.
func (b *Bot) referenceHistoryLocked(ctx context.Context, before, limit, maxBytes int) (refbridge.SourcePage, error) {
	base, tail, lazy := b.ctxMgr.TranscriptTail()
	if !lazy {
		base = 0
	}
	end := base + len(tail)
	if before > 0 {
		end = min(end, before-1)
	}
	out := refbridge.SourcePage{}
	for sequence := end; sequence > base; sequence-- {
		if err := ctx.Err(); err != nil {
			return refbridge.SourcePage{}, err
		}
		msg := tail[sequence-base-1]
		if !refbridge.HasReference(msg, sequence) {
			continue
		}
		if len(out.Records) == limit {
			out.HasMore = true
			return out, nil
		}
		data, _ := json.Marshal(msg)
		if len(out.Records) > 0 && out.Bytes+len(data) > maxBytes {
			out.HasMore = true
			return out, nil
		}
		out.Records = append(out.Records, refbridge.Record{Sequence: sequence, Message: msg})
		out.Before = sequence
		out.Bytes += len(data)
	}
	if base == 0 {
		return out, nil
	}
	// The index can establish whether older references exist without loading
	// another body when this page has already reached its count/byte budget.
	remaining := limit - len(out.Records)
	if out.Bytes >= maxBytes {
		remaining = 0
	}
	bytes := max(1, maxBytes-out.Bytes)
	older, err := b.sess.ReferencePage(ctx, before, remaining, bytes, min(base, end))
	if err != nil {
		return refbridge.SourcePage{}, err
	}
	if len(out.Records) == limit || len(out.Records) > 0 && out.Bytes+older.Bytes > maxBytes {
		out.HasMore = len(older.Records) > 0 || older.HasMore
		return out, nil
	}
	out.Records = append(out.Records, older.Records...)
	if len(older.Records) > 0 {
		out.Before = older.Before
	}
	out.Bytes += older.Bytes
	out.HasMore = older.HasMore
	return out, nil
}
