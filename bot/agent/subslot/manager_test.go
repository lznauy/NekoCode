package subslot

import "testing"

func TestSubSlotManagerAcquireRelease(t *testing.T) {
	m := NewManager()

	color, ok := m.Acquire("sub-1", "executor")
	if !ok {
		t.Fatal("Acquire failed")
	}
	if color != 0 {
		t.Fatalf("color = %d, want first slot 0", color)
	}
	if m.Count() != 1 {
		t.Fatalf("count = %d, want 1", m.Count())
	}
	if active := m.Active(); len(active) != 1 || active[0].ID != "sub-1" {
		t.Fatalf("active = %+v, want sub-1", active)
	}

	m.Release("sub-1")
	if m.Count() != 0 {
		t.Fatalf("count after release = %d, want 0", m.Count())
	}
}

func TestSubSlotManagerReleaseUnknownIsNoop(t *testing.T) {
	m := NewManager()
	m.Release("missing")
	if m.Count() != 0 {
		t.Fatalf("count = %d, want 0", m.Count())
	}
}
