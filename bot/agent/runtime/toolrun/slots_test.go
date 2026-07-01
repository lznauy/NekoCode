package toolrun

import (
	"strings"
	"testing"
)

func TestSubSlotManagerAcquireRelease(t *testing.T) {
	m := NewSlotManager()

	color, ok := m.Acquire("sub-1", "executor")
	if !ok {
		t.Fatal("Acquire failed")
	}
	if color != 0 {
		t.Fatalf("color = %d, want first slot 0", color)
	}
	if s := m.String(); !strings.Contains(s, "1/8") {
		t.Fatalf("after acquire, String = %q, want 1/8", s)
	}

	m.Release("sub-1")
	if s := m.String(); !strings.Contains(s, "0/8") {
		t.Fatalf("after release, String = %q, want 0/8", s)
	}
}

func TestSubSlotManagerReleaseUnknownIsNoop(t *testing.T) {
	m := NewSlotManager()
	m.Release("missing")
	if s := m.String(); !strings.Contains(s, "0/8") {
		t.Fatalf("after noop release, String = %q, want 0/8", s)
	}
}
