package token

import "sync"

// Tracker provides accurate token counting by combining API usage data
// with heuristic estimation for new messages. Also tracks KV cache metrics.
type Tracker struct {
	mu               sync.RWMutex
	lastPromptTokens int // from last API response
	cacheHitTokens   int // cumulative (free — already cached)
	cacheMissTokens  int // cumulative (charged as input)
	newMessageTokens int // estimated tokens in messages added since last API call
	sub              SubStats
}

type State struct {
	LastPromptTokens int
	CacheHitTokens   int
	CacheMissTokens  int
	NewMessageTokens int
	Sub              SubStats
}

func (t *Tracker) Snapshot() State {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return State{
		LastPromptTokens: t.lastPromptTokens,
		CacheHitTokens:   t.cacheHitTokens,
		CacheMissTokens:  t.cacheMissTokens,
		NewMessageTokens: t.newMessageTokens,
		Sub:              t.sub,
	}
}

func (t *Tracker) Restore(s State) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.lastPromptTokens = s.LastPromptTokens
	t.cacheHitTokens = s.CacheHitTokens
	t.cacheMissTokens = s.CacheMissTokens
	t.newMessageTokens = s.NewMessageTokens
	t.sub = s.Sub
}

// RecordPrompt records prompt usage from an API response. Completion usage is
// an agent metric and does not affect the next model context.
func (t *Tracker) RecordPrompt(promptTokens int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if promptTokens > 0 {
		t.lastPromptTokens = promptTokens
	}
	t.newMessageTokens = 0
}

// ResetModel clears provider-calibrated prompt and cache state after a model
// switch. Conversation messages remain untouched and are estimated afresh.
func (t *Tracker) ResetModel() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.lastPromptTokens = 0
	t.newMessageTokens = 0
	t.cacheHitTokens = 0
	t.cacheMissTokens = 0
}

// RecordCache records KV cache hit/miss tokens.
func (t *Tracker) RecordCache(hitTokens, missTokens int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if hitTokens > 0 {
		t.cacheHitTokens += hitTokens
	}
	if missTokens > 0 {
		t.cacheMissTokens += missTokens
	}
}

// CacheStats returns cumulative cache hit and miss tokens.
func (t *Tracker) CacheStats() (hit, miss int) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.cacheHitTokens, t.cacheMissTokens
}

// CacheHitRatio returns the cache hit ratio (0.0–1.0), or 0 if no data.
func (t *Tracker) CacheHitRatio() float64 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	total := t.cacheHitTokens + t.cacheMissTokens
	if total == 0 {
		return 0
	}
	return float64(t.cacheHitTokens) / float64(total)
}

// PromptEstimate returns the estimated prompt token count for the next call.
// Uses API-calibrated prompt_tokens as baseline plus heuristic for new messages.
// Returns 0 if no API response has been received yet.
func (t *Tracker) PromptEstimate() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if t.lastPromptTokens == 0 {
		return 0
	}
	return t.lastPromptTokens + t.newMessageTokens
}

type SubStats struct {
	Count           int
	TotalTokens     int
	CacheHitTokens  int
	CacheMissTokens int
}

func (t *Tracker) RecordSubagent(tokens, hit, miss int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sub.Count++
	t.sub.TotalTokens += tokens
	t.sub.CacheHitTokens += hit
	t.sub.CacheMissTokens += miss
}

func (t *Tracker) SubStats() SubStats {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.sub
}

// AddNew estimates tokens for messages added since the last API call.
func (t *Tracker) AddNew(charCount int) {
	t.AddEstimated((charCount + 3) / 4)
}

// AddEstimated records a token estimate change since the last API response.
// Replacing existing content can supply a negative delta.
func (t *Tracker) AddEstimated(tokens int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.newMessageTokens += tokens
}
