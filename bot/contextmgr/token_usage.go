package contextmgr

// RecordUsage, RecordCache, and ResetCache hold the read lock for the full
// call so FreshStart (which replaces m.Tracker under write lock) cannot race.
func (m *Manager) RecordUsage(prompt, completion int) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	m.Tracker.RecordUsage(prompt, completion)
}

func (m *Manager) RecordCache(hit, miss int) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	m.Tracker.RecordCache(hit, miss)
}

func (m *Manager) ResetCache() {
	m.mu.RLock()
	defer m.mu.RUnlock()
	m.Tracker.ResetCache()
}

func (m *Manager) TokenUsage() (int, int) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.totalTokenEstimate(), m.ContextWindow
}
