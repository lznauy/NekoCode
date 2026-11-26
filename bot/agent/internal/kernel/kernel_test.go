package kernel

import (
	"context"
	"errors"
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

func TestLifecycleAbortAndReplace(t *testing.T) {
	l := NewLifecycle(context.Background(), 4)

	if l.Context().Err() != nil {
		t.Fatal("fresh context should not be canceled")
	}

	l.Abort()
	if l.Context().Err() == nil {
		t.Fatal("after Abort, context should be canceled")
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

	l.Abort()
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

func TestRunValuesSurviveContextReplacementAndClear(t *testing.T) {
	type key struct{}
	lifecycle := NewLifecycle(context.Background(), 1)
	lifecycle.SetRunContext(context.WithValue(context.Background(), key{}, "run-authority"))
	if lifecycle.Context().Value(key{}) != "run-authority" {
		t.Fatal("run values lost")
	}
	old := lifecycle.Context()
	lifecycle.ReplaceContext()
	if old.Err() == nil || lifecycle.Context().Err() != nil || lifecycle.Context().Value(key{}) != "run-authority" {
		t.Fatal("steering replaced run values or lost cancellation")
	}
	lifecycle.SetRunContext(nil)
	if lifecycle.Context().Value(key{}) != nil {
		t.Fatal("run metadata leaked after cleanup")
	}
}

func TestAbortCancelsBeforePublishingFinished(t *testing.T) {
	l := NewLifecycle(t.Context(), 1)
	cancel := l.cancelFn
	l.cancelFn = func() {
		if l.Finished().Load() {
			t.Fatal("finished became visible before context cancellation")
		}
		cancel()
	}
	l.Abort()
	if !l.Finished().Load() || l.Context().Err() == nil {
		t.Fatal("abort must both cancel the context and stop the loop")
	}
}

func TestRunContextDeadlineCauseAndSteer(t *testing.T) {
	type key string
	owner := context.WithValue(t.Context(), key("owner"), 1)
	l := NewLifecycle(owner, 1)
	ctx, cancel := context.WithTimeout(context.WithValue(t.Context(), key("run"), 2), time.Minute)
	defer cancel()
	l.SetRunContext(ctx)
	defer l.SetRunContext(nil)
	first := l.Context()
	want, _ := ctx.Deadline()
	if d, ok := first.Deadline(); !ok || !d.Equal(want) {
		t.Errorf("attempt deadline = %v, want %v", d, want)
	}
	l.ReplaceContext()
	if first.Err() == nil || context.Cause(first) == nil || l.Context().Err() != nil {
		t.Errorf("steering: old err=%v cause=%v, new err=%v", first.Err(), context.Cause(first), l.Context().Err())
	}
	if l.Context().Value(key("owner")) != 1 || l.Context().Value(key("run")) != 2 {
		t.Fatal("lost construction or caller metadata")
	}
	cancel()
	l.ReplaceContext()
	if l.Context().Err() == nil {
		t.Error("steering resurrected a cancelled run")
	}
}

func TestEarlierOwnerDeadlineAndTerminalCancellation(t *testing.T) {
	for _, abort := range []bool{false, true} {
		owner, release := context.WithTimeout(t.Context(), time.Hour)
		caller, cancel := context.WithCancelCause(t.Context())
		l := NewLifecycle(owner, 1)
		l.SetRunContext(caller)
		want, _ := owner.Deadline()
		if got, ok := l.Context().Deadline(); !ok || !got.Equal(want) {
			t.Fatalf("got deadline %v, want %v", got, want)
		}
		cause := errors.New("caller stopped")
		if abort {
			l.Abort()
			cause = context.Canceled
		} else {
			cancel(cause)
		}
		l.ReplaceContext()
		l.ResetContextIfCanceled()
		if !errors.Is(context.Cause(l.Context()), cause) {
			t.Fatalf("abort=%v: replacement lost cause: %v", abort, context.Cause(l.Context()))
		}
		l.SetRunContext(nil)
		l.SetRunContext(t.Context())
		if l.Context().Err() != nil {
			t.Fatal("subsequent run inherited previous cancellation")
		}
		l.SetRunContext(nil)
		cancel(nil)
		release()
	}
}
