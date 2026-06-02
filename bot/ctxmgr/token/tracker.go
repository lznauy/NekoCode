package token

import "sync"

// Tracker provides accurate token counting by combining API usage data
// with heuristic estimation for new messages. Also tracks KV cache metrics.
type Tracker struct {
	mu               sync.RWMutex
	lastPromptTokens int // from last API response
	lastCompTokens   int
	cacheHitTokens   int // cumulative (free — already cached)
	cacheMissTokens  int // cumulative (charged as input)
	newMessageTokens int // estimated tokens in messages added since last API call
	sub              SubStats
}

// RecordUsage records token usage from an API response.
func (t *Tracker) RecordUsage(promptTokens, completionTokens int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if promptTokens > 0 {
		t.lastPromptTokens = promptTokens
	}
	if completionTokens > 0 {
		t.lastCompTokens = completionTokens
	}
	t.newMessageTokens = 0
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

// Total returns the estimated total token count (prompt + completion + new).
func (t *Tracker) Total() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.lastPromptTokens + t.lastCompTokens + t.newMessageTokens
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
	Count            int
	TotalTokens      int
	CacheHitTokens   int
	CacheMissTokens  int
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
	t.mu.Lock()
	defer t.mu.Unlock()
	t.newMessageTokens += charCount / 4
}
