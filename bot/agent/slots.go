package agent

import (
	"fmt"
	"nekocode/bot/extension/tool/runtime/taskbridge"
	"sync"
)

const maxSubSlots = taskbridge.DefaultMaxConcurrentTasks

type slotManager struct {
	mu       sync.Mutex
	slots    []*subSlot
	limit    int
	active   int
	reserved map[string]bool
}

type subSlot struct {
	id      string
	subType string
}

func newSlotManager(limits ...int) *slotManager {
	limit := maxSubSlots
	if len(limits) > 0 && limits[0] > 0 {
		limit = limits[0]
	}
	return &slotManager{reserved: make(map[string]bool), limit: limit}
}

// Reserve holds identity only; queued tasks consume no execution/color slot.
func (m *slotManager) Reserve(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.reserved[id] {
		return false
	}
	for _, slot := range m.slots {
		if slot != nil && slot.id == id {
			return false
		}
	}
	m.reserved[id] = true
	return true
}

// Acquire activates an admitted task's execution/color slot without blocking.
// The worker pool enforces capacity; queued identities consume no slot.
func (m *slotManager) Acquire(id, subType string) (colorIdx int, ok bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, slot := range m.slots {
		if slot != nil && slot.id == id {
			return -1, false
		}
	}

	if m.active >= m.limit {
		return -1, false
	}
	for i := range m.slots {
		if m.slots[i] == nil {
			m.slots[i] = &subSlot{id: id, subType: subType}
			m.active++
			return i, true
		}
	}
	// Grow with actual execution, not the configured ceiling.
	m.slots = append(m.slots, &subSlot{id: id, subType: subType})
	m.active++
	return len(m.slots) - 1, true
}

func (m *slotManager) Release(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.reserved, id)

	for i := range m.slots {
		if m.slots[i] != nil && m.slots[i].id == id {
			m.slots[i] = nil
			m.active--
			return
		}
	}
}

func (m *slotManager) String() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var ids []string
	for _, s := range m.slots {
		if s != nil {
			ids = append(ids, fmt.Sprintf("%s:%s", s.subType, s.id[:min(4, len(s.id))]))
		}
	}
	return fmt.Sprintf("SubSlotManager(%d/%d) %v", m.active, m.limit, ids)
}
