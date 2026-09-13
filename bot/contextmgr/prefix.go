package contextmgr

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"nekocode/bot/calllog"
	"nekocode/bot/provider/types"
)

type prefixShape struct {
	system  [32]byte
	tools   [32]byte
	history [][32]byte
}

type PrefixMiss struct {
	Tokens int
	Parts  []string
}

// PrefixCallStats describes one model request's cache outcome. The JSON tags
// pin the shape used by session.json: renaming a Go field must not silently
// invalidate previously saved sessions.
type PrefixCallStats struct {
	Request    int      `json:"request"`
	HitTokens  int      `json:"hit_tokens"`
	MissTokens int      `json:"miss_tokens"`
	Parts      []string `json:"parts,omitempty"`
}

// PrefixTurnStats summarizes cache behavior across one user conversation.
// A conversation may contain multiple model requests while tools are running.
type PrefixTurnStats struct {
	Requests   int             `json:"requests"`
	HitTokens  int             `json:"hit_tokens"`
	MissTokens int             `json:"miss_tokens"`
	PeakMiss   PrefixCallStats `json:"peak_miss"`
	LowestHit  PrefixCallStats `json:"lowest_hit"`
}

type prefixTracker struct {
	previous *prefixShape
	pending  []string
	calls    []PrefixCallStats
	turn     PrefixTurnStats
}

// PrefixBaseline carries the last observed request's cache-relevant head
// across a process boundary. Layer 2-3 (archive and history) are append-only
// within an epoch, so only the stable head needs a baseline: a restored
// tracker can then say whether the new process changed that head, or whether
// the miss came from the provider's tail. Empty when no request was observed.
type PrefixBaseline struct {
	System string // full hex digest of the Layer 0-1 messages
	Tools  string // full hex digest of the tool definitions
}

// Baseline reports the head digests of the last observed request.
func (t *prefixTracker) Baseline() PrefixBaseline {
	if t.previous == nil {
		return PrefixBaseline{}
	}
	return PrefixBaseline{
		System: hex.EncodeToString(t.previous.system[:]),
		Tools:  hex.EncodeToString(t.previous.tools[:]),
	}
}

// RestoreBaseline seeds previous from a persisted baseline. History stays
// empty on purpose: it is append-only within an epoch, so the append-only
// check passes and the first miss after a resume is attributed to
// system/tools (we changed the head) rather than to cold-start.
func (t *prefixTracker) RestoreBaseline(baseline PrefixBaseline) {
	system, sysOK := decodePrefixDigest(baseline.System)
	tools, toolsOK := decodePrefixDigest(baseline.Tools)
	if !sysOK || !toolsOK {
		return
	}
	t.previous = &prefixShape{system: system, tools: tools}
}

func decodePrefixDigest(value string) ([32]byte, bool) {
	var digest [32]byte
	if value == "" {
		return digest, false
	}
	raw, err := hex.DecodeString(value)
	if err != nil || len(raw) != len(digest) {
		return digest, false
	}
	copy(digest[:], raw)
	return digest, true
}

func buildPrefixShape(system, history []types.Message, tools []types.ToolDef) prefixShape {
	return prefixShape{
		system:  digestJSON(canonicalMessages(system)),
		tools:   digestJSON(nonNilTools(tools)),
		history: digestMessages(history),
	}
}

func (t *prefixTracker) Observe(current prefixShape) {
	t.turn.Requests++
	if t.previous == nil {
		t.pending = []string{"cold-start"}
	} else {
		t.pending = changedPrefixParts(*t.previous, current)
	}
	copyShape := current
	copyShape.history = append([][32]byte(nil), current.history...)
	t.previous = &copyShape
	parts := append([]string(nil), t.pending...)
	if len(parts) == 0 {
		parts = []string{"tail/provider"}
	}
	t.calls = append(t.calls, PrefixCallStats{Request: t.turn.Requests, Parts: parts})
}

func (t *prefixTracker) RecordCache(hit, miss int) PrefixMiss {
	if hit > 0 {
		t.turn.HitTokens += hit
	}
	if miss <= 0 {
		t.recordCall(hit, 0)
		return PrefixMiss{}
	}
	t.turn.MissTokens += miss
	parts := append([]string(nil), t.pending...)
	if len(parts) == 0 {
		parts = []string{"tail/provider"}
	}
	event := PrefixMiss{Tokens: miss, Parts: parts}
	t.recordCall(hit, miss)
	return event
}

func (t *prefixTracker) recordCall(hit, miss int) {
	if len(t.calls) == 0 {
		return
	}
	call := &t.calls[len(t.calls)-1]
	call.HitTokens += hit
	call.MissTokens += miss
	t.turn.PeakMiss = PrefixCallStats{}
	t.turn.LowestHit = PrefixCallStats{}
	for _, candidate := range t.calls {
		if candidate.MissTokens > t.turn.PeakMiss.MissTokens {
			t.turn.PeakMiss = clonePrefixCall(candidate)
		}
		if candidate.HitTokens+candidate.MissTokens == 0 {
			continue
		}
		if t.turn.LowestHit.Request == 0 || lowerHitRatio(candidate, t.turn.LowestHit) {
			t.turn.LowestHit = clonePrefixCall(candidate)
		}
	}
}

func lowerHitRatio(a, b PrefixCallStats) bool {
	aTotal := a.HitTokens + a.MissTokens
	bTotal := b.HitTokens + b.MissTokens
	return int64(a.HitTokens)*int64(bTotal) < int64(b.HitTokens)*int64(aTotal)
}

func clonePrefixCall(call PrefixCallStats) PrefixCallStats {
	call.Parts = append([]string(nil), call.Parts...)
	return call
}

func cloneTurnStats(stats PrefixTurnStats) PrefixTurnStats {
	stats.PeakMiss = clonePrefixCall(stats.PeakMiss)
	stats.LowestHit = clonePrefixCall(stats.LowestHit)
	return stats
}

// IsZero reports whether the turn has anything /context can display. Hit and
// miss tokens always accompany an accounted request (prefixTracker.Observe runs
// before RecordCache), so these three fields fully decide it.
func (s PrefixTurnStats) IsZero() bool {
	return s.Requests == 0 && s.PeakMiss.MissTokens == 0 && s.LowestHit.Request == 0
}

func (t *prefixTracker) TurnStats() PrefixTurnStats {
	return cloneTurnStats(t.turn)
}

// Diagnostics returns the pending change classification plus the fingerprint
// of the most recently observed request shape, for the per-call evidence
// log. After Observe, previous holds the current request — so these hashes
// identify this call's prefix, comparable across records. The same
// tail/provider fallback as RecordCache applies: an empty parts list means
// "append-only, provider-side miss", which must be visible in the log rather
// than omitted as if diagnostics were missing.
func (t *prefixTracker) Diagnostics() calllog.PrefixDiag {
	parts := append([]string(nil), t.pending...)
	if len(parts) == 0 && t.previous != nil {
		parts = []string{"tail/provider"}
	}
	diag := calllog.PrefixDiag{ChangedParts: parts}
	if t.previous == nil {
		return diag
	}
	diag.SystemHash = calllog.ShortDigest(t.previous.system)
	diag.ToolsHash = calllog.ShortDigest(t.previous.tools)
	diag.HistoryCount = len(t.previous.history)
	var joined []byte
	for _, h := range t.previous.history {
		joined = append(joined, h[:]...)
	}
	diag.HistoryHash = calllog.ShortDigest(sha256.Sum256(joined))
	return diag
}

// BeginTurn starts cache accounting for a new user conversation while keeping
// the previous request shape as the comparison baseline.
func (t *prefixTracker) BeginTurn() {
	t.pending = nil
	t.calls = nil
	t.turn = PrefixTurnStats{}
}

func (t *prefixTracker) Reset() {
	*t = prefixTracker{}
}

func changedPrefixParts(previous, current prefixShape) []string {
	var parts []string
	if previous.system != current.system {
		parts = append(parts, "system")
	}
	if previous.tools != current.tools {
		parts = append(parts, "tools")
	}
	if !historyIsAppendOnly(previous.history, current.history) {
		parts = append(parts, "history")
	}
	return parts
}

func historyIsAppendOnly(previous, current [][32]byte) bool {
	if len(current) < len(previous) {
		return false
	}
	for i := range previous {
		if previous[i] != current[i] {
			return false
		}
	}
	return true
}

func digestMessages(messages []types.Message) [][32]byte {
	digests := make([][32]byte, len(messages))
	for i, message := range canonicalMessages(messages) {
		digests[i] = digestJSON(message)
	}
	return digests
}

func canonicalMessages(messages []types.Message) []types.Message {
	out := append([]types.Message(nil), messages...)
	for i := range out {
		out[i].Source = ""
	}
	if out == nil {
		out = []types.Message{}
	}
	return out
}

func nonNilTools(tools []types.ToolDef) []types.ToolDef {
	if tools == nil {
		return []types.ToolDef{}
	}
	return tools
}

func digestJSON(value any) [32]byte {
	data, _ := json.Marshal(value)
	return sha256.Sum256(data)
}
