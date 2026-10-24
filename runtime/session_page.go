package runtime

import "fmt"

// SessionMessagePage is a page of visible messages, including coalesced tool
// turns. Offsets count display messages, rather than raw transcript entries.
type SessionMessagePage struct {
	Messages   []DisplayMessage `json:"messages"`
	NextOffset int              `json:"next_offset"`
	HasMore    bool             `json:"has_more"`
	Total      int              `json:"total"`
}

// SessionMessagesPage prefers an optional page projection. Legacy service
// providers remain compatible through the existing full-history callback.
func (r *Runtime) SessionMessagesPage(offset, limit int) (SessionMessagePage, error) {
	if offset < 0 || limit < 1 || limit > 1000 {
		return SessionMessagePage{}, fmt.Errorf("offset must be within history; limit must be 1..1000")
	}
	r.mu.Lock()
	page, all, closed := r.services.Sessions.SessionMessagesPage, r.services.Sessions.SessionMessages, r.closed
	r.mu.Unlock()
	if closed {
		return SessionMessagePage{}, protocolError(ErrorClosed, "session_messages_page", "closed")
	}
	if page != nil {
		return page(offset, limit)
	}
	var messages []DisplayMessage
	if all != nil {
		messages = all()
	}
	if offset > len(messages) {
		return SessionMessagePage{}, fmt.Errorf("offset must be within history; limit must be 1..1000")
	}
	end := offset + min(limit, len(messages)-offset)
	return SessionMessagePage{Messages: messages[offset:end], NextOffset: end, HasMore: end < len(messages), Total: len(messages)}, nil
}

// SessionHistoryWindow uses exclusive raw-sequence cursors. A zero cursor
// reads the newest window; Before can be reused after new messages arrive.
type SessionHistoryWindow struct {
	Messages []DisplayMessage `json:"messages"`
	Before   int              `json:"before"`
	End      int              `json:"end"`
	HasMore  bool             `json:"has_more"`
	Total    int              `json:"total"`
}

func (r *Runtime) SessionHistoryWindow(before, limit int) (SessionHistoryWindow, error) {
	if before < 0 || limit < 1 || limit > 1000 {
		return SessionHistoryWindow{}, fmt.Errorf("invalid history cursor or limit")
	}
	r.mu.Lock()
	callback, closed := r.services.Sessions.HistoryWindow, r.closed
	r.mu.Unlock()
	if closed {
		return SessionHistoryWindow{}, protocolError(ErrorClosed, "history_window", "closed")
	}
	if callback != nil {
		return callback(before, limit)
	}
	// Compatibility for custom runtimes that only provide the old full view.
	all := r.SessionMessages()
	end := len(all)
	if before > 0 {
		end = min(end, before-1)
	}
	first := max(0, end-limit)
	return SessionHistoryWindow{Messages: all[first:end], Before: first + 1, End: end, HasMore: first > 0, Total: len(all)}, nil
}
