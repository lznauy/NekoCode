package refbridge

import (
	"context"
	"encoding/json"
	"fmt"

	"nekocode/bot/provider/types"
)

// Source supplies actor-owned durable history and the current skill registry.
// It retains no transcript, index, actor registry or mutable query state.
type Source struct {
	Page   func(context.Context, int, int, int, int) (SourcePage, error)
	Record func(context.Context, int, int) (types.Message, bool, error)
	Skill  func(string) (string, bool)
}

// referencePage merges a stable transcript tail with its acknowledged durable prefix.
func (s Source) referencePage(ctx context.Context, base int, tail []types.Message, before, limit, maxBytes int) (SourcePage, error) {
	end := base + len(tail)
	if before > 0 {
		end = min(end, before-1)
	}
	out := SourcePage{}
	for sequence := end; sequence > base; sequence-- {
		if err := ctx.Err(); err != nil {
			return SourcePage{}, err
		}
		msg := tail[sequence-base-1]
		if !HasReference(msg, sequence) {
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
		out.Records = append(out.Records, Record{Sequence: sequence, Message: msg})
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
	if s.Page == nil {
		return SourcePage{}, fmt.Errorf("reference history unavailable")
	}
	older, err := s.Page(ctx, before, remaining, bytes, min(base, end))
	if err != nil {
		return SourcePage{}, err
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
