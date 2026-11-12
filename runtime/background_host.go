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
	return e != nil && e.SessionID == h.sessionID && e.interactionID == h.interactionID
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
		if ev.SubAgentAttemptID != "" {
			e.AttemptID, e.AttemptSeq = ev.SubAgentAttemptID, ev.SubAgentAttemptSeq
		}
		if ev.SubAgentAttemptID == "" {
			ev.SubAgentAttemptID, ev.SubAgentAttemptSeq = e.AttemptID, e.AttemptSeq
		}
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
func (h backgroundHost) status(status string) {
	h.runtime.background.mu.Lock()
	if e := h.runtime.background.entries[h.id]; h.owns(e) && h.ctx.Err() == nil && e.cancel != nil {
		e.Status = status
	}
	h.runtime.background.mu.Unlock()
	h.Step(protocol.StepEvent{Action: protocol.StepActionSubAgentUpdate, SubAgentStatus: status})
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
	h.status("waiting_approval")
	reply := wait()
	h.status("running")
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
	h.status("waiting_question")
	reply := wait()
	h.status("running")
	if h.ctx.Err() != nil {
		return protocol.QuestionReply{Rejected: true}
	}
	return reply
}
