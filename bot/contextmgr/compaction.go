package contextmgr

import (
	"context"

	"nekocode/bot/contextmgr/compact"
)

func (m *Manager) AutoCompactIfNeeded() (compact.Level, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.CM != nil {
		return m.CM.AutoCompactIfNeeded()
	}
	return compact.LevelNormal, nil
}

func (m *Manager) NeedsSummarization() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.CM != nil {
		return m.CM.NeedsSummarization()
	}
	return false
}

func (m *Manager) CompactStats() (compactCount, trimCount int) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.CompactCount, m.TrimCount
}

func (m *Manager) Summarize() error {
	if m.CM == nil {
		return nil
	}
	m.mu.Lock()
	prevArchive := m.ctx.Archive
	if err := m.CM.FullCompact(); err != nil {
		m.mu.Unlock()
		return err
	}
	newArchive := m.ctx.Archive
	m.mu.Unlock()

	if prevArchive != "" && newArchive != "" && m.MergeClient != nil {
		mergeCtx := m.CM.CancelCtx
		if mergeCtx == nil {
			mergeCtx = context.Background()
		}
		merged := compact.MergeSummaries(mergeCtx, m.MergeClient, prevArchive, newArchive)

		m.mu.Lock()
		m.ctx.Archive = merged
		m.mu.Unlock()
	}
	return nil
}
