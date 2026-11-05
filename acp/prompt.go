package acp

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	controlruntime "nekocode/runtime"
)

func (s *server) prompt(ctx context.Context, params json.RawMessage) (response any, failure *wireError) {
	var request promptRequest
	if err := json.Unmarshal(params, &request); err != nil || request.SessionID == "" {
		return nil, rpcError(-32602, "invalid session/prompt params")
	}
	s.sessionMu.Lock()
	if !s.verifySession(request.SessionID) {
		s.sessionMu.Unlock()
		return nil, rpcError(-32602, "unknown session %q", request.SessionID)
	}

	turnCtx, cancelTurn := context.WithCancel(ctx)
	turn := &activeTurn{cancel: cancelTurn}
	s.mu.Lock()
	if len(s.active) != 0 {
		s.mu.Unlock()
		s.sessionMu.Unlock()
		cancelTurn()
		return nil, rpcError(-32000, "another prompt is already running")
	}
	s.active[request.SessionID] = turn
	s.mu.Unlock()
	var startedRun controlruntime.RunID
	settled := false
	defer func() {
		cancelTurn()
		if startedRun != "" && !settled {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			if err := s.cancelAndWait(ctx, startedRun); err != nil {
				failure = backendError("settle prompt", errors.Join(err, wireFailure(failure)))
				response = nil
				return
			}
		}
		s.mu.Lock()
		delete(s.active, request.SessionID)
		s.mu.Unlock()
	}()

	if s.backend.CurrentSessionID() != request.SessionID {
		if err := s.backend.ResumeSession(request.SessionID); err != nil {
			s.sessionMu.Unlock()
			return nil, backendError("activate session", err)
		}
	}
	if err := s.applySessionConfig(request.SessionID); err != nil {
		s.sessionMu.Unlock()
		return nil, backendError("apply session config", err)
	}
	// Parse the prompt after the session is active: image blocks are stored
	// in the session's attachment directory, so CurrentSessionID must match.
	input, images, rpcErr := promptContent(request.Prompt, request.SessionID)
	if rpcErr != nil {
		s.sessionMu.Unlock()
		return nil, rpcErr
	}
	if s.activeMCPSession != request.SessionID {
		if err := s.backend.ReplaceMCPServers(turnCtx, mcpSource, cloneMCPServerSpecs(s.sessionMCPConfigs[request.SessionID])); err != nil {
			s.sessionMu.Unlock()
			return nil, backendError("activate session MCP servers", err)
		}
		s.activeMCPSession = request.SessionID
	}
	eventCtx, cancelEvents := context.WithCancel(ctx)
	defer cancelEvents()
	events, err := s.backend.Events(eventCtx, controlruntime.EventFilter{Reliable: true})
	if err != nil {
		s.sessionMu.Unlock()
		return nil, backendError("subscribe to run", err)
	}
	s.mu.Lock()
	cancelled := turn.cancelled
	s.mu.Unlock()
	if cancelled {
		s.sessionMu.Unlock()
		return map[string]any{"stopReason": "cancelled"}, nil
	}

	runID, err := s.backend.StartRun(turnCtx, controlruntime.Input{
		Source: controlruntime.SourceRef{Kind: "acp"},
		Text:   input,
		Images: images,
	})
	if err != nil {
		s.sessionMu.Unlock()
		if errors.Is(err, context.Canceled) {
			return map[string]any{"stopReason": "cancelled"}, nil
		}
		return nil, backendError("start prompt", err)
	}
	startedRun = runID
	s.mu.Lock()
	turn.runID = runID
	cancelled = turn.cancelled
	s.mu.Unlock()
	if cancelled {
		cancelCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		err := s.backend.CancelRun(cancelCtx, runID)
		cancel()
		if err != nil {
			s.sessionMu.Unlock()
			return nil, backendError("cancel prompt", err)
		}
	}
	s.sessionMu.Unlock()

	var updatesForTurn turnUpdates
	for {
		select {
		case <-ctx.Done():
			return nil, backendError("prompt", ctx.Err())
		case event, ok := <-events:
			if !ok {
				return nil, rpcError(-32603, "runtime event stream closed")
			}
			if event.RunID != runID {
				continue
			}
			if err := controlruntime.ValidateEventPayload(event); err != nil {
				return nil, backendError("invalid runtime event", err)
			}
			if updates, ok := updatesForTurn.update(event); ok {
				for _, update := range updates {
					if err := s.notifySession(request.SessionID, update); err != nil {
						return nil, backendError("send session update", err)
					}
				}
			}
			switch event.Type {
			case controlruntime.EventApprovalRequested:
				view, ok := event.Payload.(controlruntime.ApprovalView)
				if ok {
					s.handlePermission(turnCtx, request.SessionID, view)
				}
			case controlruntime.EventQuestionRequested:
				view, ok := event.Payload.(controlruntime.QuestionView)
				if ok {
					_ = s.backend.AnswerQuestion(ctx, view.ID, controlruntime.QuestionReply{Rejected: true})
				}
			case controlruntime.EventRunDone:
				if err := s.backend.WaitRun(ctx, runID); err != nil {
					return nil, backendError("settle prompt", err)
				}
				settled = true
				result, _ := event.Payload.(controlruntime.RunResult)
				return terminalPromptResult("end_turn", result), nil
			case controlruntime.EventRunCancelled:
				if err := s.backend.WaitRun(ctx, runID); err != nil {
					return nil, backendError("settle prompt", err)
				}
				settled = true
				result, _ := event.Payload.(controlruntime.RunResult)
				return terminalPromptResult("cancelled", result), nil
			case controlruntime.EventRunFailed:
				if err := s.backend.WaitRun(ctx, runID); err != nil {
					return nil, backendError("settle prompt", err)
				}
				settled = true
				result, _ := event.Payload.(controlruntime.RunResult)
				if result.Error == "" {
					result.Error = "prompt failed"
				}
				failure := rpcError(-32603, "%s", result.Error)
				if result.EvidenceIncomplete {
					failure.Data = map[string]any{"nekocode/evidence_incomplete": true}
				}
				return nil, failure
			}
		}
	}
}

func terminalPromptResult(reason string, result controlruntime.RunResult) map[string]any {
	response := map[string]any{"stopReason": reason}
	if result.EvidenceIncomplete {
		response["_meta"] = map[string]any{"nekocode/evidence_incomplete": true}
	}
	return response
}

// Cancellation is asynchronous; configuration mutations require lifecycle completion.
func (s *server) cancelAndWait(ctx context.Context, id controlruntime.RunID) error {
	cancelErr := s.backend.CancelRun(ctx, id)
	waitErr := s.backend.WaitRun(ctx, id)
	return errors.Join(cancelErr, waitErr)
}

func wireFailure(err *wireError) error {
	if err == nil {
		return nil
	}
	return errors.New(err.Message)
}
