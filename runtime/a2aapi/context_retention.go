package a2aapi

import (
	"fmt"

	"github.com/a2aproject/a2a-go/v2/a2a"
)

func (e *executor) reserveContext(contextID, protectedSessionID string, inputBytes int) (*contextReservation, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.contextClock++
	reservation := &contextReservation{contextID: contextID, inputBytes: inputBytes}
	if existing := e.contexts[contextID]; existing != nil {
		if existing.turns < maxContextTurns && existing.inputBytes+inputBytes <= maxContextInputBytes {
			reservation.sessionID = existing.sessionID
			reservation.previousLastUsed = existing.lastUsed
			reservation.reused = true
			existing.active++
			existing.turns++
			existing.inputBytes += inputBytes
			existing.lastUsed = e.contextClock
			return reservation, nil
		}
		if existing.active != 0 || existing.sessionID == protectedSessionID {
			return nil, fmt.Errorf("A2A context retention limit reached")
		}
		if len(e.pendingSessionDeletes) >= maxContexts {
			return nil, fmt.Errorf("A2A session cleanup backlog limit reached")
		}
		reservation.evictedID, reservation.evicted = contextID, existing
		delete(e.contexts, contextID)
	} else if len(e.pendingSessionDeletes) >= maxContexts {
		return nil, fmt.Errorf("A2A session cleanup backlog limit reached")
	}
	if len(e.contexts) >= maxContexts {
		for id, candidate := range e.contexts {
			if candidate.active != 0 || candidate.sessionID == protectedSessionID || (reservation.evicted != nil && candidate.lastUsed >= reservation.evicted.lastUsed) {
				continue
			}
			reservation.evictedID, reservation.evicted = id, candidate
		}
		if reservation.evicted == nil {
			return nil, fmt.Errorf("A2A context limit reached")
		}
		delete(e.contexts, reservation.evictedID)
	}
	e.contexts[contextID] = &contextBinding{lastUsed: e.contextClock, active: 1, turns: 1, inputBytes: inputBytes}
	return reservation, nil
}

func (e *executor) rememberSession(contextID, sessionID string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if binding := e.contexts[contextID]; binding != nil && binding.sessionID == "" {
		binding.sessionID = sessionID
	}
}

func (e *executor) rollbackContextReservation(reservation *contextReservation) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if reservation.evicted != nil {
		delete(e.contexts, reservation.contextID)
		e.contexts[reservation.evictedID] = reservation.evicted
		return
	}
	context := e.contexts[reservation.contextID]
	if reservation.reused && context != nil {
		if context.active > 0 {
			context.active--
		}
		if context.turns > 0 {
			context.turns--
		}
		context.inputBytes = max(0, context.inputBytes-reservation.inputBytes)
		context.lastUsed = reservation.previousLastUsed
		return
	}
	delete(e.contexts, reservation.contextID)
}

func (e *executor) deleteBinding(taskID a2a.TaskID, binding *taskBinding) {
	binding.mu.Lock()
	contextID, contextHeld := binding.contextID, binding.contextHeld
	evictedSessionID := binding.evictedSessionID
	binding.contextHeld = false
	binding.evictedSessionID = ""
	binding.mu.Unlock()
	e.mu.Lock()
	if e.tasks[taskID] == binding {
		delete(e.tasks, taskID)
	}
	if contextHeld {
		if contextBinding := e.contexts[contextID]; contextBinding != nil && contextBinding.active > 0 {
			contextBinding.active--
			if contextBinding.active == 0 && contextBinding.sessionID == "" {
				delete(e.contexts, contextID)
			}
		}
	}
	e.mu.Unlock()
	if evictedSessionID != "" {
		e.deleteOrQueueSession(evictedSessionID)
	}
}

func (e *executor) deleteOrQueueSession(sessionID string) {
	err := e.rt.DeleteInactiveSession(sessionID)
	e.mu.Lock()
	defer e.mu.Unlock()
	if err == nil {
		delete(e.pendingSessionDeletes, sessionID)
		return
	}
	if len(e.pendingSessionDeletes) < maxContexts {
		e.pendingSessionDeletes[sessionID] = struct{}{}
	}
}

func (e *executor) retrySessionDeletes() {
	e.mu.Lock()
	sessionIDs := make([]string, 0, len(e.pendingSessionDeletes))
	for sessionID := range e.pendingSessionDeletes {
		sessionIDs = append(sessionIDs, sessionID)
	}
	e.mu.Unlock()
	for _, sessionID := range sessionIDs {
		e.deleteOrQueueSession(sessionID)
	}
}
