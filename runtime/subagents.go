package runtime

import (
	"fmt"
	"nekocode/protocol"
	"slices"
	"sort"
	"strings"
	"time"
)

// SubagentSummary is a bounded task projection shared by all UI adapters.
type SubagentSummary struct {
	Isolation     string                  `json:"isolation,omitempty"`
	Recorded      bool                    `json:"recorded"`
	Title         string                  `json:"title,omitempty"`
	ID            string                  `json:"id"`
	SessionID     string                  `json:"session_id"`
	Profile       string                  `json:"profile"`
	Skills        []string                `json:"skills,omitempty"`
	Prompt        string                  `json:"prompt"`
	Status        string                  `json:"status"`
	Phase         string                  `json:"phase,omitempty"`
	Result        string                  `json:"result,omitempty"`
	Error         string                  `json:"error,omitempty"`
	RunCount      int                     `json:"run_count"`
	TranscriptSeq int                     `json:"transcript_seq"`
	CreatedAt     time.Time               `json:"created_at"`
	UpdatedAt     time.Time               `json:"updated_at"`
	LatestAttempt *SubagentAttemptSummary `json:"latest_attempt,omitempty"`
}

type SubagentAttemptSummary struct {
	ID              string    `json:"id"`
	Seq             int       `json:"seq"`
	Prompt          string    `json:"prompt"`
	Status          string    `json:"status"`
	Result          string    `json:"result,omitempty"`
	StartedAt       time.Time `json:"started_at"`
	EndedAt         time.Time `json:"ended_at,omitempty"`
	TranscriptStart int       `json:"transcript_start"`
	TranscriptEnd   int       `json:"transcript_end"`
	TotalTokens     int       `json:"total_tokens,omitempty"`
	CacheHitTokens  int       `json:"cache_hit_tokens,omitempty"`
	CacheMissTokens int       `json:"cache_miss_tokens,omitempty"`
	ToolUseCount    int       `json:"tool_use_count,omitempty"`
	DurationMs      int64     `json:"duration_ms,omitempty"`
	ContextTokens   int       `json:"context_tokens,omitempty"`
	ContextWindow   int       `json:"context_window,omitempty"`
}

type SubagentAttemptPage struct {
	SessionID  string                   `json:"session_id"`
	SubagentID string                   `json:"subagent_id"`
	Attempts   []SubagentAttemptSummary `json:"attempts"`
	Before     int                      `json:"before"`
	End        int                      `json:"end"`
	Total      int                      `json:"total"`
	HasMore    bool                     `json:"has_more"`
}

type SubagentFilter struct {
	SessionID string `json:"session_id,omitempty"`
	Query     string `json:"query,omitempty"`
	Status    string `json:"status,omitempty"`
	Offset    int    `json:"offset"`
	Limit     int    `json:"limit"`
}

type SubagentPage struct {
	Items      []SubagentSummary `json:"items"`
	SessionID  string            `json:"session_id"`
	Total      int               `json:"total"`
	NextOffset int               `json:"next_offset"`
	HasMore    bool              `json:"has_more"`
}

type SubagentToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// Records preserve chronological model/tool messages, including read/search
// calls normally omitted by the main conversation's final display projection.
type SubagentRecord struct {
	RuntimeEvent     bool               `json:"runtime_event,omitempty"`
	Sequence         int                `json:"sequence,omitempty"`
	ContextOnly      bool               `json:"context_only,omitempty"`
	Role             string             `json:"role"`
	Content          string             `json:"content"`
	Reasoning        string             `json:"reasoning,omitempty"`
	ToolCalls        []SubagentToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string             `json:"tool_call_id,omitempty"`
	IsError          bool               `json:"is_error,omitempty"`
	OutputPath       string             `json:"output_path,omitempty"`
	OutputIncomplete bool               `json:"output_incomplete,omitempty"`
}

type SubagentHistoryPage struct {
	SessionID  string           `json:"session_id"`
	SubagentID string           `json:"subagent_id"`
	Records    []SubagentRecord `json:"records"`
	Before     int              `json:"before"`
	End        int              `json:"end"`
	Total      int              `json:"total"`
	HasMore    bool             `json:"has_more"`
}

type SubagentReader interface {
	Subagents(SubagentFilter) (SubagentPage, error)
	SubagentHistory(sessionID, id string, before, limit int) (SubagentHistoryPage, error)
}

var _ SubagentReader = (*Runtime)(nil)

func liveAttemptSummary(live SubAgentView) *SubagentAttemptSummary {
	ended := time.Time{}
	if live.FinishedAt != nil {
		ended = *live.FinishedAt
	}
	return &SubagentAttemptSummary{ID: live.AttemptID, Seq: live.AttemptSeq, Prompt: live.Prompt, Status: live.Status, StartedAt: live.StartedAt, EndedAt: ended}
}

func committedSubagentStatus(status string) bool {
	return status == "completed" || status == "partial" || status == "failed"
}

func (r *Runtime) Subagents(filter SubagentFilter) (SubagentPage, error) {
	if filter.Offset < 0 || filter.Limit < 1 || filter.Limit > 1000 {
		return SubagentPage{}, fmt.Errorf("invalid subagent offset or limit")
	}
	r.mu.Lock()
	callback, closed := r.services.Sessions.Subagents, r.closed
	r.mu.Unlock()
	if closed {
		return SubagentPage{}, protocolError(ErrorClosed, "subagents", "closed")
	}
	if callback == nil {
		return SubagentPage{}, fmt.Errorf("subagent observation unavailable")
	}
	if filter.SessionID == "" {
		filter.SessionID = r.CurrentSessionID()
	}
	items, err := callback(filter.SessionID)
	if err != nil {
		return SubagentPage{}, err
	}
	items = slices.Clone(items)
	byID := make(map[string]int, len(items))
	for i := range items {
		if items[i].LatestAttempt != nil {
			copy := *items[i].LatestAttempt
			items[i].LatestAttempt = &copy
		}
		byID[items[i].ID] = i
	}
	latestAttempts := make(map[string]SubAgentView)
	latestLegacy := make(map[string]SubAgentView)
	latestQueued := make(map[string]SubAgentView)
	for _, live := range r.runs.Subagents(filter.SessionID) {
		if live.Status == "not_started" {
			continue
		}
		if live.AttemptID != "" {
			previous, ok := latestAttempts[live.ID]
			if !ok || live.AttemptSeq > previous.AttemptSeq || live.AttemptSeq == previous.AttemptSeq && live.AttemptID == previous.AttemptID && live.UpdatedAt.After(previous.UpdatedAt) {
				latestAttempts[live.ID] = live
			}
			continue
		}
		target := latestLegacy
		if live.Status == "queued" {
			target = latestQueued
		}
		if previous, ok := target[live.ID]; !ok || live.UpdatedAt.After(previous.UpdatedAt) {
			target[live.ID] = live
		}
	}
	ensure := func(live SubAgentView, withAttempt bool) int {
		if i, ok := byID[live.ID]; ok {
			return i
		}
		item := SubagentSummary{ID: live.ID, SessionID: filter.SessionID, Profile: live.Profile, Skills: live.Skills, Prompt: live.Prompt, Status: live.Status, Phase: live.Phase, CreatedAt: live.StartedAt, UpdatedAt: live.UpdatedAt}
		if withAttempt {
			item.RunCount = live.AttemptSeq
			item.LatestAttempt = liveAttemptSummary(live)
		}
		byID[live.ID] = len(items)
		items = append(items, item)
		return len(items) - 1
	}
	for _, live := range latestAttempts {
		i := ensure(live, true)
		item := &items[i]
		latest := item.LatestAttempt
		if latest != nil {
			if live.AttemptSeq < latest.Seq || live.AttemptSeq == latest.Seq && live.AttemptID != latest.ID {
				continue
			}
			if live.AttemptSeq == latest.Seq {
				if committedSubagentStatus(item.Status) {
					continue
				}
				item.Status, item.Phase, item.UpdatedAt = live.Status, live.Phase, live.UpdatedAt
				latest.Status = live.Status
				continue
			}
		} else if item.Recorded && live.AttemptSeq < item.RunCount {
			continue
		}
		item.Status, item.Phase, item.UpdatedAt = live.Status, live.Phase, live.UpdatedAt
		item.Result = ""
		item.RunCount = max(item.RunCount, live.AttemptSeq)
		item.LatestAttempt = liveAttemptSummary(live)
	}
	// Legacy events without attempt identity retain the old compatibility path,
	// but can never overwrite an attempt-aware persisted task.
	for _, live := range latestLegacy {
		i := ensure(live, false)
		if items[i].LatestAttempt != nil || items[i].UpdatedAt.After(live.UpdatedAt) {
			continue
		}
		if live.Active || live.Status == "failed" || items[i].Status == "running" || items[i].Status == "interrupted" {
			items[i].Status = live.Status
			items[i].Phase = live.Phase
			items[i].UpdatedAt = live.UpdatedAt
		}
	}
	for id, live := range latestQueued {
		if actual, ok := latestAttempts[id]; ok && (actual.Active || !live.UpdatedAt.After(actual.UpdatedAt)) {
			continue
		}
		i := ensure(live, false)
		items[i].Status, items[i].Phase, items[i].UpdatedAt = live.Status, live.Phase, live.UpdatedAt
	}
	query := strings.ToLower(strings.TrimSpace(filter.Query))
	selected := make([]SubagentSummary, 0, len(items))
	for _, item := range items {
		item.Prompt = protocol.SubagentTextPreview(item.Prompt, 1024)
		item.Phase = protocol.SubagentTextPreview(item.Phase, 1024)
		item.Result = protocol.SubagentTextPreview(item.Result, 4096)
		item.Error = protocol.SubagentTextPreview(item.Error, 1024)
		if item.LatestAttempt != nil {
			item.LatestAttempt.Prompt = protocol.SubagentTextPreview(item.LatestAttempt.Prompt, 1024)
			item.LatestAttempt.Result = protocol.SubagentTextPreview(item.LatestAttempt.Result, 4096)
		}
		if filter.Status != "" && item.Status != filter.Status {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(item.ID+" "+item.Profile+" "+item.Prompt), query) {
			continue
		}
		selected = append(selected, item)
	}
	sort.Slice(selected, func(i, j int) bool {
		if !selected[i].CreatedAt.Equal(selected[j].CreatedAt) {
			return selected[i].CreatedAt.Before(selected[j].CreatedAt)
		}
		return selected[i].ID < selected[j].ID
	})
	if filter.Offset > len(selected) {
		return SubagentPage{}, fmt.Errorf("subagent offset exceeds result count")
	}
	end := min(len(selected), filter.Offset+filter.Limit)
	return SubagentPage{SessionID: filter.SessionID, Items: selected[filter.Offset:end], Total: len(selected), NextOffset: end, HasMore: end < len(selected)}, nil
}

func (r *Runtime) SubagentHistory(sessionID, id string, before, limit int) (SubagentHistoryPage, error) {
	if id == "" || before < 0 || limit < 1 || limit > 1000 {
		return SubagentHistoryPage{}, fmt.Errorf("invalid subagent history cursor or limit")
	}
	r.mu.Lock()
	callback, closed := r.services.Sessions.SubagentHistory, r.closed
	r.mu.Unlock()
	if closed {
		return SubagentHistoryPage{}, protocolError(ErrorClosed, "subagent_history", "closed")
	}
	if callback == nil {
		return SubagentHistoryPage{}, fmt.Errorf("subagent history unavailable")
	}
	if sessionID == "" {
		sessionID = r.CurrentSessionID()
	}
	return callback(sessionID, id, before, limit)
}

// SubagentOutputReader pages the original artifact referenced by a committed
// tool result. No caller-supplied paths cross this boundary.
type SubagentAttemptReader interface {
	SubagentAttempts(sessionID, id string, before, limit int) (SubagentAttemptPage, error)
}

func (r *Runtime) SubagentAttempts(sessionID, id string, before, limit int) (SubagentAttemptPage, error) {
	if id == "" || before < 0 || limit < 1 || limit > 1000 {
		return SubagentAttemptPage{}, fmt.Errorf("invalid subagent attempt cursor or limit")
	}
	r.mu.Lock()
	callback, closed := r.services.Sessions.SubagentAttempts, r.closed
	r.mu.Unlock()
	if closed {
		return SubagentAttemptPage{}, protocolError(ErrorClosed, "subagent_attempts", "closed")
	}
	if callback == nil {
		return SubagentAttemptPage{}, fmt.Errorf("subagent attempts unavailable")
	}
	if sessionID == "" {
		sessionID = r.CurrentSessionID()
	}
	return callback(sessionID, id, before, limit)
}

type SubagentOutputReader interface {
	SubagentToolOutput(sessionID, id string, sequence int, offset int64) (SubagentOutputPage, error)
}

type SubagentOutputPage struct {
	Content    string `json:"content"`
	Offset     int64  `json:"offset"`
	NextOffset int64  `json:"next_offset"`
	Total      int64  `json:"total"`
	HasMore    bool   `json:"has_more"`
	Incomplete bool   `json:"incomplete"`
}

func (r *Runtime) SubagentToolOutput(sessionID, id string, sequence int, offset int64) (SubagentOutputPage, error) {
	if id == "" || sequence < 1 || sequence == int(^uint(0)>>1) || offset < 0 {
		return SubagentOutputPage{}, fmt.Errorf("invalid tool output cursor")
	}
	r.mu.Lock()
	callback, closed := r.services.Sessions.SubagentToolOutput, r.closed
	r.mu.Unlock()
	if closed {
		return SubagentOutputPage{}, protocolError(ErrorClosed, "subagent_output", "closed")
	}
	if callback == nil {
		return SubagentOutputPage{}, fmt.Errorf("stored tool output unavailable")
	}
	if sessionID == "" {
		sessionID = r.CurrentSessionID()
	}
	return callback(sessionID, id, sequence, offset)
}

// SubagentObservationAvailable distinguishes optional application services
// from the Runtime's always-present Go methods.
func (r *Runtime) SubagentObservationAvailable() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return !r.closed && r.services.Sessions.Subagents != nil && r.services.Sessions.SubagentHistory != nil
}
