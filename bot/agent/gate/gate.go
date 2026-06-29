package gate

import "nekocode/bot/debug"

const defaultMaxRetries = 2

// ResponseGate prevents governance internal signals from leaking into
// the model's visible assistant output. It tracks retries for final-answer
// blocks and enforces a hard limit.
type ResponseGate struct {
	MaxRetries int
	retries    int
}

// NewResponseGate creates a gate with the default retry limit.
func NewResponseGate() *ResponseGate {
	return &ResponseGate{MaxRetries: defaultMaxRetries}
}

// TryRetry returns (shouldRetry, hintContent). When retries are exhausted
// it returns (false, "") — the caller should let the answer through
// without appending [Governance note] to assistant content.
func (g *ResponseGate) TryRetry(reason string) (retry bool, hint string) {
	g.retries++
	if g.retries > g.MaxRetries {
		debug.Log("[GOVERNANCE] response gate: retries exhausted (%d/%d), allowing through: %s",
			g.retries-1, g.MaxRetries, reason)
		return false, ""
	}
	return true, reason
}

func (g *ResponseGate) Reset() { g.retries = 0 }
