package kernel

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// Gate is a retry budget: each Try consumes one attempt and reports whether
// the budget is still within the limit. It carries no policy, logging, or
// hint text — those belong to the caller.
type Gate struct {
	maxRetries int
	retries    int
}

// NewGate creates a Gate allowing up to maxRetries retries.
func NewGate(maxRetries int) *Gate {
	return &Gate{maxRetries: maxRetries}
}

// Try consumes one retry attempt and reports whether it is within budget.
func (g *Gate) Try() bool {
	g.retries++
	return g.retries <= g.maxRetries
}

// Reset restores the full retry budget.
func (g *Gate) Reset() { g.retries = 0 }

// Loop is the generic step-loop skeleton. Each callback is supplied by the
// caller; nil callbacks are treated as absent (Done and StepLimitReached
// report false and FinishStep is skipped).
type Loop struct {
	Done             func() bool
	StepLimitReached func() bool
	Step             func() bool
	FinishStep       func()
}

// RunLoop runs the loop until Done, the step limit is reached, or a step
// reports itself finished.
func RunLoop(loop Loop) {
	for !loop.done() {
		if loop.stepLimitReached() {
			break
		}
		if loop.Step() {
			if loop.FinishStep != nil {
				loop.FinishStep()
			}
			break
		}
	}
}

func (loop Loop) done() bool {
	return loop.Done != nil && loop.Done()
}

func (loop Loop) stepLimitReached() bool {
	return loop.StepLimitReached != nil && loop.StepLimitReached()
}

// Lifecycle is the interruptible, revivable execution context of an agent
// run: Cancel aborts the current context, ReplaceContext derives a fresh one
// from the parent (used to retry after interruption), and the steering
// mailbox lets callers inject messages while a run is in flight.
type Lifecycle struct {
	mu        sync.Mutex
	parentCtx context.Context
	curCtx    context.Context
	cancelFn  context.CancelFunc
	steering  chan string
	startTime time.Time
	endTime   time.Time
	finished  atomic.Bool
}

// NewLifecycle derives the initial context from parent and creates a
// steering mailbox with the given buffer size.
func NewLifecycle(parent context.Context, steeringBuf int) *Lifecycle {
	ctx, cancel := context.WithCancel(parent)
	return &Lifecycle{
		parentCtx: parent,
		curCtx:    ctx,
		cancelFn:  cancel,
		steering:  make(chan string, steeringBuf),
	}
}

// Context returns the current run context.
func (l *Lifecycle) Context() context.Context {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.curCtx
}

// ReplaceContext cancels the current context and derives a fresh one from
// the parent.
func (l *Lifecycle) ReplaceContext() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.cancelFn()
	l.curCtx, l.cancelFn = context.WithCancel(l.parentCtx)
}

// ResetContextIfCanceled derives a fresh context from the parent only when
// the current one is already canceled.
func (l *Lifecycle) ResetContextIfCanceled() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.curCtx.Err() != nil {
		l.curCtx, l.cancelFn = context.WithCancel(l.parentCtx)
	}
}

// Cancel aborts the current context.
func (l *Lifecycle) Cancel() {
	l.mu.Lock()
	l.cancelFn()
	l.mu.Unlock()
}

// Duration reports elapsed run time, fixed after StopTiming, or 0 before Start.
func (l *Lifecycle) Duration() time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.startTime.IsZero() {
		return 0
	}
	if !l.endTime.IsZero() {
		return l.endTime.Sub(l.startTime)
	}
	return time.Since(l.startTime)
}

// Start marks the run as unfinished and records its start time.
func (l *Lifecycle) Start() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.finished.Store(false)
	l.startTime = time.Now()
	l.endTime = time.Time{}
}

// StopTiming freezes elapsed time after all run processing has finished.
// It is idempotent and does not change the loop's termination flag.
func (l *Lifecycle) StopTiming() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.startTime.IsZero() && l.endTime.IsZero() {
		l.endTime = time.Now()
	}
}

// ResetTiming clears the previous run's timing between sessions.
func (l *Lifecycle) ResetTiming() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.startTime, l.endTime = time.Time{}, time.Time{}
}

// Finished exposes the run's finished flag for atomic Load/Store by callers.
func (l *Lifecycle) Finished() *atomic.Bool {
	return &l.finished
}

// Steering returns the steering mailbox: senders inject messages, the run
// loop drains them.
func (l *Lifecycle) Steering() chan string {
	return l.steering
}
