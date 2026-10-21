package kernel

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestGateTryBoundary(t *testing.T) {
	g := NewGate(2)

	if !g.Try() {
		t.Fatal("first Try should succeed")
	}
	if !g.Try() {
		t.Fatal("second Try should succeed")
	}
	if g.Try() {
		t.Fatal("third Try should exceed budget of 2")
	}
	if g.Try() {
		t.Fatal("budget stays exhausted")
	}
}

func TestGateReset(t *testing.T) {
	g := NewGate(1)
	g.Try()
	g.Try()

	g.Reset()
	if !g.Try() {
		t.Fatal("after Reset, first Try should succeed")
	}
	if g.Try() {
		t.Fatal("after Reset, budget is 1 again")
	}
}

func TestRunLoopRunsUntilStepFinishes(t *testing.T) {
	var steps, finishes int
	RunLoop(Loop{
		Step: func() bool {
			steps++
			return steps == 3
		},
		FinishStep: func() { finishes++ },
	})

	if steps != 3 {
		t.Fatalf("steps = %d, want 3", steps)
	}
	if finishes != 1 {
		t.Fatalf("FinishStep called %d times, want 1", finishes)
	}
}

func TestRunLoopStopsWhenDone(t *testing.T) {
	var steps int
	RunLoop(Loop{
		Done: func() bool { return true },
		Step: func() bool { steps++; return false },
	})
	if steps != 0 {
		t.Fatalf("steps = %d, want 0 when Done is true before the loop", steps)
	}
}

func TestRunLoopStopsAtStepLimit(t *testing.T) {
	var steps, finishes int
	RunLoop(Loop{
		StepLimitReached: func() bool { return steps >= 5 },
		Step:             func() bool { steps++; return false },
		FinishStep:       func() { finishes++ },
	})
	if steps != 5 {
		t.Fatalf("steps = %d, want 5", steps)
	}
	if finishes != 0 {
		t.Fatalf("FinishStep called %d times, want 0 (limit break is not a finished step)", finishes)
	}
}

func TestRunLoopNilCallbacksAreSafe(t *testing.T) {

	steps := 0
	RunLoop(Loop{Step: func() bool { steps++; return true }})
	if steps != 1 {
		t.Fatalf("steps = %d, want 1", steps)
	}
}

func TestLifecycleCancelAndReplace(t *testing.T) {
	l := NewLifecycle(context.Background(), 4)

	if l.Context().Err() != nil {
		t.Fatal("fresh context should not be canceled")
	}

	l.Cancel()
	if l.Context().Err() == nil {
		t.Fatal("after Cancel, context should be canceled")
	}

	l.ReplaceContext()
	if l.Context().Err() != nil {
		t.Fatal("after ReplaceContext, context should be live again")
	}
}

func TestLifecycleResetContextIfCanceled(t *testing.T) {
	l := NewLifecycle(context.Background(), 4)

	l.ResetContextIfCanceled()
	if l.Context().Err() != nil {
		t.Fatal("reset on live context should keep it live")
	}

	l.Cancel()
	l.ResetContextIfCanceled()
	if l.Context().Err() != nil {
		t.Fatal("reset on canceled context should revive it")
	}
}

func TestLifecycleReplaceContextKeepsParentCancellation(t *testing.T) {
	parent, cancelParent := context.WithCancel(context.Background())
	l := NewLifecycle(parent, 4)

	cancelParent()
	l.ReplaceContext()
	if l.Context().Err() == nil {
		t.Fatal("context derived from canceled parent should be canceled")
	}
}

func TestLifecycleSteeringBuffer(t *testing.T) {
	l := NewLifecycle(context.Background(), 2)

	for i := 0; i < 2; i++ {
		select {
		case l.Steering() <- "msg":
		default:
			t.Fatalf("send %d should fit in buffer", i)
		}
	}
	select {
	case l.Steering() <- "overflow":
		t.Fatal("third send should not fit in buffer of 2")
	default:
	}

	if got := <-l.Steering(); got != "msg" {
		t.Fatalf("drained %q, want msg", got)
	}
}

func TestLifecycleFinishedFlagAndDuration(t *testing.T) {
	l := NewLifecycle(context.Background(), 4)

	if d := l.Duration(); d != 0 {
		t.Fatalf("before Start, Duration = %v, want 0", d)
	}

	l.Finished().Store(true)
	l.Start()
	if l.Finished().Load() {
		t.Fatal("Start should clear the finished flag")
	}
	if d := l.Duration(); d <= 0 {
		t.Fatalf("after Start, Duration = %v, want > 0", d)
	}
	if l.startTime.After(time.Now()) {
		t.Fatal("start time should not be in the future")
	}
}

func TestLifecycleTimingBoundaries(t *testing.T) {
	l := NewLifecycle(context.Background(), 1)
	l.StopTiming()
	if l.Duration() != 0 {
		t.Fatal("stopping an unstarted run must not create timing")
	}
	l.Start()
	l.Finished().Store(true)
	if !l.endTime.IsZero() {
		t.Fatal("loop termination must not end timing before finalization")
	}
	l.StopTiming()
	elapsed := l.Duration()
	l.StopTiming()
	if elapsed <= 0 || l.Duration() != elapsed {
		t.Fatal("stopped duration must remain fixed")
	}
	l.Start()
	if !l.endTime.IsZero() {
		t.Fatal("new run must restart timing")
	}
	l.ResetTiming()
	if l.Duration() != 0 {
		t.Fatal("session reset must clear timing")
	}
}

func TestLifecycleConcurrentTiming(t *testing.T) {
	l := NewLifecycle(context.Background(), 1)
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			for range 1000 {
				l.Start()
				l.Duration()
				l.StopTiming()
				l.ResetTiming()
			}
		})
	}
	wg.Wait()
}
