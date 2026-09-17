package runstore

import (
	"strings"
	"sync"
	"time"

	"nekocode/protocol"
	"nekocode/runtime/internal/core"
)

// DefaultLimit is the number of recent runs retained in memory.
const DefaultLimit = 100

type RunStore struct {
	mu    sync.Mutex
	limit int
	runs  map[core.RunID]*runRecord
	order []core.RunID
	last  core.RunID
}

type runRecord struct {
	snapshot     core.RunSnapshot
	toolStack    []int
	previewSpans []outputSpan
}

// Spans identify only the current model response's deltas in Output.
// System messages between deltas must survive canonical text replacement.
type outputSpan struct{ start, end int }

func NewRunStore(limit int) *RunStore {
	if limit <= 0 {
		limit = DefaultLimit
	}
	return &RunStore{
		limit: limit,
		runs:  make(map[core.RunID]*runRecord),
	}
}

func (s *RunStore) Record(ev core.Event) {
	if ev.RunID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	rec := s.record(ev.RunID)
	rec.snapshot.UpdatedAt = ev.Time
	rec.snapshot.EventCount++

	switch ev.Type {
	case core.EventInputAccepted:
		rec.snapshot.Status = core.RunRunning
		if rec.snapshot.StartedAt.IsZero() {
			rec.snapshot.StartedAt = ev.Time
		}
		if p, ok := ev.Payload.(core.MessagePayload); ok {
			rec.snapshot.Input = p.Content
			rec.snapshot.Source = p.Source
			rec.snapshot.Sender = p.Sender
		}
	case core.EventRunStarted:
		rec.snapshot.Status = core.RunRunning
		if rec.snapshot.StartedAt.IsZero() {
			rec.snapshot.StartedAt = ev.Time
		}
	case core.EventSystemMessage:
		if p, ok := ev.Payload.(core.MessagePayload); ok && p.Content != "" {
			if rec.snapshot.Output != "" {
				rec.snapshot.Output += "\n"
			}
			rec.snapshot.Output += p.Content
		}
	case core.EventAssistantDelta:
		if p, ok := ev.Payload.(core.DeltaPayload); ok {
			rec.appendPreview(p.Delta)
		}
	case core.EventAssistantMessage:
		if p, ok := ev.Payload.(core.MessagePayload); ok {
			rec.completeMessage(p.Content)
		}
	case core.EventReasoningDelta:
		if p, ok := ev.Payload.(core.DeltaPayload); ok {
			rec.snapshot.Reasoning += p.Delta
		}
	case core.EventTodosUpdated:
		if p, ok := ev.Payload.([]protocol.TodoItem); ok {
			rec.snapshot.Todos = append([]protocol.TodoItem(nil), p...)
		}
	case core.EventSubAgentStarted, core.EventSubAgentEnded:
		if p, ok := ev.Payload.(core.SubAgentPayload); ok {
			rec.applySubAgent(p, ev.Type == core.EventSubAgentStarted, ev.Time)
		}
	case core.EventPhaseChanged:
		if p, ok := ev.Payload.(core.PhasePayload); ok {
			rec.snapshot.Phase = p.Phase
		}
	case core.EventToolStarted:
		if p, ok := ev.Payload.(core.ToolPayload); ok {
			if p.SubAgentID == "" {
				rec.previewSpans = nil
			}
			rec.startTool(p, ev.Time)
		}
	case core.EventToolPreview:
		if p, ok := ev.Payload.(core.ToolPayload); ok {
			rec.applyToolPreview(p)
		}
	case core.EventToolCompleted, core.EventToolBlocked:
		if p, ok := ev.Payload.(core.ToolPayload); ok {
			if p.SubAgentID == "" {
				rec.previewSpans = nil
			}
			status := core.ToolDone
			if ev.Type == core.EventToolBlocked || p.IsError {
				status = core.ToolBlocked
			}
			rec.finishTool(p, status, ev.Time)
		}
	case core.EventApprovalRequested:
		if p, ok := ev.Payload.(core.ApprovalView); ok {
			rec.snapshot.Status = core.RunWaitingApproval
			rec.upsertApproval(p)
		}
	case core.EventApprovalResolved:
		if p, ok := ev.Payload.(core.ApprovalView); ok {
			rec.upsertApproval(p)
		}
		if rec.snapshot.Status == core.RunWaitingApproval {
			rec.snapshot.Status = core.RunRunning
		}
	case core.EventQuestionRequested:
		if p, ok := ev.Payload.(core.QuestionView); ok {
			rec.snapshot.Status = core.RunWaitingQuestion
			rec.upsertQuestion(p)
		}
	case core.EventQuestionResolved:
		if p, ok := ev.Payload.(core.QuestionView); ok {
			rec.upsertQuestion(p)
		}
		if rec.snapshot.Status == core.RunWaitingQuestion {
			rec.snapshot.Status = core.RunRunning
		}
	case core.EventRunDone:
		if p, ok := ev.Payload.(core.RunResult); ok {
			if p.Output != "" {
				rec.snapshot.Output = p.Output
			}
		}
		rec.finish(core.RunDone, ev.Time)
	case core.EventRunFailed:
		if p, ok := ev.Payload.(core.RunResult); ok {
			if p.Output != "" {
				rec.snapshot.Output = p.Output
			}
			rec.snapshot.Error = p.Error
		}
		rec.finish(core.RunFailed, ev.Time)
	case core.EventRunCancelled:
		if p, ok := ev.Payload.(core.RunResult); ok {
			rec.snapshot.Error = p.Error
		}
		rec.finish(core.RunCancelled, ev.Time)
	}
}

func (r *runRecord) appendPreview(delta string) {
	if delta == "" {
		return
	}
	start := len(r.snapshot.Output)
	r.snapshot.Output += delta
	n := len(r.previewSpans)
	if n > 0 && r.previewSpans[n-1].end == start {
		r.previewSpans[n-1].end = len(r.snapshot.Output)
	} else {
		r.previewSpans = append(r.previewSpans, outputSpan{start, len(r.snapshot.Output)})
	}
}

func (r *runRecord) completeMessage(content string) {
	if len(r.previewSpans) == 0 {
		r.snapshot.Output += content
		return
	}
	var output strings.Builder
	cursor := 0
	for i, span := range r.previewSpans {
		output.WriteString(r.snapshot.Output[cursor:span.start])
		if i == 0 {
			output.WriteString(content)
		}
		cursor = span.end
	}
	output.WriteString(r.snapshot.Output[cursor:])
	r.snapshot.Output = output.String()
	r.previewSpans = nil
}

func (s *RunStore) Current() (core.RunSnapshot, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.last == "" {
		return core.RunSnapshot{}, false
	}
	return s.lookupLocked(s.last)
}

func (s *RunStore) Lookup(id core.RunID) (core.RunSnapshot, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lookupLocked(id)
}

func (s *RunStore) List(limit int) []core.RunSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit <= 0 || limit > len(s.order) {
		limit = len(s.order)
	}
	out := make([]core.RunSnapshot, 0, limit)
	for i := len(s.order) - 1; i >= 0 && len(out) < limit; i-- {
		if snapshot, ok := s.lookupLocked(s.order[i]); ok {
			out = append(out, snapshot)
		}
	}
	return out
}

func (s *RunStore) record(id core.RunID) *runRecord {
	if rec, ok := s.runs[id]; ok {
		s.last = id
		return rec
	}
	now := time.Now()
	rec := &runRecord{
		snapshot: core.RunSnapshot{
			ID:        id,
			Status:    core.RunRunning,
			StartedAt: now,
			UpdatedAt: now,
		},
	}
	s.runs[id] = rec
	s.order = append(s.order, id)
	s.last = id
	if len(s.order) > s.limit {
		drop := s.order[0]
		s.order = s.order[1:]
		delete(s.runs, drop)
	}
	return rec
}

func (s *RunStore) lookupLocked(id core.RunID) (core.RunSnapshot, bool) {
	if id == "" {
		id = s.last
	}
	rec, ok := s.runs[id]
	if !ok {
		return core.RunSnapshot{}, false
	}
	return copyRunSnapshot(rec.snapshot), true
}

func (r *runRecord) startTool(p core.ToolPayload, at time.Time) {
	tool := core.ToolView{
		CallID:        p.CallID,
		Name:          p.ToolName,
		Args:          p.Args,
		Preview:       p.Preview,
		Status:        core.ToolRunning,
		StartedAt:     at,
		SubAgentID:    p.SubAgentID,
		SubAgentColor: p.SubAgentColor,
	}
	r.snapshot.Tools = append(r.snapshot.Tools, tool)
	r.toolStack = append(r.toolStack, len(r.snapshot.Tools)-1)
}

func (r *runRecord) applyToolPreview(p core.ToolPayload) {
	idx := r.findOpenTool(p.CallID, p.ToolName)
	if idx >= 0 {
		r.snapshot.Tools[idx].Preview = p.Preview
	}
}

func (r *runRecord) finishTool(p core.ToolPayload, status core.ToolStatus, at time.Time) {
	idx := r.findOpenTool(p.CallID, p.ToolName)
	if idx < 0 {
		r.startTool(p, at)
		idx = len(r.snapshot.Tools) - 1
	}
	r.snapshot.Tools[idx].Args = p.Args
	r.snapshot.Tools[idx].Output = p.Output
	r.snapshot.Tools[idx].IsError = p.IsError
	r.snapshot.Tools[idx].Status = status
	r.snapshot.Tools[idx].FinishedAt = &at
	r.removeToolStack(idx)
}

func (r *runRecord) findOpenTool(callID, name string) int {
	for i := len(r.toolStack) - 1; i >= 0; i-- {
		idx := r.toolStack[i]
		if idx < 0 || idx >= len(r.snapshot.Tools) {
			continue
		}
		tool := r.snapshot.Tools[idx]
		if callID != "" && tool.CallID == callID {
			return idx
		}
		if callID == "" && tool.Name == name {
			return idx
		}
	}
	return -1
}

func (r *runRecord) applySubAgent(p core.SubAgentPayload, started bool, at time.Time) {
	profile := p.Profile
	if profile == "" {
		profile = p.Type
	}
	for i := range r.snapshot.SubAgents {
		if r.snapshot.SubAgents[i].ID != p.ID {
			continue
		}
		if started {
			r.snapshot.SubAgents[i].Type = profile
			r.snapshot.SubAgents[i].Profile = profile
			r.snapshot.SubAgents[i].Skills = append([]string(nil), p.Skills...)
			r.snapshot.SubAgents[i].Color = p.Color
		}
		r.snapshot.SubAgents[i].Active = started
		if !started {
			r.snapshot.SubAgents[i].FinishedAt = &at
		}
		return
	}
	view := core.SubAgentView{
		ID: p.ID, Type: profile, Profile: profile, Skills: append([]string(nil), p.Skills...),
		Color: p.Color, Active: started, StartedAt: at,
	}
	if !started {
		view.FinishedAt = &at
	}
	r.snapshot.SubAgents = append(r.snapshot.SubAgents, view)
}

func (r *runRecord) removeToolStack(idx int) {
	for i := len(r.toolStack) - 1; i >= 0; i-- {
		if r.toolStack[i] == idx {
			r.toolStack = append(r.toolStack[:i], r.toolStack[i+1:]...)
			return
		}
	}
}

func (r *runRecord) upsertApproval(view core.ApprovalView) {
	view = copyApproval(view)
	for i := range r.snapshot.Approvals {
		if r.snapshot.Approvals[i].ID == view.ID {
			r.snapshot.Approvals[i] = view
			return
		}
	}
	r.snapshot.Approvals = append(r.snapshot.Approvals, view)
}

func (r *runRecord) upsertQuestion(view core.QuestionView) {
	view = copyQuestion(view)
	for i := range r.snapshot.Questions {
		if r.snapshot.Questions[i].ID == view.ID {
			r.snapshot.Questions[i] = view
			return
		}
	}
	r.snapshot.Questions = append(r.snapshot.Questions, view)
}

func (r *runRecord) finish(status core.RunStatus, at time.Time) {
	r.previewSpans = nil
	r.snapshot.Status = status
	r.snapshot.FinishedAt = &at
}

func copyRunSnapshot(in core.RunSnapshot) core.RunSnapshot {
	out := in
	out.FinishedAt = copyTime(in.FinishedAt)
	out.Tools = append([]core.ToolView(nil), in.Tools...)
	for i := range out.Tools {
		out.Tools[i].FinishedAt = copyTime(in.Tools[i].FinishedAt)
	}
	out.Todos = append([]protocol.TodoItem(nil), in.Todos...)
	out.SubAgents = append([]core.SubAgentView(nil), in.SubAgents...)
	for i := range out.SubAgents {
		out.SubAgents[i].FinishedAt = copyTime(in.SubAgents[i].FinishedAt)
		out.SubAgents[i].Skills = append([]string(nil), in.SubAgents[i].Skills...)
	}
	out.Approvals = make([]core.ApprovalView, len(in.Approvals))
	for i, approval := range in.Approvals {
		out.Approvals[i] = copyApproval(approval)
	}
	out.Questions = make([]core.QuestionView, len(in.Questions))
	for i, question := range in.Questions {
		out.Questions[i] = copyQuestion(question)
	}
	return out
}

func copyApproval(input core.ApprovalView) core.ApprovalView {
	output := input
	output.Args = copyMap(input.Args)
	output.Metadata = copyMap(input.Metadata)
	output.Approval = input.Approval.Clone()
	output.ResolvedAt = copyTime(input.ResolvedAt)
	return output
}

func copyQuestion(input core.QuestionView) core.QuestionView {
	output := input
	output.ResolvedAt = copyTime(input.ResolvedAt)
	output.Questions = append([]protocol.QuestionItem(nil), input.Questions...)
	for i := range output.Questions {
		output.Questions[i].Options = append([]protocol.QuestionOption(nil), input.Questions[i].Options...)
	}
	return output
}

func copyTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

func copyMap(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	output := make(map[string]any, len(input))
	for key, value := range input {
		output[key] = copyValue(value)
	}
	return output
}

func copyValue(value any) any {
	switch value := value.(type) {
	case map[string]any:
		return copyMap(value)
	case []any:
		output := make([]any, len(value))
		for i := range value {
			output[i] = copyValue(value[i])
		}
		return output
	case []string:
		return append([]string(nil), value...)
	default:
		return value
	}
}
