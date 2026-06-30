package runtime

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

const steeringChBuffer = 4

type lifecycleState struct {
	mu        sync.Mutex
	parentCtx context.Context
	curCtx    context.Context
	cancelFn  context.CancelFunc
	steering  chan string
	startTime time.Time
	finished  atomic.Bool
}

func newLifecycleState(parent context.Context) lifecycleState {
	ctx, cancel := context.WithCancel(parent)
	return lifecycleState{
		parentCtx: parent,
		curCtx:    ctx,
		cancelFn:  cancel,
		steering:  make(chan string, steeringChBuffer),
	}
}

func (s *lifecycleState) context() context.Context {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.curCtx
}

func (s *lifecycleState) replaceContext() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancelFn()
	s.curCtx, s.cancelFn = context.WithCancel(s.parentCtx)
}

func (s *lifecycleState) resetContextIfCanceled() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.curCtx.Err() != nil {
		s.curCtx, s.cancelFn = context.WithCancel(s.parentCtx)
	}
}

func (s *lifecycleState) cancel() {
	s.mu.Lock()
	s.cancelFn()
	s.mu.Unlock()
}

func (s *lifecycleState) duration() time.Duration {
	if s.startTime.IsZero() {
		return 0
	}
	return time.Since(s.startTime)
}

func (s *lifecycleState) start() {
	s.finished.Store(false)
	s.startTime = time.Now()
}
