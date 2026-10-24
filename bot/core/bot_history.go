package core

import (
	"encoding/json"
	"errors"

	"nekocode/bot/provider/types"
	"nekocode/bot/session"
)

// HistoryPage returns a bounded raw page. Zero cursor starts at the newest
// sequence; subsequent cursors remain stable when newer messages are appended.
func (b *Bot) HistoryPage(before, limit, maxBytes int) (session.HistoryPage, error) {
	if before < 0 || limit < 1 || limit > 1000 || maxBytes < 1 {
		return session.HistoryPage{}, errors.New("invalid history page")
	}
	b.historyMu.RLock()
	defer b.historyMu.RUnlock()
	base, tail, lazy := b.ctxMgr.TranscriptTail()
	if !lazy {
		base = 0
	}
	total := base + len(tail)
	end := total
	if before > 0 {
		end = min(end, before-1)
	}
	if end <= base {
		page, err := b.sess.HistoryPage(before, limit, maxBytes, base)
		page.Total = total
		return page, err
	}
	first := end
	bytes := 0
	for seq := end; seq > base && end-seq < limit; seq-- {
		data, _ := json.Marshal(tail[seq-base-1])
		if bytes > 0 && bytes+len(data) > maxBytes {
			break
		}
		bytes += len(data)
		first = seq
	}
	out := session.HistoryPage{End: end, Before: first, HasMore: first > 1, Total: total, Messages: append([]types.Message(nil), tail[first-base-1:end-base]...)}
	if first == base+1 && len(out.Messages) < limit && base > 0 && bytes < maxBytes {
		older, err := b.sess.HistoryPage(base+1, limit-len(out.Messages), maxBytes-bytes, base)
		if err != nil {
			return out, err
		}
		out.Messages = append(older.Messages, out.Messages...)
		out.Before = older.Before
		out.HasMore = older.HasMore
	}
	// A newly appended tool batch may straddle the durable/pending boundary.
	if len(out.Messages) > 0 && out.Messages[0].Role == "tool" {
		for i := first - base - 2; i >= 0; i-- {
			if tail[i].Role == "assistant" {
				header := tail[i]
				header.Content = ""
				out.Messages = append([]types.Message{header}, out.Messages...)
				break
			}
			if tail[i].Role != "tool" {
				break
			}
		}
		if out.Messages[0].Role == "tool" && base > 0 {
			older, err := b.sess.HistoryPage(base+1, 1, maxBytes, base)
			if err != nil {
				return out, err
			}
			for _, m := range older.Messages {
				if m.Role == "assistant" {
					m.Content = ""
					out.Messages = append([]types.Message{m}, out.Messages...)
					break
				}
			}
		}
	}
	return out, nil
}
