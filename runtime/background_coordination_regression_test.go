package runtime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestBackgroundQueryDoesNotWakeCoordinator(t *testing.T) {
	ran := make(chan struct{}, 1)
	rt := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) {
		ran <- struct{}{}
		return "", nil
	}), backgroundServices(t.TempDir()))
	defer rt.Close()
	b := rt.background
	b.mu.Lock()
	b.entries["done"] = &backgroundEntry{BackgroundTask: BackgroundTask{ID: "done", SessionID: "session", Status: "completed", Result: "evidence"}}
	b.order = append(b.order, "done")
	b.mu.Unlock()
	if _, err := rt.BackgroundTasks("session"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ran:
		t.Fatal("receipt query started a model turn")
	case <-time.After(100 * time.Millisecond):
	}
	// Explicit activation must still deliver the saved notification.
	b.signal()
	awaitBackground(t, ran)
}

func TestBackgroundReviewAdmissionFailureIsVisibleAndNotReplayed(t *testing.T) {
	root := t.TempDir()
	rt := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) {
		t.Error("recording failure must prevent execution")
		return "", nil
	}), backgroundServices(root))
	defer rt.Close()
	if err := rt.EnableEventRecording(root); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "session"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "session", "runs"), []byte("blocked"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	events, err := rt.Events(ctx, EventFilter{Types: []EventType{EventSystemMessage}})
	if err != nil {
		t.Fatal(err)
	}
	b := rt.background
	b.mu.Lock()
	b.entries["done"] = &backgroundEntry{BackgroundTask: BackgroundTask{ID: "done", SessionID: "session", Status: "completed", Result: "evidence"}}
	b.order = append(b.order, "done")
	b.mu.Unlock()
	b.signal()
	select {
	case <-events:
	case <-ctx.Done():
		t.Fatal("review admission failure was silent")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.entries["done"].Delivery != "failed" || b.entries["done"].Notified {
		t.Fatalf("failed review must remain available for explicit recovery: %+v", b.entries["done"])
	}
}

func TestBackgroundCoordinatorReleaseAlwaysWakes(t *testing.T) {
	b := &backgroundTasks{coordinating: true, coordinatorSession: "session", wake: make(chan struct{}, 1)}
	b.releaseCoordinator()
	if b.coordinating || b.coordinatorSession != "" || b.coordinatorCancel != nil {
		t.Fatal("coordinator reservation was retained")
	}
	select {
	case <-b.wake:
	default:
		t.Fatal("released reservation lost pending work wake")
	}
}

func TestCoordinatorRetriesTransientClaimWrite(t *testing.T) {
	for _, demand := range []bool{false, true} {
		t.Run(fmt.Sprintf("queued-demand-%t", demand), func(t *testing.T) {
			root := t.TempDir()
			tmpFile := filepath.Join(root, "file")
			if err := os.WriteFile(tmpFile, nil, 0600); err != nil {
				t.Fatal(err)
			}
			services := backgroundServices(root)
			path := services.Sessions.BackgroundStatePath
			var failNext atomic.Bool
			services.Sessions.BackgroundStatePath = func(id string) string {
				if failNext.CompareAndSwap(true, false) {
					return filepath.Join(tmpFile, "background.json")
				}
				return path(id)
			}
			ran := make(chan struct{}, 1)
			rt := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) { ran <- struct{}{}; return "ok", nil }), services)
			defer rt.Close()
			b := rt.background
			if err := b.load("session"); err != nil {
				t.Fatal(err)
			}
			if demand {
				// Hold admission so the input is queued before the failing claim.
				rt.mutationMu.Lock()
				_, err := rt.queueBackgroundInput(t.Context(), Input{Text: "queued"})
				failNext.Store(true)
				rt.mutationMu.Unlock()
				if err != nil {
					t.Fatal(err)
				}
			} else {
				b.mu.Lock()
				b.entries["result"] = &backgroundEntry{BackgroundTask: BackgroundTask{ID: "result", SessionID: "session", Status: "completed", Result: "ready"}}
				b.order = append(b.order, "result")
				failNext.Store(true)
				b.mu.Unlock()
				b.signal()
			}
			select {
			case <-ran:
			case <-time.After(2 * time.Second):
				t.Fatal("ready work stranded after one failed claim write")
			}
			if failNext.Load() {
				t.Fatal("failure was not exercised")
			}
		})
	}
}

func TestReviewSettlementHonorsClaimOwnershipAndReceiptWrite(t *testing.T) {
	for _, failWrite := range []bool{false, true} {
		t.Run(fmt.Sprintf("fail-write-%t", failWrite), func(t *testing.T) {
			root := t.TempDir()
			if failWrite {
				root = filepath.Join(root, "blocked")
				if err := os.WriteFile(root, nil, 0600); err != nil {
					t.Fatal(err)
				}
			}
			b := &backgroundTasks{runtime: &Runtime{services: backgroundServices(root)},
				order: []string{"current", "replaced", "stopped"}, entries: make(map[string]*backgroundEntry)}
			claimed := make(map[string]*backgroundEntry)
			for _, id := range b.order {
				e := &backgroundEntry{BackgroundTask: BackgroundTask{ID: id, SessionID: "session", Delivery: "claimed"}}
				b.entries[id], claimed[id] = e, e
			}
			replacement := *b.entries["replaced"]
			b.entries["replaced"] = &replacement
			b.entries["stopped"].Delivery = "stopped"

			b.lockMutation()
			err := b.settleReviewsLocked("session", claimed, coordinatorDone, nil)
			b.unlockMutation()
			current := b.entries["current"]
			if failWrite {
				if err == nil || current.Notified || current.Delivery != "failed" {
					t.Fatalf("failed receipt write: task=%+v error=%v", current, err)
				}
			} else if err != nil || !current.Notified || current.Delivery != "delivered" {
				t.Fatalf("completion: task=%+v error=%v", current, err)
			}
			if replacement.Delivery != "claimed" || replacement.Notified || b.entries["stopped"].Delivery != "stopped" || b.entries["stopped"].Notified {
				t.Fatal("settlement modified a replaced or stopped claim")
			}
		})
	}
}

func TestDemandReceiptFailureReleasesClaim(t *testing.T) {
	root := filepath.Join(t.TempDir(), "blocked")
	if err := os.WriteFile(root, nil, 0600); err != nil {
		t.Fatal(err)
	}
	demand := &BackgroundDemand{ID: "queued_test", SessionID: "session", Status: "claimed"}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	input := &backgroundInput{id: demand.ID, ctx: ctx, cancel: cancel, done: make(chan struct{})}
	b := &backgroundTasks{runtime: &Runtime{services: backgroundServices(root)}, demandOrder: []RunID{demand.ID},
		demands: map[RunID]*BackgroundDemand{demand.ID: demand}, pending: map[RunID]*backgroundInput{demand.ID: input}}
	b.lockMutation()
	err := b.settleDemandLocked(input, coordinatorDone, nil)
	b.unlockMutation()
	if err == nil || demand.Status != "interrupted" || demand.Error == "" || b.pending[demand.ID] != nil {
		t.Fatalf("receipt could not be inspected or discarded after the write failure: %+v, err=%v", demand, err)
	}
}
