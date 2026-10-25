package runstore

import (
	"encoding/json"
	"strings"
	"sync"
	"time"

	"nekocode/protocol"
	"nekocode/runtime/internal/core"
)

// DefaultLimit is the number of recent runs retained in memory.
const DefaultLimit = 100
const DefaultBytes = 8 << 20

type RunStore struct {
	mu    sync.Mutex
	limit int
	runs  map[core.RunID]*runRecord
	order []core.RunID
	last  core.RunID
}

type runRecord struct {
	snapshot      core.RunSnapshot
	metadataBytes int
	// Builders are append-only between replacements, so strings already handed
	// to snapshot readers remain immutable. Never copy a non-empty builder.
	output       strings.Builder
	reasoning    strings.Builder
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
			// Keep the first concrete source: later frames may carry an empty
			// SourceRef and must not erase who started the run.
			if p.Source.Kind != "" {
				rec.snapshot.Source = p.Source
			}
			if p.Sender.ID != "" || p.Sender.Username != "" || p.Sender.Display != "" {
				rec.snapshot.Sender = p.Sender
			}
		}
	case core.EventRunStarted:
		rec.snapshot.Status = core.RunRunning
		if rec.snapshot.StartedAt.IsZero() {
			rec.snapshot.StartedAt = ev.Time
		}
		// The runtime tags this frame with "runtime", which is the transport
		// that emitted it, not who started the run. Only a concrete caller
		// source overrides the later input_accepted payload.
		if ev.Source.Kind != "" && ev.Source.Kind != "runtime" {
			rec.snapshot.Source = ev.Source
		}
	case core.EventSystemMessage:
		if p, ok := ev.Payload.(core.MessagePayload); ok && p.Content != "" {
			if rec.snapshot.Output != "" {
				rec.appendOutput("\n")
			}
			rec.appendOutput(p.Content)
		}
	case core.EventAssistantDelta:
		if p, ok := ev.Payload.(core.DeltaPayload); ok {
			rec.appendPreview(p.Delta)
		}
	case core.EventAssistantMessage:
		if p, ok := ev.Payload.(core.MessagePayload); ok {
			rec.completeMessage(p.Content)
		}
	case core.EventAssistantReset:
		rec.completeMessage("")
	case core.EventReasoningDelta:
		if p, ok := ev.Payload.(core.DeltaPayload); ok {
			rec.reasoning.WriteString(p.Delta)
			rec.snapshot.Reasoning = rec.reasoning.String()
		}
	case core.EventTodosUpdated:
		if p, ok := ev.Payload.([]protocol.TodoItem); ok {
			rec.snapshot.Todos = append([]protocol.TodoItem(nil), p...)
		}
	case core.EventSubAgentStarted, core.EventSubAgentEnded, core.EventSubAgentUpdated:
		if p, ok := ev.Payload.(core.SubAgentPayload); ok {
			rec.applySubAgent(p, ev.Type, ev.Time)
		}
	case core.EventPhaseChanged:
		if p, ok := ev.Payload.(core.PhasePayload); ok {
			rec.snapshot.Phase = p.Phase
		}
	case core.EventContextUpdated:
		// Live context composition is observation-only; it must not mutate the
		// run snapshot that replay and session restore depend on.
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
	case core.EventApprovalRequested, core.EventApprovalResolved:
		if p, ok := ev.Payload.(core.ApprovalView); ok {
			rec.upsertApproval(p)
		}
	case core.EventQuestionRequested, core.EventQuestionResolved:
		if p, ok := ev.Payload.(core.QuestionView); ok {
			rec.upsertQuestion(p)
		}
	case core.EventRunDone:
		if p, ok := ev.Payload.(core.RunResult); ok {
			if p.Output != "" {
				rec.replaceOutput(p.Output)
			}
		}
		rec.finish(core.RunDone, ev.Time)
	case core.EventRunFailed:
		if p, ok := ev.Payload.(core.RunResult); ok {
			if p.Output != "" {
				rec.replaceOutput(p.Output)
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
	switch ev.Type {
	case core.EventSubAgentStarted, core.EventSubAgentUpdated,
		core.EventApprovalRequested, core.EventApprovalResolved,
		core.EventQuestionRequested, core.EventQuestionResolved:
		rec.refreshSubagentWaits(ev.Time)
		if ev.Type == core.EventApprovalRequested || ev.Type == core.EventApprovalResolved || ev.Type == core.EventQuestionRequested || ev.Type == core.EventQuestionResolved {
			rec.refreshParentWaits()
		}
	}

	switch ev.Type {
	case core.EventTodosUpdated, core.EventSubAgentStarted, core.EventSubAgentEnded, core.EventSubAgentUpdated,
		core.EventApprovalRequested, core.EventApprovalResolved, core.EventQuestionRequested, core.EventQuestionResolved:
		rec.metadataBytes = snapshotMetadataBytes(rec.snapshot)
	}
	if ev.Type == core.EventRunDone || ev.Type == core.EventRunFailed || ev.Type == core.EventRunCancelled {
		s.evictBytesLocked()
	}

}

func (r *runRecord) appendPreview(delta string) {
	if delta == "" {
		return
	}
	start := len(r.snapshot.Output)
	r.appendOutput(delta)
	n := len(r.previewSpans)
	if n > 0 && r.previewSpans[n-1].end == start {
		r.previewSpans[n-1].end = len(r.snapshot.Output)
	} else {
		r.previewSpans = append(r.previewSpans, outputSpan{start, len(r.snapshot.Output)})
	}
}

func (r *runRecord) replaceOutput(content string) {
	// Canonical strings are immutable and can be shared with the event history.
	// Drop preview storage; a later append seeds a fresh builder on demand.
	r.output.Reset()
	r.snapshot.Output = content
}

func (r *runRecord) appendOutput(content string) {
	if content == "" {
		return
	}
	if r.output.Len() == 0 && r.snapshot.Output != "" {
		r.output.WriteString(r.snapshot.Output)
	}
	r.output.WriteString(content)
	r.snapshot.Output = r.output.String()
}

func (r *runRecord) completeMessage(content string) {
	if len(r.previewSpans) == 0 {
		r.appendOutput(content)
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
	r.replaceOutput(output.String())
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
	s.evictBytesLocked()
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

func (r *runRecord) applySubAgent(p core.SubAgentPayload, kind core.EventType, at time.Time) {
	p.Prompt = protocol.SubagentTextPreview(p.Prompt, 1024)
	p.Phase = protocol.SubagentTextPreview(p.Phase, 1024)
	started := kind != core.EventSubAgentEnded
	if p.Status == "" {
		if started {
			p.Status = "running"
		} else {
			p.Status = "unknown"
		}
	}
	profile := p.Profile
	if profile == "" {
		profile = p.Type
	}
	match := -1
	// The newest placeholder belongs to the latest queued execution. Older
	// rejected resumes have no attempt ID and must not consume its end event.
	for i := len(r.snapshot.SubAgents) - 1; i >= 0; i-- {
		view := &r.snapshot.SubAgents[i]
		if view.ID != p.ID {
			continue
		}
		if p.AttemptID != "" {
			if view.AttemptID == p.AttemptID {
				match = i
				break
			}
			continue
		}
		// Empty attempt identities are queued placeholders or legacy events.
		// Never let them overwrite an admitted attempt.
		if view.AttemptID != "" || p.Status == "queued" && !view.Active {
			continue
		}
		match = i
		break
	}
	if match < 0 && p.AttemptID != "" && kind == core.EventSubAgentStarted {
		for i := range r.snapshot.SubAgents {
			view := &r.snapshot.SubAgents[i]
			if view.ID == p.ID && view.AttemptID == "" && view.Active && view.Status == "queued" {
				match = i
				view.AttemptID, view.AttemptSeq = p.AttemptID, p.AttemptSeq
				break
			}
		}
	}
	if match >= 0 {
		view := &r.snapshot.SubAgents[match]
		if kind == core.EventSubAgentUpdated && !view.Active {
			return
		}
		if kind == core.EventSubAgentStarted {
			view.FinishedAt = nil
			if p.SessionID != "" {
				view.SessionID = p.SessionID
			}
			view.Type = profile
			view.Profile = profile
			view.Skills = append([]string(nil), p.Skills...)
			view.Color = p.Color
			view.AttemptID, view.AttemptSeq = p.AttemptID, p.AttemptSeq
		}
		if p.Status != "" {
			view.Status = p.Status
		}
		if p.Phase != "" {
			view.Phase = p.Phase
		}
		if p.Prompt != "" {
			view.Prompt = p.Prompt
		}
		view.UpdatedAt = at
		view.Active = started
		if !started {
			view.FinishedAt = &at
		}
		return
	}
	view := core.SubAgentView{
		ID: p.ID, Type: profile, Profile: profile, Skills: append([]string(nil), p.Skills...), SessionID: p.SessionID,
		Color: p.Color, Active: started, StartedAt: at,
		Status: p.Status, Phase: p.Phase, Prompt: p.Prompt, UpdatedAt: at,
		AttemptID: p.AttemptID, AttemptSeq: p.AttemptSeq,
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
	for i := range r.snapshot.SubAgents {
		agent := &r.snapshot.SubAgents[i]
		if !agent.Active {
			continue
		}
		agent.Active = false
		agent.Status = "interrupted"
		agent.FinishedAt = &at
		agent.UpdatedAt = at
	}
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

func snapshotBytes(v core.RunSnapshot) int {
	return snapshotTextBytes(v) + snapshotMetadataBytes(v)
}

func snapshotTextBytes(v core.RunSnapshot) int {
	n := len(v.ID) + len(v.Input) + len(v.Output) + len(v.Reasoning) + len(v.Error) + len(v.Phase) +
		len(v.Source.Kind) + len(v.Source.ID) + len(v.Sender.ID) + len(v.Sender.Username) + len(v.Sender.Display) + 512
	for _, tool := range v.Tools {
		n += len(tool.Args) + len(tool.Preview) + len(tool.Output) + len(tool.Name) + len(tool.CallID) + len(tool.SubAgentID) + 256
	}
	return n
}

// Only metadata-changing events recalculate this estimate; streaming text
// never serializes structured controls or existing large outputs.
func snapshotMetadataBytes(v core.RunSnapshot) int {
	n := 0
	for _, todo := range v.Todos {
		n += 64 + len(todo.Content)
	}
	for _, agent := range v.SubAgents {
		n += 128 + len(agent.ID) + len(agent.Type) + len(agent.Profile) + len(agent.Status) + len(agent.Phase) + len(agent.Prompt)
		for _, skill := range agent.Skills {
			n += 16 + len(skill)
		}
	}
	if len(v.Approvals) > 0 {
		data, _ := json.Marshal(v.Approvals)
		n += len(data)
	}
	if len(v.Questions) > 0 {
		data, _ := json.Marshal(v.Questions)
		n += len(data)
	}
	return n
}

// The active/latest run is pinned; completed older details can always be
// recovered from the event recorder. Never evict a live approval or question.
func (s *RunStore) evictBytesLocked() {
	total := 0
	for _, rec := range s.runs {
		total += snapshotTextBytes(rec.snapshot) + rec.metadataBytes
	}
	for i := 0; i < len(s.order) && (total > DefaultBytes || len(s.order) > s.limit); {
		id := s.order[i]
		rec := s.runs[id]
		terminal := rec.snapshot.Status == core.RunDone || rec.snapshot.Status == core.RunFailed || rec.snapshot.Status == core.RunCancelled
		if id == s.last || !terminal {
			i++
			continue
		}
		total -= snapshotTextBytes(rec.snapshot) + rec.metadataBytes
		delete(s.runs, id)
		copy(s.order[i:], s.order[i+1:])
		s.order = s.order[:len(s.order)-1]
	}
}

// CacheRestored preserves the current run identity and applies both cache
// budgets. Oversized historical details are returned to callers but not kept.
func (s *RunStore) CacheRestored(snapshot core.RunSnapshot) {
	if snapshot.Status != core.RunDone && snapshot.Status != core.RunFailed && snapshot.Status != core.RunCancelled {
		return // An interrupted archived run is not a live run in this process.
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.runs[snapshot.ID]; exists {
		return
	}
	size := snapshotBytes(snapshot)
	if size > DefaultBytes {
		return
	}
	s.runs[snapshot.ID] = &runRecord{snapshot: copyRunSnapshot(snapshot), metadataBytes: size - snapshotTextBytes(snapshot)}
	s.order = append(s.order, snapshot.ID)
	s.evictBytesLocked()
}

// Subagents copies only task metadata, never parent tool output or model text.
func (s *RunStore) Subagents(sessionID string) []core.SubAgentView {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []core.SubAgentView
	for _, id := range s.order {
		for _, agent := range s.runs[id].snapshot.SubAgents {
			if agent.SessionID != sessionID {
				continue
			}
			agent.Skills = append([]string(nil), agent.Skills...)
			agent.FinishedAt = copyTime(agent.FinishedAt)
			out = append(out, agent)
		}
	}
	return out
}

func (r *runRecord) refreshSubagentWaits(at time.Time) {
	for i := range r.snapshot.SubAgents {
		agent := &r.snapshot.SubAgents[i]
		if !agent.Active || agent.Status == "queued" {
			continue
		}
		status := "running"
		for _, approval := range r.snapshot.Approvals {
			if approval.SubAgentID == agent.ID && approval.Status == core.ApprovalPending {
				status = "waiting_approval"
				break
			}
		}
		if status == "running" {
			for _, question := range r.snapshot.Questions {
				if question.SubAgentID == agent.ID && question.Status == core.QuestionPending {
					status = "waiting_question"
					break
				}
			}
		}
		if status != agent.Status {
			agent.Status = status
			agent.UpdatedAt = at
		}
	}
}

func (r *runRecord) refreshParentWaits() {
	if r.snapshot.Status == core.RunDone || r.snapshot.Status == core.RunFailed || r.snapshot.Status == core.RunCancelled {
		return
	}
	for _, approval := range r.snapshot.Approvals {
		if approval.Status == core.ApprovalPending {
			r.snapshot.Status = core.RunWaitingApproval
			return
		}
	}
	for _, question := range r.snapshot.Questions {
		if question.Status == core.QuestionPending {
			r.snapshot.Status = core.RunWaitingQuestion
			return
		}
	}
	r.snapshot.Status = core.RunRunning
}
