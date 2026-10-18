package llmstream

import (
	"fmt"
	"time"

	"nekocode/bot/provider/types"
	"nekocode/protocol"
)

// ConsumeStream reads provider tokens without interpreting answer text. Retry
// replacement and leaked protocol detection belong to the calling layers.
func ConsumeStream(tokenCh <-chan types.StreamToken, s *StreamResult, cb StreamCallbacks, checkDone func() bool) error {
	const idleTimeout = 3 * time.Minute
	timer := time.NewTimer(idleTimeout)
	defer timer.Stop()

	firstContent := true
	firstReasoning := true
	for {
		select {
		case token, ok := <-tokenCh:
			if !ok {
				return nil
			}
			if token.Request != nil {
				// Metadata token sent before the HTTP request starts — not
				// content, so it must not arm the TTFT clock.
				s.Request = token.Request
				continue
			}
			if token.ReasoningSignature != "" {
				s.ReasoningSignature = token.ReasoningSignature
			}
			if s.FirstTokenAt.IsZero() {
				s.FirstTokenAt = time.Now()
			}
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(idleTimeout)

			if checkDone != nil && checkDone() {
				go func() {
					for range tokenCh {
					}
				}()
				return nil
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

		case <-timer.C:
			go func() {
				for range tokenCh {
				}
			}()
			return fmt.Errorf("stream idle timeout: no tokens for %v", idleTimeout)
		}
	}
}
