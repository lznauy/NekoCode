package runtime

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

func TestMutationGateMixedWaitersShareExclusion(t *testing.T) {
	gate := make(mutationGate, 1)
	var active atomic.Int32
	var visits int
	var wg sync.WaitGroup
	for worker := range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 50 {
				if worker%2 == 0 {
					gate.Lock()
				} else if err := gate.LockContext(t.Context()); err != nil {
					t.Error(err)
					return
				}
				if active.Add(1) != 1 {
					t.Error("mutation operations overlapped")
				}
				visits++
				if gate.TryLock() {
					t.Error("TryLock bypassed the same exclusion boundary")
					gate.Unlock()
				}
				active.Add(-1)
				gate.Unlock()
			}
		}()
	}
	wg.Wait()
	if visits != 32*50 {
		t.Fatalf("lost operations: %d", visits)
	}
}

func TestMutationGateCancelledWaitDoesNotConsumeAdmission(t *testing.T) {
	gate := make(mutationGate, 1)
	gate.Lock()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- gate.LockContext(ctx) }()
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("waiting cancellation: %v", err)
	}
	gate.Unlock()
	if err := gate.LockContext(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled caller entered free gate: %v", err)
	}
	if !gate.TryLock() {
		t.Fatal("cancelled waiter retained the gate")
	}
	gate.Unlock()
	if err := gate.LockContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	gate.Unlock()
}
