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

// Lifecycle owns the current run and its replaceable model/tool attempt.
// Steering cancels one attempt. Aborting a bound run cancels every attempt.
type Lifecycle struct {
	mu            sync.Mutex
	parentCtx     context.Context
	attemptParent context.Context
	cancelRun     context.CancelCauseFunc
	otherParent   context.Context
	stopOther     func() bool
	curCtx        context.Context
	cancelFn      context.CancelFunc
	steering      chan string
	startTime     time.Time
	endTime       time.Time
	finished      atomic.Bool
}

// NewLifecycle derives the initial context from parent and creates a
// steering mailbox with the given buffer size.
func NewLifecycle(parent context.Context, steeringBuf int) *Lifecycle {
	ctx, cancel := context.WithCancel(parent)
	return &Lifecycle{
		parentCtx:     parent,
		attemptParent: parent,
		curCtx:        ctx,
		cancelFn:      cancel,
		steering:      make(chan string, steeringBuf),
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
	l.newAttemptLocked()
}

// ResetContextIfCanceled derives a fresh context from the parent only when
// the current one is already canceled.
func (l *Lifecycle) ResetContextIfCanceled() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.curCtx.Err() != nil {
		l.newAttemptLocked()
	}
}

// Abort publishes cancellation before marking the loop finished. A normally
// finished loop may still be synthesizing a report on a live context.
func (l *Lifecycle) Abort() {
	l.mu.Lock()
	if l.cancelRun != nil {
		l.cancelRun(context.Canceled)
	}
	l.cancelFn()
	l.finished.Store(true)
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

// SetRunContext binds the caller for the duration of one owned run. Steering
// can replace an attempt, but every such attempt remains a child of this run.
// Passing nil ends the binding and releases its cancellation registrations.
func (l *Lifecycle) SetRunContext(caller context.Context) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.cancelFn()
	if l.cancelRun != nil {
		l.stopOther()
		l.cancelRun(context.Canceled)
		l.cancelRun, l.otherParent, l.stopOther = nil, nil, nil
	}
	l.attemptParent = l.parentCtx
	if caller != nil {
		// Derive directly from the parent with the earlier deadline so the
		// standard library propagates its deadline, Err and Cause together.
		primary, other := caller, l.parentCtx
		ownerDeadline, ownerTimed := l.parentCtx.Deadline()
		callerDeadline, callerTimed := caller.Deadline()
		if ownerTimed && (!callerTimed || ownerDeadline.Before(callerDeadline)) {
			primary, other = l.parentCtx, caller
		}
		base := runParents{primary, context.WithoutCancel(caller), context.WithoutCancel(l.parentCtx)}
		run, cancel := context.WithCancelCause(base)
		l.stopOther = context.AfterFunc(other, func() { cancel(context.Cause(other)) })
		l.attemptParent, l.cancelRun, l.otherParent = run, cancel, other
	}
	l.newAttemptLocked()
}

// A steer can race the secondary parent's asynchronous cancellation callback.
func (l *Lifecycle) newAttemptLocked() {
	if l.otherParent != nil && l.otherParent.Err() != nil {
		l.cancelRun(context.Cause(l.otherParent))
	}
	l.curCtx, l.cancelFn = context.WithCancel(l.attemptParent)
}

// The value-only views keep run metadata ahead of construction defaults.
// Cancellation internals come exclusively from the actual parent.
type runParents struct {
	context.Context
	callerValues, ownerValues context.Context
}

func (p runParents) Value(key any) any {
	if v := p.callerValues.Value(key); v != nil {
		return v
	}
	if v := p.ownerValues.Value(key); v != nil {
		return v
	}
	return p.Context.Value(key)
}
