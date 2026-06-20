package compact

import (
	"nekocode/bot/debug"
	"nekocode/llm/types"
)

// Layer 2: History Sniping.
// Wholesale removal of the oldest messages from the FRONT of the queue.
// Only targets "cold history" — messages before compactBoundary that are
// already excluded from Build() output. Zero impact on active KV cache prefix.

const snipeBoundaryMarker = "[History boundary — earlier messages snipped]"

// SnipHistory removes the oldest messages before compactBoundary when
// they accumulate beyond a threshold. Returns the number snipped.
func (m *Compactor) SnipHistory() int {
	boundary := m.Ctx.CompactBoundary
	if boundary <= 60 {
		return 0
	}

	keep := 40
	snip := boundary - keep
	if snip <= 0 {
		return 0
	}

	m.Ctx.Messages = m.Ctx.Messages[snip:]
	m.Ctx.CompactBoundary -= snip

	debug.Log("snipe: removed %d cold-history messages (boundary now %d, total %d)", snip, m.Ctx.CompactBoundary, len(m.Ctx.Messages))

	// Insert boundary marker at the cut point.
	if m.Ctx.CompactBoundary > 0 && len(m.Ctx.Messages) > 0 {
		m.Ctx.Messages = append(
			[]types.Message{{Role: "system", Content: snipeBoundaryMarker}},
			m.Ctx.Messages...,
		)
		m.Ctx.CompactBoundary++
	}

	*m.TrimCount += snip
	return snip
}
