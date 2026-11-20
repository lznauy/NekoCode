package connect

import (
	"context"
	"testing"
)

func TestBaseLifecycle(t *testing.T) {
	base := NewBase(nil, "test", "Test")
	ctx, generation := base.Start(context.Background())
	if !base.IsRunning() || generation != 1 {
		t.Fatalf("Start state = running:%v generation:%d", base.IsRunning(), generation)
	}
	if err := base.Stop(); err != nil {
		t.Fatal(err)
	}
	if base.IsRunning() {
		t.Fatal("Stop left connector running")
	}
	select {
	case <-ctx.Done():
	default:
		t.Fatal("Stop did not cancel connector context")
	}
}

func TestBaseGenerationGuard(t *testing.T) {
	base := NewBase(nil, "test", "Test")
	_, gen1 := base.Start(context.Background())
	_, gen2 := base.Start(context.Background())

	// A stale generation (from an older Start) must not clear the state.
	base.MarkStopped(gen1)
	if !base.IsRunning() {
		t.Fatal("stale generation should not clear running state")
	}

	base.MarkStopped(gen2)
	if base.IsRunning() {
		t.Fatal("current generation should clear running state")
	}
}

func TestNaturalStopCancelsAllLoopsOfOnlyItsGeneration(t *testing.T) {
	b := NewBase(nil, "test", "Test")
	first, g1 := b.Start(t.Context())
	second, g2 := b.Start(t.Context())
	b.MarkStopped(g1)
	if first.Err() == nil || second.Err() != nil {
		t.Fatalf("stale completion: old=%v new=%v", first.Err(), second.Err())
	}
	b.MarkStopped(g2)
	if second.Err() == nil || b.IsRunning() {
		t.Fatal("natural stop left sibling loops active")
	}
}
