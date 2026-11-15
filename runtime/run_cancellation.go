package runtime

import "context"

// CancellationState describes the lifecycle boundary observed atomically with
// cancellation. Only queued work can disappear without a terminal run event.
type CancellationState string

const (
	CancellationAbsent   CancellationState = "absent"
	CancellationQueued   CancellationState = "queued_removed"
	CancellationStarted  CancellationState = "started"
	CancellationTerminal CancellationState = "terminal"
)

type RunCancellation struct {
	RunID RunID             `json:"run_id,omitempty"`
	State CancellationState `json:"state"`
}

// CancelRunWithResult selects the current execution when id is empty. Admission
// and queued removal use the same mutation lock, so callers never infer whether
// a cancellation raced with the queued-to-started transition.
func (r *Runtime) CancelRunWithResult(ctx context.Context, id RunID) (RunCancellation, error) {
	if err := ctx.Err(); err != nil {
		return RunCancellation{}, err
	}
	if err := r.ensureOpen(); err != nil {
		return RunCancellation{}, err
	}
	if err := r.mutationMu.LockContext(ctx); err != nil {
		return RunCancellation{}, err
	}
	if err := r.ensureOpen(); err != nil {
		r.mutationMu.Unlock()
		return RunCancellation{}, err
	}
	r.mu.Lock()
	if id == "" {
		id = r.currentRun
	}
	current := r.currentRun
	status := r.status
	r.mu.Unlock()
	result := RunCancellation{RunID: id, State: CancellationAbsent}
	if id == "" {
		r.mutationMu.Unlock()
		return result, nil
	}
	if id != current {
		if _, ok := r.LookupRun(id); ok {
			r.mutationMu.Unlock()
			result.State = CancellationTerminal
			return result, nil
		}
		if handled, err := r.cancelQueuedInputLocked(id); handled {
			r.mutationMu.Unlock()
			result.State = CancellationQueued
			return result, err
		}
	}
	if id == current && (status == RunIdle || status == RunDone || status == RunFailed || status == RunCancelled) {
		r.mutationMu.Unlock()
		result.State = CancellationTerminal
		return result, nil
	}
	control, active, err := r.beginCancel(id)
	r.mutationMu.Unlock()
	if err != nil {
		return result, err
	}
	if !active {
		if control.runID != "" {
			result.State = CancellationTerminal
		}
		return result, nil
	}
	result.State = CancellationStarted
	return result, r.cancelExecution(ctx, control)
}

func (r *Runtime) cancelExecution(ctx context.Context, control cancelControl) error {
	control.lease.deactivate()
	if control.cancel != nil {
		control.cancel()
	}
	r.approvals.RejectRun(control.runID)
	r.questions.RejectRun(control.runID)
	if err := control.lease.wait(ctx); err != nil {
		go r.publishCancellation(control, "cancelled")
		return err
	}
	go r.publishCancellation(control, "cancelled")
	return nil
}
