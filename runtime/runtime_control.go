package runtime

import (
	"context"
	"errors"
	"fmt"
	"nekocode/mode"
	"nekocode/protocol"
	"nekocode/runtime/internal/core"
	"net/url"
	"strings"

	"github.com/google/uuid"
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
	return r.startCurrentRun(ctx, input, nil)
}

// StartRunIfCurrent admits a queued message only in the session that owns it.
func (r *Runtime) StartRunIfCurrent(ctx context.Context, sessionID string, input Input) (RunID, error) {
	return r.startCurrentRun(ctx, input, &sessionID)
}

func (r *Runtime) startCurrentRun(ctx context.Context, input Input, expected *string) (RunID, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := r.mutationMu.LockContext(ctx); err != nil {
		return "", err
	}
	defer r.mutationMu.Unlock()
	if expected != nil {
		if err := r.checkQueuedSessionLocked(*expected); err != nil {
			return "", err
		}
	}
	return r.startRunLocked(ctx, input, nil, "")
}

// ExecuteLocalCommandIfCurrent keeps queued local commands inside the same
// session selection barrier as queued foreground runs.
func (r *Runtime) ExecuteLocalCommandIfCurrent(ctx context.Context, sessionID, input string) (string, LocalCommandResult, error) {
	if err := r.mutationMu.LockContext(ctx); err != nil {
		return "", LocalCommandNotCommand, err
	}
	defer r.mutationMu.Unlock()
	if err := r.checkQueuedSessionLocked(sessionID); err != nil {
		return "", LocalCommandNotCommand, err
	}
	out, status := r.ExecuteLocalCommand(ctx, input)
	return out, status, nil
}

// Caller owns mutationMu. A competing run must never absorb queued input
// through Super Agent's separate background inbox.
func (r *Runtime) checkQueuedSessionLocked(sessionID string) error {
	if sessionID != r.CurrentSessionID() {
		return protocolError(ErrorConflict, "queued_input", "queued message belongs to a different session")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return protocolError(ErrorClosed, "queued_input", "runtime closed")
	}
	if r.status != RunIdle {
		return protocolError(ErrorBusy, "queued_input", "another run is still active")
	}
	return nil
}

// startRunLocked preserves normal admission while the caller holds mutationMu.
func (r *Runtime) startRunLocked(ctx context.Context, input Input, finishSession func() (bool, error), steerTarget RunID) (RunID, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if strings.TrimSpace(input.Text) == "" {
		return "", protocolError(ErrorInvalidInput, "start_run", "empty input")
	}
	if len(input.Images) > MaxImageAttachments {
		return "", protocolError(ErrorInvalidInput, "start_run", "too many image attachments")
	}
	if ctx.Value(sessionRunKey{}) != nil && r.services.Modes.Current != nil && !sessionModeRecoveryCommand(input.Text) {
		if err := validateSessionRunMode(ctx, r.services.Modes.Current()); err != nil {
			return "", err
		}
	}
	if sessionID, ok := ctx.Value(backgroundDemandKey{}).(string); ok {
		if sessionID != r.CurrentSessionID() || r.services.Modes.Current == nil || r.services.Modes.Current() != mode.SuperAgent {
			return "", protocolError(ErrorConflict, "submit_background_demand", "session or mode changed; keep the draft and retry")
		}
	}
	if len(input.Images) > 0 && !r.ImageAttachmentsEnabled() {
		return "", protocolError(ErrorUnsupported, "start_run", "image attachments require a configured image understanding model")
	}
	sessionID := r.CurrentSessionID()
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return "", protocolError(ErrorClosed, "start_run", "closed")
	}
	if r.mutating {
		r.mu.Unlock()
		return "", protocolError(ErrorBusy, "start_run", "runtime capability mutation in progress")
	}
	if r.sessionRestoreFailed {
		r.mu.Unlock()
		return "", protocolError(ErrorConflict, "start_run", "previous session restoration failed")
	}
	if steerTarget != "" && (steerTarget != r.currentRun ||
		(r.status != RunRunning && r.status != RunWaitingApproval && r.status != RunWaitingQuestion)) {
		r.mu.Unlock()
		return "", protocolError(ErrorConflict, "steer_run", fmt.Sprintf("run %s is no longer active", steerTarget))
	}
	if r.status != RunIdle {
		runID := r.currentRun
		status := r.status
		r.mu.Unlock()
		if r.services.Modes.Current != nil && r.services.Modes.Current() == mode.SuperAgent && !protocol.CoordinationFromContext(ctx).Notification && ctx.Value(queuedRunKey{}) == nil {
			return r.queueBackgroundInput(ctx, input)
		}
		return runID, protocolError(ErrorBusy, "start_run", fmt.Sprintf("run %s is %s", runID, status))
	}
	if r.services.Modes.Current != nil && r.services.Modes.Current() == mode.Standard && !protocol.IsSlashCommandInput(input.Text) {
		r.background.mu.Lock()
		writing := len(r.background.workspaces) > 0
		r.background.mu.Unlock()
		if writing {
			r.mu.Unlock()
			return "", protocolError(ErrorBusy, "start_run", "shared workspace writer is active; use Plan/Super Agent or wait for it to finish")
		}
	}
	r.nextRun++
	runID := RunID(fmt.Sprintf("run_%d_%s", r.nextRun, uuid.NewString()))
	if queued, ok := ctx.Value(queuedRunKey{}).(RunID); ok {
		runID = queued
	}
	r.mu.Unlock()
	if input.Source.Kind == "tui" && sessionID == "" && r.services.Sessions.EnsureSessionIdentity != nil {
		sessionID = r.services.Sessions.EnsureSessionIdentity()
	}
	// Registration precedes Running: steering must not publish into the
	// previous run's recording destination while this run is starting.
	if err := r.beginRecordedRun(runID); err != nil {
		return "", err
	}
	r.invalidateContext()
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		r.releaseRecordedRun(runID)
		return "", protocolError(ErrorClosed, "start_run", "closed")
	}
	if err := ctx.Err(); err != nil {
		r.mu.Unlock()
		r.releaseRecordedRun(runID)
		return "", err
	}
	// Install the projection before exposing Running or starting preparation.
	r.runs.PrepareRun(runID, input.Source, sessionID)
	r.currentRun = runID
	r.status = RunRunning
	delete(r.cancelled, runID)
	executionCtx := ctx
	if detached, _ := ctx.Value(detachedRunKey{}).(bool); detached {
		executionCtx = context.WithoutCancel(ctx)
	}
	runCtx, cancel := context.WithCancel(core.WithRunSource(executionCtx, input.Source))
	r.runContext = runCtx
	r.cancelRun = cancel
	r.cancelDone = nil
	r.runDone = make(chan struct{})
	lease := newRunLease()
	execution := newRunExecution()
	execution.finishSession = finishSession
	execution.admissionContext = executionCtx
	r.runLease = lease
	r.runExecution = execution
	r.mu.Unlock()

	go r.run(runCtx, runID, input, lease, execution, sessionID)
	return runID, nil
}

func (r *Runtime) SteerRun(ctx context.Context, runID RunID, input Input) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !r.mutationMu.TryLock() {
		return protocolError(ErrorBusy, "steer_run", "runtime operation in progress")
	}
	super := r.services.Modes.Current != nil && r.services.Modes.Current() == mode.SuperAgent
	if super && !protocol.IsSlashCommandInput(input.Text) {
		defer r.mutationMu.Unlock()
		_, err := r.startRunLocked(ctx, input, nil, runID)
		return err
	}
	r.mutationMu.Unlock()
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
	steer := r.services.Commands.Steer
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
		// The steer chain (bot.Steer → TrySteer → drainSteering) appends the
		// message as text only: images never enter Message.Images, so they
		// cannot be delivered as native content parts even on vision models.
		// The envelope must therefore always carry the tool-proxy
		// instruction; the vision variant would claim native delivery that
		// never happened and the model would answer about images it cannot
		// see.
		steerErr = steer(steerCtx, InputWithImageAttachments(input.Text, input.Images, false))
		if steerErr == nil {
			r.events.Publish(Event{
				RunID:  runID,
				Type:   EventInputAccepted,
				Source: input.Source,
				Payload: MessagePayload{
					Steering: true,
					Role:     "user",
					Content:  RedactInputText(input.Text),
					Source:   input.Source,
					Sender:   input.Sender,
					Images:   ValidImageAttachments(input.Text, input.Images),
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
	_, err := r.CancelRunWithResult(ctx, runID)
	return err
}

func (r *Runtime) publishCancellation(control cancelControl, message string) {
	_ = control.lease.wait(context.Background())
	// A registration admitted just before deactivation can miss the first
	// rejection sweep. No registrations can arrive after the lease drains.
	r.approvals.RejectRun(control.runID)
	r.questions.RejectRun(control.runID)
	control.execution.wait()
	if control.execution.hasMetrics() {
		r.publishMetrics(control.runID)
	}
	r.publishRunTerminal(Event{
		RunID:   control.runID,
		Type:    EventRunCancelled,
		Source:  SourceRef{Kind: "runtime"},
		Payload: RunResult{Error: message, EvidenceIncomplete: control.execution.incompleteEvidence()},
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

		r.background.close()

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
			r.publishRunTerminal(Event{
				RunID:   runID,
				Type:    EventRunCancelled,
				Source:  SourceRef{Kind: "runtime"},
				Payload: RunResult{Error: "runtime closed", EvidenceIncomplete: execution.incompleteEvidence()},
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
		r.mu.Lock()
		clear(r.sessionDrafts)
		r.mu.Unlock()
		if r.connectors != nil {
			if err := r.connectors.Close(); err != nil {
				errs = append(errs, err)
			}
		}
		if r.services.Lifecycle.Close != nil {
			if err := r.services.Lifecycle.Close(); err != nil {
				errs = append(errs, err)
			}
		}
		if r.events != nil {
			r.events.Close()
		}
		if r.recorder != nil {
			if err := r.recorder.Close(); err != nil {
				errs = append(errs, protocol.MarkEvidenceIncomplete(err))
			}
		}
		r.closeErr = errors.Join(errs...)
	})
	return r.closeErr
}
