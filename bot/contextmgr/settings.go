package contextmgr

import "nekocode/common"

func (m *Manager) SetSystemPrompt(s string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ctx.SystemPrompt = s
}

func (m *Manager) SetSkillList(s string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ctx.Skills = s
}

func (m *Manager) SetHints(s string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ctx.Hints = s
}

func (m *Manager) SetContextWindow(budget int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if budget > 0 {
		m.ContextWindow = budget
	}
}

func (m *Manager) SetTodos(items []common.TodoItem) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ctx.LoadTodos(items)
}

func (m *Manager) AllTasksDone() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.ctx.AllTasksDone()
}

func (m *Manager) HasTasks() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.ctx.HasTasks()
}
