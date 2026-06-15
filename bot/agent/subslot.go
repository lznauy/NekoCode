// subslot.go — sub-agent slot manager: max 8 concurrent sub-agents, each with exclusive color.
package agent

import (
	"fmt"
	"sync"

	"nekocode/common"
)

const maxSubSlots = 8

// SubSlotManager controls sub-agent concurrency and color assignment.
// Max 8 concurrent sub-agents. Each gets an exclusive color index 0-7.
// Acquire blocks when all 8 slots are occupied.
type SubSlotManager struct {
	mu     sync.Mutex
	cond   *sync.Cond
	slots  [maxSubSlots]*common.SubSlot
	active int
}

// NewSubSlotManager creates a new slot manager.
func NewSubSlotManager() *SubSlotManager {
	m := &SubSlotManager{}
	m.cond = sync.NewCond(&m.mu)
	return m
}

// Acquire blocks until a free slot is available, then assigns it.
// Returns the color index and sub-agent info. ok is always true.
func (m *SubSlotManager) Acquire(id, subType string) (colorIdx int, ok bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for m.active >= maxSubSlots {
		m.cond.Wait()
	}

	// Find first free slot.
	for i := range m.slots {
		if m.slots[i] == nil {
			m.slots[i] = &common.SubSlot{ID: id, SubType: subType, ColorIdx: i}
			m.active++
			return i, true
		}
	}
	return -1, false
}

// Release frees the slot occupied by the given sub-agent ID.
// Idempotent: releasing an unknown ID is a no-op.
func (m *SubSlotManager) Release(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i := range m.slots {
		if m.slots[i] != nil && m.slots[i].ID == id {
			m.slots[i] = nil
			m.active--
			m.cond.Signal()
			return
		}
	}
}

// Active returns the current active sub-agent slots (for TUI rendering).
func (m *SubSlotManager) Active() []common.SubSlot {
	m.mu.Lock()
	defer m.mu.Unlock()

	var out []common.SubSlot
	for _, s := range m.slots {
		if s != nil {
			out = append(out, *s)
		}
	}
	return out
}

// Count returns the number of active sub-agents.
func (m *SubSlotManager) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.active
}

// String returns a debug representation.
func (m *SubSlotManager) String() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var ids []string
	for _, s := range m.slots {
		if s != nil {
			ids = append(ids, fmt.Sprintf("%s:%s", s.SubType, s.ID[:min(4, len(s.ID))]))
		}
	}
	return fmt.Sprintf("SubSlotManager(%d/%d) %v", m.active, maxSubSlots, ids)
}
