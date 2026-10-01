package runtime

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

const redactedSecret = "[redacted]"

// RedactInputText masks connector credentials before persistence or display.
func RedactInputText(input string) string {
	fields := strings.Fields(strings.TrimSpace(input))
	if len(fields) >= 3 &&
		strings.EqualFold(strings.TrimPrefix(fields[0], "/"), "plugin") &&
		strings.EqualFold(fields[1], "install") {
		if parsed, err := url.Parse(fields[2]); err == nil {
			if parsed.User != nil {
				parsed.User = nil
				fields[2] = parsed.String()
				return strings.Join(fields, " ")
			}
		}
		if at := strings.LastIndex(fields[2], "@"); at >= 0 {
			suffix := fields[2][at+1:]
			host := strings.ToLower(suffix)
			if strings.HasPrefix(host, "github.com:") || strings.HasPrefix(host, "gitlab.com:") {
				fields[2] = suffix
				return strings.Join(fields, " ")
			}
		}
	}
	if len(fields) < 4 || !strings.EqualFold(strings.TrimPrefix(fields[0], "/"), "connect") {
		return input
	}
	switch {
	case strings.EqualFold(fields[2], "token"):
		return strings.Join(append(fields[:3], redactedSecret), " ")
	case strings.EqualFold(fields[2], "add"):
		keep := 3
		if len(fields) >= 5 {
			keep = 4
		}
		return strings.Join(append(fields[:keep], redactedSecret), " ")
	default:
		return input
	}
}

func (r *Runtime) StartRun(ctx context.Context, input Input) (RunID, error) {
	if strings.TrimSpace(input.Text) == "" {
		return "", protocolError(ErrorInvalidInput, "start_run", "empty input")
	}
	if len(input.Images) > MaxImageAttachments {
		return "", protocolError(ErrorInvalidInput, "start_run", "too many image attachments")
	}
	r.mutationMu.Lock()
	defer r.mutationMu.Unlock()
	if len(input.Images) > 0 && !r.ImageAttachmentsEnabled() {
		return "", protocolError(ErrorUnsupported, "start_run", "image attachments require a configured image understanding model")
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return "", protocolError(ErrorClosed, "start_run", "closed")
	}
	if r.mutating {
		r.mu.Unlock()
		return "", protocolError(ErrorBusy, "start_run", "runtime capability mutation in progress")
	}
	if r.status != RunIdle {
		runID := r.currentRun
		status := r.status
		r.mu.Unlock()
		return runID, protocolError(ErrorBusy, "start_run", fmt.Sprintf("run %s is %s", runID, status))
	}
	r.nextRun++
	runID := RunID(fmt.Sprintf("run_%d", r.nextRun))
	r.currentRun = runID
	r.status = RunRunning
	delete(r.cancelled, runID)
	runCtx, cancel := context.WithCancel(ctx)
	r.runContext = runCtx
	r.cancelRun = cancel
	r.cancelDone = nil
	r.runDone = make(chan struct{})
	lease := newRunLease()
	execution := newRunExecution()
	r.runLease = lease
	r.runExecution = execution
	r.mu.Unlock()

	go r.run(runCtx, runID, input, lease, execution)
	return runID, nil
}

func (r *Runtime) SteerRun(ctx context.Context, runID RunID, input Input) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(input.Images) > MaxImageAttachments {
		return protocolError(ErrorInvalidInput, "steer_run", "too many image attachments")
	}
	if len(input.Images) > 0 && !r.ImageAttachmentsEnabled() {
		return protocolError(ErrorUnsupported, "steer_run", "image attachments require a configured image understanding model")
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return protocolError(ErrorClosed, "steer_run", "closed")
	}
	if runID == "" {
		runID = r.currentRun
	}
	if runID == "" || r.currentRun == "" {
		r.mu.Unlock()
		return protocolError(ErrorNotFound, "steer_run", "no active run")
	}
	if runID != r.currentRun {
		currentRun := r.currentRun
		r.mu.Unlock()
		return protocolError(ErrorConflict, "steer_run", fmt.Sprintf("run %s is not active; current run is %s", runID, currentRun))
	}
	if r.status != RunRunning {
		status := r.status
		r.mu.Unlock()
		return protocolError(ErrorConflict, "steer_run", fmt.Sprintf("run is %s", status))
	}
	if _, cancelled := r.cancelled[runID]; cancelled {
		r.mu.Unlock()
		return protocolError(ErrorConflict, "steer_run", fmt.Sprintf("run %s is cancelled", runID))
	}
	steer := r.services.Steer
	lease := r.runLease
	runCtx := r.runContext
	r.mu.Unlock()

	if steer == nil {
		return protocolError(ErrorUnsupported, "steer_run", "runner does not support steering")
	}
	steerCtx, cancelSteer := context.WithCancel(ctx)
	stopRunCancel := context.AfterFunc(runCtx, cancelSteer)
	defer func() {
		stopRunCancel()
		cancelSteer()
	}()
	var steerErr error
	if !lease.guard(func() {
		steerErr = steer(steerCtx, InputWithImageAttachments(input.Text, input.Images))
		if steerErr == nil {
			r.events.Publish(Event{
				RunID:  runID,
				Type:   EventInputAccepted,
				Source: input.Source,
				Payload: MessagePayload{
					Role:    "user",
					Content: RedactInputText(input.Text),
					Source:  input.Source,
					Sender:  input.Sender,
					Images:  ValidImageAttachments(input.Text, input.Images),
				},
			})
		}
	}) {
		return protocolError(ErrorConflict, "steer_run", fmt.Sprintf("run %s is no longer active", runID))
	}
	if steerErr != nil {
		return steerErr
	}
	return nil
}

func (r *Runtime) CancelRun(ctx context.Context, runID RunID) error {
	if err := r.ensureOpen(); err != nil {
		return err
	}
	control, active, err := r.beginCancel(runID)
	if err != nil {
		return err
	}
	if !active {
		return nil
	}
	control.lease.deactivate()
	if control.cancel != nil {
		control.cancel()
	}
	r.approvals.RejectAll()
	r.questions.RejectAll()
	if err := control.lease.wait(ctx); err != nil {
		go r.publishCancellation(control, "cancelled")
		return err
	}
	// Runner shutdown may involve provider cleanup and final usage accounting.
	// Keep CancelRun responsive while the terminal event is serialized after
	// that work by publishCancellation.
	go r.publishCancellation(control, "cancelled")
	return nil
}

func (r *Runtime) publishCancellation(control cancelControl, message string) {
	_ = control.lease.wait(context.Background())
	control.execution.wait()
	r.approvals.RejectAll()
	r.questions.RejectAll()
	if control.execution.hasMetrics() {
		r.publishMetrics(control.runID)
	}
	r.events.Publish(Event{
		RunID:   control.runID,
		Type:    EventRunCancelled,
		Source:  SourceRef{Kind: "runtime"},
		Payload: RunResult{Error: message},
	})
	close(control.published)
}

func (r *Runtime) DecideApproval(ctx context.Context, approvalID string, decision ApprovalDecision) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := r.ensureOpen(); err != nil {
		return err
	}
	return r.approvals.Decide(approvalID, decision)
}

func (r *Runtime) AnswerQuestion(ctx context.Context, questionID string, reply QuestionReply) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := r.ensureOpen(); err != nil {
		return err
	}
	return r.questions.Answer(questionID, reply)
}

// Close stops the active run and releases all runtime-owned resources. It must
// be called by the application lifecycle, not synchronously from Runner or
// command callbacks.
func (r *Runtime) Close() error {
	r.closeOnce.Do(func() {
		var errs []error
		r.mu.Lock()
		r.closed = true
		runID := r.currentRun
		active := r.status == RunRunning || r.status == RunWaitingApproval || r.status == RunWaitingQuestion
		cancel := r.cancelRun
		r.cancelRun = nil
		r.runContext = nil
		runDone := r.runDone
		cancelDone := r.cancelDone
		lease := r.runLease
		execution := r.runExecution
		r.runLease = nil
		if active {
			r.cancelled[runID] = struct{}{}
			r.status = RunCancelled
			cancelDone = make(chan struct{})
			r.cancelDone = cancelDone
		}
		r.mu.Unlock()

		// No new mutation can start after closed is set. Wait for an existing
		// model/config/extension/session mutation before closing runner-owned
		// resources.
		r.mutationMu.Lock()
		defer r.mutationMu.Unlock()
		r.recordingMu.Lock()
		defer r.recordingMu.Unlock()

		lease.deactivate()
		if cancel != nil {
			cancel()
		}
		if active {
			r.approvals.Close()
			r.questions.Close()
			_ = lease.wait(context.Background())
			execution.wait()
			if execution.hasMetrics() {
				r.publishMetrics(runID)
			}
			r.events.Publish(Event{
				RunID:   runID,
				Type:    EventRunCancelled,
				Source:  SourceRef{Kind: "runtime"},
				Payload: RunResult{Error: "runtime closed"},
			})
			close(cancelDone)
		} else if cancelDone != nil {
			<-cancelDone
		}
		if runDone != nil {
			<-runDone
		}
		if !active {
			r.approvals.Close()
			r.questions.Close()
		}
		if r.connectors != nil {
			if err := r.connectors.Close(); err != nil {
				errs = append(errs, err)
			}
		}
		if r.services.Close != nil {
			if err := r.services.Close(); err != nil {
				errs = append(errs, err)
			}
		}
		if r.events != nil {
			r.events.Close()
		}
		if r.recorder != nil {
			if err := r.recorder.Close(); err != nil {
				errs = append(errs, err)
			}
		}
		r.closeErr = errors.Join(errs...)
	})
	return r.closeErr
}
