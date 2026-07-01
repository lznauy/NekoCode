package toolrun

import (
	"fmt"
	"sync"

	"nekocode/common"
)

const maxSubSlots = 8

type SlotManager struct {
	mu     sync.Mutex
	cond   *sync.Cond
	slots  [maxSubSlots]*common.SubSlot
	active int
}

func NewSlotManager() *SlotManager {
	m := &SlotManager{}
	m.cond = sync.NewCond(&m.mu)
	return m
}

func (m *SlotManager) Acquire(id, subType string) (colorIdx int, ok bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for m.active >= maxSubSlots {
		m.cond.Wait()
	}

	for i := range m.slots {
		if m.slots[i] == nil {
			m.slots[i] = &common.SubSlot{ID: id, SubType: subType, ColorIdx: i}
			m.active++
			return i, true
		}
	}
	return -1, false
}

func (m *SlotManager) Release(id string) {
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

func (m *SlotManager) String() string {
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
