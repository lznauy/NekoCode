package modelbudget

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

func TestConcurrentAdmissionPreservesFinalizationReserve(t *testing.T) {
	b, _ := New(100)
	ctx := WithBudget(t.Context(), b)
	var admitted atomic.Int32
	var wg sync.WaitGroup
	for range 200 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if Admit(ctx) == nil {
				admitted.Add(1)
			}
		}()
	}
	wg.Wait()
	if admitted.Load() != 88 || b.Snapshot().Admitted != 88 {
		t.Fatalf("ordinary admission: %+v", b.Snapshot())
	}
	for i := range 8 {
		actor := string(rune('a' + i))
		ctx := WithRole(ctx, Handoff, actor)
		if err := Admit(ctx); err != nil {
			t.Fatal(err)
		}
		if !errors.Is(Admit(ctx), ErrExhausted) {
			t.Fatal("same attempt drained another handoff")
		}
	}
	for range 4 {
		if err := Admit(WithRole(ctx, Coordinator, "")); err != nil {
			t.Fatal(err)
		}
	}
	if err := Admit(WithRole(ctx, Coordinator, "")); !errors.Is(err, ErrExhausted) {
		t.Fatal(err)
	}
	if b.Snapshot().Admitted != 100 {
		t.Fatal(b.Snapshot())
	}
}
func TestBudgetSmallLimitsCancellationAndLegacy(t *testing.T) {
	for _, limit := range []int{1, 2, 3, 10, 15} {
		b, _ := New(limit)
		ctx := WithRole(WithBudget(t.Context(), b), Coordinator, "")
		n := 0
		for Admit(ctx) == nil {
			n++
		}
		if n > limit {
			t.Fatalf("%d > %d", n, limit)
		}
	}
	if err := Admit(t.Context()); err != nil {
		t.Fatal(err)
	}
	b, _ := New(10)
	ctx, cancel := context.WithCancel(WithBudget(t.Context(), b))
	cancel()
	if !errors.Is(Admit(ctx), context.Canceled) || b.Snapshot().Admitted != 0 {
		t.Fatal("cancel consumed budget")
	}
	for _, n := range []int{-1, 0, 1000001} {
		if _, err := New(n); err == nil {
			t.Fatal("invalid limit accepted")
		}
	}
}

func TestUnusedWorkerReserveCanFinishSynthesisWithoutRaisingCeiling(t *testing.T) {
	b, _ := New(100)
	ctx := WithBudget(t.Context(), b)
	b.ReleaseHandoffs()
	if b.Snapshot().HandoffRemaining != 8 {
		t.Fatal("reserve released while investigation still possible")
	}
	for Admit(ctx) == nil {
	}
	b.ReleaseHandoffs()
	s := b.Snapshot()
	if s.CoordinatorRemaining != 12 || s.HandoffRemaining != 0 {
		t.Fatal(s)
	}
	for Admit(WithRole(ctx, Coordinator, "")) == nil {
	}
	if b.Snapshot().Admitted != 100 {
		t.Fatal("shared physical ceiling changed", b.Snapshot())
	}
	b.ReleaseHandoffs()
	if Admit(WithRole(ctx, Coordinator, "")) == nil {
		t.Fatal("repeated transfer created capacity")
	}
}
