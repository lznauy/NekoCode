package agent

import (
	"fmt"
	"testing"
)

func TestConfiguredSlotLimitCanExceedDefault(t *testing.T) {
	for _, limit := range []int{1, 3, 12} {
		m := newSlotManager(limit)
		for i := 0; i < limit; i++ {
			if _, ok := m.Acquire(fmt.Sprint(i), "explore"); !ok {
				t.Fatalf("limit=%d rejected task %d", limit, i)
			}
		}
		if _, ok := m.Acquire("overflow", "explore"); ok {
			t.Fatalf("exceeded configured limit %d", limit)
		}
	}
}

func TestSlotManagerAcquireUpToLimit(t *testing.T) {
	m := newSlotManager()
	for i := 0; i < maxSubSlots; i++ {
		idx, ok := m.Acquire(fmt.Sprintf("id-%d", i), "coder")
		if !ok {
			t.Fatalf("expected slot %d to be acquired", i)
		}
		if idx != i {
			t.Fatalf("expected color idx %d, got %d", i, idx)
		}
	}
	if m.active != maxSubSlots {
		t.Fatalf("expected %d active slots, got %d", maxSubSlots, m.active)
	}
}

func TestSlotManagerFullReturnsImmediately(t *testing.T) {
	m := newSlotManager()
	for i := 0; i < maxSubSlots; i++ {
		m.Acquire(fmt.Sprintf("id-%d", i), "coder")
	}
	if _, ok := m.Acquire("overflow", "coder"); ok {
		t.Fatal("expected acquire to fail when all slots are full")
	}
}

func TestSlotManagerReleaseFreesSlot(t *testing.T) {
	m := newSlotManager()
	first := "id-0"
	for i := 0; i < maxSubSlots; i++ {
		m.Acquire(fmt.Sprintf("id-%d", i), "coder")
	}
	m.Release(first)
	idx, ok := m.Acquire("new", "coder")
	if !ok {
		t.Fatal("expected acquire after release to succeed")
	}
	if idx != 0 {
		t.Fatalf("expected freed slot 0 to be reused, got %d", idx)
	}
	// Releasing an unknown id must not corrupt accounting.
	m.Release("unknown")
	if m.active != maxSubSlots {
		t.Fatalf("expected %d active slots, got %d", maxSubSlots, m.active)
	}
}
