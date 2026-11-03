package tui

import (
	"context"
	controlruntime "nekocode/runtime"
)

// Runtime approvals and questions share one FIFO so concurrent tasks never
// replace a prompt the user is reading. The active prompt stays at index zero.
type runtimePrompt struct {
	approval *controlruntime.ApprovalView
	question *controlruntime.QuestionView
}

func (p runtimePrompt) identity() (string, bool) {
	if p.approval != nil {
		return p.approval.ID, true
	}
	return p.question.ID, false
}

func (m *Model) enqueueRuntimePrompt(p runtimePrompt) {
	id, approval := p.identity()
	for _, queued := range m.runtimePrompts {
		queuedID, queuedApproval := queued.identity()
		if id == queuedID && approval == queuedApproval {
			return
		}
	}
	m.runtimePrompts = append(m.runtimePrompts, p)
	m.showRuntimePrompt()
}

func (m *Model) showRuntimePrompt() {
	if m.runtimePromptActive || len(m.runtimePrompts) == 0 || m.state == stateConfirming || m.state == stateQuestioning {
		return
	}
	p := m.runtimePrompts[0]
	m.preConfirmState = m.state
	m.runtimePromptActive = true
	if p.approval != nil {
		view := p.approval
		req := view.ToConfirmRequest()
		m.ConfirmBar.SetRequest(&req, func(ok, remember bool) {
			_ = m.Runtime.DecideApproval(context.Background(), view.ID, controlruntime.ApprovalDecision{Allowed: ok, Remember: ok && remember})
		})
		m.state = stateConfirming
	} else {
		view := p.question
		req := view.ToQuestionRequest()
		m.QuestionBar.SetRequest(&req, func(reply controlruntime.QuestionReply) {
			_ = m.Runtime.AnswerQuestion(context.Background(), view.ID, reply)
		})
		m.state = stateQuestioning
	}
	m.resizeMessages()
}

func (m *Model) resolveRuntimePrompt(id string, approval bool) {
	for i, p := range m.runtimePrompts {
		queuedID, queuedApproval := p.identity()
		if id != queuedID || approval != queuedApproval {
			continue
		}
		active := i == 0 && m.runtimePromptActive
		copy(m.runtimePrompts[i:], m.runtimePrompts[i+1:])
		m.runtimePrompts[len(m.runtimePrompts)-1] = runtimePrompt{}
		m.runtimePrompts = m.runtimePrompts[:len(m.runtimePrompts)-1]
		if active {
			m.ConfirmBar.Clear()
			m.QuestionBar.Clear()
			m.runtimePromptActive = false
			m.state = m.preConfirmState
			m.showRuntimePrompt()
			m.resizeMessages()
		}
		return
	}
}

func (m *Model) finishPromptInput() {
	if m.runtimePromptActive && len(m.runtimePrompts) > 0 {
		id, approval := m.runtimePrompts[0].identity()
		m.resolveRuntimePrompt(id, approval)
		return
	}
	// A local confirmation (such as session deletion) may have runtime prompts
	// waiting behind it too.
	m.state = m.preConfirmState
	m.showRuntimePrompt()
	m.resizeMessages()
}
