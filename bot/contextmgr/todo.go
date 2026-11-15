package contextmgr

import (
	"fmt"
	"sort"
	"strings"

	"nekocode/protocol"
)

// ApplyTodos serializes plan changes through persistence. Observers are notified
// by the caller only after this transaction has succeeded.
func (m *Manager) ApplyTodos(update protocol.TodoUpdate, persist func() error) (protocol.TodoPlan, error) {
	m.todoWriteMu.Lock()
	defer m.todoWriteMu.Unlock()
	m.state.mu.Lock()
	current := m.state.ctx.TodoGroup
	group := update.PlanID
	if group == "" {
		group = current
	}
	if group == "" {
		group = "current"
	}
	oldItems := append([]protocol.TodoItem(nil), m.state.ctx.TodoItems...)
	oldPlans := m.state.ctx.TodoPlans.Clone()
	previous, exists := oldPlans[group]
	if group == current || current == "" && update.PlanID == "" {
		previous = oldItems
	}
	if update.PlanID != "" && !exists && group != current {
		m.state.mu.Unlock()
		if current != "" {
			return protocol.TodoPlan{}, fmt.Errorf("unknown plan_id %q in this session; no changes were applied. Use the exact plan_id returned by task_submit/todo_write, or omit plan_id to update the active plan %q. Never invent plan IDs", group, current)
		}
		return protocol.TodoPlan{}, fmt.Errorf("unknown plan_id %q in this session; no changes were applied. Omit plan_id to start a plan for the current demand, or use an exact saved plan_id returned by todo_write. Never invent plan IDs", group)
	}
	previous = protocol.EnsureTodoIDs(previous)
	next := append([]protocol.TodoItem(nil), previous...)
	var err error
	if !update.SelectOnly {
		if update.Patch {
			next, err = protocol.PatchTodos(previous, update.Items)
		} else {
			next, err = protocol.ReconcileTodos(previous, update.Items)
		}
	}
	if err != nil {
		m.state.mu.Unlock()
		return protocol.TodoPlan{}, err
	}
	if m.state.ctx.TodoPlans == nil {
		m.state.ctx.TodoPlans = TodoPlans{}
	}
	m.state.ctx.TodoPlans[group] = append([]protocol.TodoItem(nil), next...)
	m.state.ctx.TodoGroup = group
	m.state.ctx.TodoItems = next
	m.state.mu.Unlock()
	if persist != nil {
		if err = persist(); err != nil {
			m.state.mu.Lock()
			m.state.ctx.TodoGroup = current
			m.state.ctx.TodoItems = oldItems
			m.state.ctx.TodoPlans = oldPlans
			m.state.mu.Unlock()
			return protocol.TodoPlan{}, fmt.Errorf("save todo plan: %w", err)
		}
	}
	return protocol.TodoPlan{ID: group, Items: append([]protocol.TodoItem(nil), next...)}, nil
}

// Saved-plan references let a new turn resume a prior demand explicitly;
// they never authorize automatically continuing an unrelated plan.
func (c *contextContent) todoContext() string {
	var b strings.Builder
	if c.TodoGroup != "" && len(c.TodoItems) > 0 {
		fmt.Fprintf(&b, "Active plan_id: %s\n", c.TodoGroup)
	}
	b.WriteString(c.TodoText())
	count := 0
	groups := make([]string, 0, len(c.TodoPlans))
	for group := range c.TodoPlans {
		groups = append(groups, group)
	}
	sort.Strings(groups)
	for _, group := range groups {
		items := c.TodoPlans[group]
		if group == c.TodoGroup {
			continue
		}
		pending := 0
		for _, item := range items {
			if item.Status != "completed" && item.Status != "cancelled" {
				pending++
			}
		}
		if pending == 0 {
			continue
		}
		if count == 0 {
			b.WriteString("\nSaved unfinished plans (activate only to continue the relevant demand using todo_write with plan_id and no todos):")
		}
		fmt.Fprintf(&b, "\n- plan_id=%s: %d unfinished", group, pending)
		for _, item := range items {
			if item.Status != "completed" && item.Status != "cancelled" {
				fmt.Fprintf(&b, "; %s", protocol.SubagentTextPreview(item.Content, 128))
				break
			}
		}
		count++
		if count == 4 {
			break
		}
	}
	return b.String()
}
