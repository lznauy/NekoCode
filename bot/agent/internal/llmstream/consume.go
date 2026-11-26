package llmstream

import (
	"context"
	"fmt"
	"time"

	"nekocode/bot/provider/llm"
	"nekocode/bot/provider/types"
	"nekocode/protocol"
)

var errAttemptStopped = fmt.Errorf("model attempt stopped: %w", context.Canceled)

// ConsumeStream projects wire tokens into a single agent attempt.
func ConsumeStream(ctx context.Context, tokenCh <-chan types.StreamToken, errCh <-chan error, s *StreamResult, cb StreamCallbacks, checkDone func() bool) error {
	firstContent := true
	firstReasoning := true
	err := llm.ReadStream(ctx, tokenCh, errCh, func(token types.StreamToken) error {
		if token.Request != nil {
			// Metadata token sent before the HTTP request starts — not
			// content, so it must not arm the TTFT clock.
			s.Request = token.Request
			return nil
		}
		if token.ReasoningSignature != "" {
			s.ReasoningSignature = token.ReasoningSignature
		}
		if s.FirstTokenAt.IsZero() {
			s.FirstTokenAt = time.Now()
		}
		if checkDone != nil && checkDone() {
			return errAttemptStopped
		}
		if token.ReasoningContent != "" && firstReasoning {
			firstReasoning = false
			if cb.OnPhase != nil {
				cb.OnPhase(protocol.PhaseThinking)
			}
		}
		if token.Content != "" {
			if firstContent {
				firstContent = false
				if cb.OnPhase != nil {
					cb.OnPhase(protocol.PhaseReasoning)
				}
			}
			s.TextBuf.WriteString(token.Content)
			if cb.OnText != nil {
				cb.OnText(token.Content)
			}
			if cb.AddTokens != nil {
				cb.AddTokens(0, 1)
			}
		}
		if token.ReasoningContent != "" {
			s.ReasoningBuf.WriteString(token.ReasoningContent)
			if cb.OnReasoning != nil {
				cb.OnReasoning(token.ReasoningContent)
			}
			if cb.AddTokens != nil {
				cb.AddTokens(0, 1)
			}
		}
		if token.Usage != nil {
			s.Usage.Merge(token.Usage)
		}
		if token.ToolCallDelta != nil {
			if firstContent {
				firstContent = false
				if cb.OnPhase != nil {
					cb.OnPhase(protocol.PhaseReasoning)
				}
			}
			if s.TcAccum == nil {
				s.TcAccum = make(map[int]*ToolAccum)
			}
			idx := token.ToolCallDelta.Index
			acc := s.TcAccum[idx]
			if acc == nil {
				acc = &ToolAccum{}
				s.TcAccum[idx] = acc
			}
			if token.ToolCallDelta.ID != "" {
				acc.ID = token.ToolCallDelta.ID
			}
			if token.ToolCallDelta.Name != "" {
				acc.Name = token.ToolCallDelta.Name
			}
			acc.Args.WriteString(token.ToolCallDelta.Arguments)
			if cb.AddTokens != nil {
				cb.AddTokens(0, 1)
			}
		}

		return nil
	})
	if err == nil && checkDone != nil && checkDone() {
		return errAttemptStopped
	}
	return err
}
