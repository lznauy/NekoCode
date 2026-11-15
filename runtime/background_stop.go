package runtime

import (
	"context"
	"fmt"
	"nekocode/mode"
	"nekocode/protocol"
)

// StopBackgroundTasks stops this session's coordination and execution together.
// Unlike CancelRun, it also discards queued demands and suppresses result review
// for the stopped batch. New user demands can start a fresh batch afterwards.
func (r *Runtime) StopBackgroundTasks(ctx context.Context, sessionID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := r.ensureOpen(); err != nil {
		return err
	}
	if sessionID == "" {
		sessionID = r.CurrentSessionID()
	}
	b := r.background
	if err := b.load(sessionID); err != nil {
		return err
	}
	// Deliver cancellation before waiting for either admission or persistence.
	// The serialized sweep below also captures work admitted during this gap.
	b.mu.Lock()
	if b.coordinatorSession == sessionID && b.coordinatorCancel != nil {
		b.coordinatorCancel()
	}
	for _, e := range b.entries {
		if e.SessionID == sessionID && !e.settling && e.cancel != nil {
			e.cancel()
		}
	}
	for _, in := range b.pending {
		if in.sessionID == sessionID {
			in.cancel()
		}
	}
	b.mu.Unlock()
	if err := r.mutationMu.LockContext(ctx); err != nil {
		return err
	}
	locked := true
	defer func() {
		if locked {
			r.mutationMu.Unlock()
		}
	}()
	if err := r.ensureOpen(); err != nil {
		return err
	}
	// Cancel admission before locking the task queue. An already admitted submit
	// must either finish before this sweep or observe its cancelled context.
	var control cancelControl
	var active bool
	if sessionID == r.CurrentSessionID() && r.services.Modes.Current != nil && r.services.Modes.Current() == mode.SuperAgent {
		var err error
		control, active, err = r.beginCancel("")
		if err != nil {
			return err
		}
		if active {
			control.lease.deactivate()
			if control.cancel != nil {
				control.cancel()
			}
		}
	}
	b.lockMutation()
	if b.coordinatorSession == sessionID && b.coordinatorCancel != nil {
		b.coordinatorCancel()
	}
	owners := []RunID{}
	queued := []string{}
	suppressed, stopped := 0, 0
	state := b.snapshotLocked(sessionID)
	for i := range state.Tasks {
		task := &state.Tasks[i]
		if pendingBackgroundStatus(task.Status) {
			stopped++
		} else if !task.Notified && task.Delivery != "delivered" && task.Delivery != "stopped" {
			suppressed++
		}
		task.Notified, task.Delivery = true, "stopped"
		e := b.entries[task.ID]
		if !e.settling && e.cancel != nil {
			e.cancel()
		}
		if task.Status == "queued" || task.Status == "waiting_dependencies" {
			queued = append(queued, task.ID)
			task.Status, task.Error = "interrupted", "stopped before execution"
		}
		owners = append(owners, e.interactionID)
	}
	for i := range state.Demands {
		d := &state.Demands[i]
		if d.Status == "queued" || d.Status == "claimed" || d.Status == "pending" {
			d.Status = "cancelled"
		}
	}
	err := b.saveStateLocked(sessionID, state)
	// A stop remains effective even if its receipt cannot be saved. Return the
	// storage error, but never restart work the user explicitly cancelled.
	for _, task := range state.Tasks {
		e := b.entries[task.ID]
		e.Notified, e.Delivery = task.Notified, task.Delivery
	}
	for _, id := range queued {
		e := b.entries[id]
		e.Status, e.Error = "interrupted", "stopped before execution"
		releaseBackgroundWork(e)
		e.work.Run = nil
	}
	for _, d := range state.Demands {
		*b.demands[d.ID] = d
	}
	retained := b.inputs[:0]
	for _, in := range b.inputs {
		if in.sessionID != sessionID {
			retained = append(retained, in)
		}
	}
	b.inputs = retained
	for id, in := range b.pending {
		if in.sessionID == sessionID {
			in.cancel()
			if active && id == control.runID {
				continue
			}
			in.finish()
			delete(b.pending, id)
		}
	}
	b.unlockMutation()
	r.mutationMu.Unlock()
	locked = false
	for _, id := range queued {
		r.publishStepForSession(RunID("task_"+id), protocol.StepEvent{Action: protocol.StepActionSubAgentEnd, SubAgentID: id, SubAgentStatus: "interrupted"}, sessionID)
	}
	for _, owner := range owners {
		r.approvals.RejectRun(owner)
		r.questions.RejectRun(owner)
	}
	if active {
		r.approvals.RejectRun(control.runID)
		r.questions.RejectRun(control.runID)
		go r.publishCancellation(control, "stopped all tasks")
	}
	b.signal()
	if err == nil && sessionID == r.CurrentSessionID() {
		r.Notify(fmt.Sprintf("已停止 %d 项后台任务；%d 项已结束但尚未消费的结果不再自动验收，结果记录仍保留，可在子任务详情中查看。", stopped, suppressed))
	}
	return err
}
