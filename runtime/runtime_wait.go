package runtime

import "context"

// WaitRun waits until the identified run has completed all lifecycle cleanup.
// It is safe to call after receiving a terminal run event.
func (r *Runtime) WaitRun(ctx context.Context, runID RunID) error {
	r.background.mu.Lock()
	var queuedDone chan struct{}
	if input := r.background.pending[runID]; input != nil {
		queuedDone = input.done
	}
	r.background.mu.Unlock()
	if queuedDone != nil {
		select {
		case <-queuedDone:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return r.waitActiveRun(ctx, runID)
}

func (r *Runtime) waitActiveRun(ctx context.Context, runID RunID) error {
	r.mu.Lock()
	if runID == "" || r.currentRun != runID || r.status == RunIdle {
		r.mu.Unlock()
		return nil
	}
	done := r.runDone
	r.mu.Unlock()
	if done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
