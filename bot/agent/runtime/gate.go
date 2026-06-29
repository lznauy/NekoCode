package runtime

import "nekocode/bot/debug"

const defaultMaxRetries = 2

// responseGate prevents governance internal signals from leaking into
// the model's visible assistant output. It tracks retries for final-answer
// blocks and enforces a hard limit.
type responseGate struct {
	MaxRetries int
	retries    int
}

func newResponseGate() *responseGate {
	return &responseGate{MaxRetries: defaultMaxRetries}
}

// TryRetry returns (shouldRetry, hintContent). When retries are exhausted
// it returns (false, "") — the caller should let the answer through
// without appending [Governance note] to assistant content.
func (g *responseGate) TryRetry(reason string) (retry bool, hint string) {
	g.retries++
	if g.retries > g.MaxRetries {
		debug.Log("[GOVERNANCE] response gate: retries exhausted (%d/%d), allowing through: %s",
			g.retries-1, g.MaxRetries, reason)
		return false, ""
	}
	return true, reason
}

func (g *responseGate) Reset() { g.retries = 0 }
