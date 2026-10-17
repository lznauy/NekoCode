package acp

import (
	"context"
	"encoding/json"
	"errors"

	controlruntime "nekocode/runtime"
)

func (s *server) prompt(ctx context.Context, params json.RawMessage) (any, *wireError) {
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
	defer func() {
		cancelTurn()
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
	events, err := s.backend.Events(eventCtx, controlruntime.EventFilter{})
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
	s.mu.Lock()
	turn.runID = runID
	cancelled = turn.cancelled
	s.mu.Unlock()
	if cancelled {
		_ = s.backend.CancelRun(context.Background(), runID)
	}
	s.sessionMu.Unlock()

	for {
		select {
		case <-ctx.Done():
			_ = s.backend.CancelRun(context.Background(), runID)
			return nil, backendError("prompt", ctx.Err())
		case event, ok := <-events:
			if !ok {
				return nil, rpcError(-32603, "runtime event stream closed")
			}
			if event.RunID != runID {
				continue
			}
			if updates, ok := eventUpdate(event); ok {
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
				return map[string]any{"stopReason": "end_turn"}, nil
			case controlruntime.EventRunCancelled:
				if err := s.backend.WaitRun(ctx, runID); err != nil {
					return nil, backendError("settle prompt", err)
				}
				return map[string]any{"stopReason": "cancelled"}, nil
			case controlruntime.EventRunFailed:
				if err := s.backend.WaitRun(ctx, runID); err != nil {
					return nil, backendError("settle prompt", err)
				}
				result, _ := event.Payload.(controlruntime.RunResult)
				if result.Error == "" {
					result.Error = "prompt failed"
				}
				return nil, rpcError(-32603, "%s", result.Error)
			}
		}
	}
}
