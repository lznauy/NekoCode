package acp

import (
	"context"
	"encoding/json"

	controlruntime "nekocode/runtime"
)

func (s *server) cancel(ctx context.Context, params json.RawMessage) *wireError {
	var request struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(params, &request); err != nil || request.SessionID == "" {
		return rpcError(-32602, "invalid session/cancel params")
	}
	s.mu.Lock()
	turn := s.active[request.SessionID]
	if turn != nil {
		turn.cancelled = true
		turn.cancel()
	}
	runID := controlruntime.RunID("")
	if turn != nil {
		runID = turn.runID
	}
	s.mu.Unlock()
	if runID == "" {
		return nil
	}
	if err := s.backend.CancelRun(ctx, runID); err != nil {
		return backendError("cancel prompt", err)
	}
	return nil
}
