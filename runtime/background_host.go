package runtime

import (
	"context"
	"nekocode/protocol"
)

type backgroundHost struct {
	interactionID RunID
	runtime       *Runtime
	ctx           context.Context
	id, sessionID string
}

// owns rejects callbacks from an older execution even when the task ID is reused.
func (h backgroundHost) owns(e *backgroundEntry) bool {
	return e != nil && !e.settling && e.SessionID == h.sessionID && e.interactionID == h.interactionID
}

func (h backgroundHost) Step(ev protocol.StepEvent) {
	terminal := ev.Action == protocol.StepActionSubAgentEnd
	if !terminal && h.ctx.Err() != nil {
		return
	}
	ev.SubAgentID = h.id
	b := h.runtime.background
	b.mu.Lock()
	if !terminal && h.ctx.Err() != nil {
		b.mu.Unlock()
		return
	}
	if e := b.entries[h.id]; h.owns(e) {
		if terminal && e.cancel == nil {
			b.mu.Unlock()
			return
		}
		// Execution identity is admitted and persisted by Runtime, never replaced
		// by a worker callback. Sequence numbers reflect the child's durable history.
		ev.SubAgentAttemptID = e.AttemptID
		if ev.SubAgentAttemptSeq > 0 {
			e.AttemptSeq = ev.SubAgentAttemptSeq
		}
		ev.SubAgentAttemptSeq = e.AttemptSeq

		if terminal {
			// Preserve the worker's outcome even after cancellation, but leave
			// the task active until execute settles and publishes its sole End.
			e.workerStatus = ev.SubAgentStatus
			if ev.Summary != nil {
				e.workerSummary = *ev.Summary
			}
			b.mu.Unlock()
			return
		}
	} else {
		b.mu.Unlock()
		return
	}
	b.mu.Unlock()
	h.runtime.publishStepForSession(RunID("task_"+h.id), ev, h.sessionID)
}
func (h backgroundHost) updateInteractionStatus(approvalDelta, questionDelta int) {
	b := h.runtime.background
	b.mu.Lock()
	e := b.entries[h.id]
	if !h.owns(e) || h.ctx.Err() != nil || e.cancel == nil {
		b.mu.Unlock()
		return
	}
	e.waitingApprovals += approvalDelta
	e.waitingQuestions += questionDelta
	status := "running"
	if e.waitingApprovals > 0 {
		status = "waiting_approval"
	} else if e.waitingQuestions > 0 {
		status = "waiting_question"
	}
	if status == e.Status {
		b.mu.Unlock()
		return
	}
	e.Status = status
	b.signal()
	if e.publishingWaitStatus {
		b.mu.Unlock()
		return
	}
	e.publishingWaitStatus = true
	// Concurrent tools can resolve while an observer is receiving the previous
	// update. Publish the latest status in order without holding the task lock.
	for {
		b.mu.Unlock()
		h.Step(protocol.StepEvent{Action: protocol.StepActionSubAgentUpdate, SubAgentStatus: status})
		b.mu.Lock()
		if b.entries[h.id] != e || !h.owns(e) || h.ctx.Err() != nil || e.Status == status {
			e.publishingWaitStatus = false
			b.mu.Unlock()
			return
		}
		status = e.Status
	}
}

func (h backgroundHost) Confirm(req protocol.ConfirmRequest) protocol.ConfirmReply {
	if h.ctx.Err() != nil {
		return protocol.ConfirmReply{Cancelled: true}
	}
	req.SubAgentID = h.id
	wait := h.runtime.approvals.RegisterScoped(h.interactionID, RunID("task_"+h.id), req)
	if wait == nil {
		return protocol.ConfirmReply{Cancelled: true}
	}
	stop := context.AfterFunc(h.ctx, func() { h.runtime.approvals.RejectRun(h.interactionID) })
	defer stop()
	if h.ctx.Err() != nil {
		h.runtime.approvals.RejectRun(h.interactionID)
	}
	h.updateInteractionStatus(1, 0)
	reply := wait()
	h.updateInteractionStatus(-1, 0)
	if h.ctx.Err() != nil {
		return protocol.ConfirmReply{Cancelled: true}
	}
	return reply
}
func (h backgroundHost) Ask(req protocol.QuestionRequest) protocol.QuestionReply {
	if h.ctx.Err() != nil {
		return protocol.QuestionReply{Rejected: true}
	}
	req.SubAgentID = h.id
	wait := h.runtime.questions.RegisterScoped(h.interactionID, RunID("task_"+h.id), req)
	if wait == nil {
		return protocol.QuestionReply{Rejected: true}
	}
	stop := context.AfterFunc(h.ctx, func() { h.runtime.questions.RejectRun(h.interactionID) })
	defer stop()
	if h.ctx.Err() != nil {
		h.runtime.questions.RejectRun(h.interactionID)
	}
	h.updateInteractionStatus(0, 1)
	reply := wait()
	h.updateInteractionStatus(0, -1)
	if h.ctx.Err() != nil {
		return protocol.QuestionReply{Rejected: true}
	}
	return reply
}
