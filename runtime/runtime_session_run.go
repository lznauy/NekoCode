package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"nekocode/mode"
)

type sessionRunKey struct{}

// Session-scoped protocol runs finish before restoring the foreground session.
// Super Agent tasks can outlive that boundary, so they need a separate protocol
// lifecycle before they can safely be admitted here.
func validateSessionRunMode(ctx context.Context, id mode.ID) error {
	if ctx.Value(sessionRunKey{}) != nil && id == mode.SuperAgent {
		return protocolError(ErrorUnsupported, "start_run_in_session", "A2A session runs do not support Super Agent mode")
	}
	return nil
}

func sessionModeRecoveryCommand(input string) bool {
	fields := strings.Fields(input)
	return len(fields) == 2 && fields[0] == "/mode" && (fields[1] == string(mode.Standard) || fields[1] == string(mode.Plan))
}

// validateSessionRunInput rejects invalid requests before selecting a session.
func validateSessionRunInput(ctx context.Context, input Input) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(input.Text) == "" {
		return protocolError(ErrorInvalidInput, "start_run_in_session", "empty input")
	}
	if len(input.Images) > MaxImageAttachments {
		return protocolError(ErrorInvalidInput, "start_run_in_session", "too many image attachments")
	}
	fields := strings.Fields(input.Text)
	switch strings.ToLower(fields[0]) {
	case "/new", "/fork":
		return protocolError(ErrorUnsupported, "start_run_in_session", "A2A sessions are managed by contextId; start a new context instead")
	case "/sessions":
		if len(fields) > 1 {
			return protocolError(ErrorUnsupported, "start_run_in_session", "A2A cannot select another session; use contextId")
		}
	case "/mode":
		if len(fields) == 2 && fields[1] == string(mode.SuperAgent) {
			return protocolError(ErrorUnsupported, "start_run_in_session", "A2A session runs do not support Super Agent mode")
		}
	}
	return nil
}

// StartRunInSession atomically selects or creates a session and starts a run.
// The previously active session is restored before the runtime becomes idle.
func (r *Runtime) StartRunInSession(ctx context.Context, sessionID string, input Input) (RunID, string, error) {
	if err := r.mutationMu.LockContext(ctx); err != nil {
		return "", "", err
	}
	defer r.mutationMu.Unlock()
	if err := validateSessionRunInput(ctx, input); err != nil {
		return "", "", err
	}
	if r.services.Sessions.CurrentSessionID == nil || r.services.Sessions.ResumeSession == nil || r.services.Sessions.NewSession == nil {
		return "", "", protocolError(ErrorUnsupported, "start_run_in_session", "session capability unavailable")
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return "", "", protocolError(ErrorClosed, "start_run_in_session", "closed")
	}
	if r.mutating || r.status != RunIdle || r.sessionRestoreFailed {
		runID, status := r.currentRun, r.status
		restoreFailed := r.sessionRestoreFailed
		r.mu.Unlock()
		if restoreFailed {
			return runID, "", protocolError(ErrorConflict, "start_run_in_session", "previous session restoration failed")
		}
		return runID, "", protocolError(ErrorBusy, "start_run_in_session", fmt.Sprintf("run %s is %s", runID, status))
	}
	r.mu.Unlock()

	if r.background.busy() {
		return "", "", protocolError(ErrorBusy, "start_run_in_session", "background tasks are active")
	}

	previousSessionID := r.services.Sessions.CurrentSessionID()
	foregroundDraft, err := r.captureSessionDraft()
	if err != nil {
		return "", "", err
	}
	restore := func() (bool, error) {
		if foregroundDraft != nil {
			err := foregroundDraft()
			r.mu.Lock()
			r.sessionRestoreFailed = err != nil && !mutationCommitted(err)
			r.mu.Unlock()
			return false, err
		}
		return r.restoreSessionOrFallback(previousSessionID)
	}
	selectedSessionID := sessionID
	if selectedSessionID == "" {
		session, selectErr := r.services.Sessions.NewSession()
		selectedSessionID = session.ID
		err = selectErr
	} else if selectedSessionID != previousSessionID {
		r.mu.Lock()
		draft := r.sessionDrafts[selectedSessionID]
		r.mu.Unlock()
		if draft != nil {
			err = draft()
		} else {
			err = r.services.Sessions.ResumeSession(selectedSessionID)
		}
	}
	if err != nil {
		_, recoveryErr := restore()
		return "", selectedSessionID, errors.Join(err, recoveryErr)
	}
	finish := func() (bool, error) {
		draft, captureErr := r.captureSessionDraft()
		r.mu.Lock()
		if draft == nil {
			delete(r.sessionDrafts, selectedSessionID)
		} else {
			if r.sessionDrafts == nil {
				r.sessionDrafts = make(map[string]func() error)
			}
			r.sessionDrafts[selectedSessionID] = draft
		}
		r.mu.Unlock()
		if previousSessionID == selectedSessionID {
			return false, captureErr
		}
		fallback, restoreErr := restore()
		return fallback, errors.Join(captureErr, restoreErr)
	}
	runID, err := r.startRunLocked(context.WithValue(ctx, sessionRunKey{}, true), input, finish)
	if err != nil && previousSessionID != selectedSessionID {
		_, recoveryErr := restore()
		err = errors.Join(err, recoveryErr)
	}
	return runID, selectedSessionID, err
}

func (r *Runtime) captureSessionDraft() (func() error, error) {
	if r.services.Sessions.CaptureDraft == nil {
		return nil, nil
	}
	return r.services.Sessions.CaptureDraft()
}

// restoreSessionOrFallback restores the requested session, falling back to a
// fresh session when restoration is no longer possible. The bool reports that
// fallback so callers can surface the session change when appropriate.
func (r *Runtime) restoreSessionOrFallback(previousSessionID string) (bool, error) {
	var restoreErr error
	if previousSessionID == "" {
		_, restoreErr = r.services.Sessions.NewSession()
	} else {
		restoreErr = r.services.Sessions.ResumeSession(previousSessionID)
	}
	fellBack := false
	restored := restoreErr == nil || mutationCommitted(restoreErr)
	if !restored {
		_, fallbackErr := r.services.Sessions.NewSession()
		restored = fallbackErr == nil || mutationCommitted(fallbackErr)
		if !restored {
			restoreErr = errors.Join(restoreErr, fallbackErr)
		} else {
			restoreErr = fallbackErr
			fellBack = true
		}
	}
	r.mu.Lock()
	r.sessionRestoreFailed = !restored
	r.mu.Unlock()
	return fellBack, restoreErr
}

// DeleteInactiveSession serializes retention cleanup with all session changes.
func (r *Runtime) DeleteInactiveSession(id string) error {
	return r.mutation("delete_inactive_session", r.services.Sessions.DeleteSession != nil && r.services.Sessions.CurrentSessionID != nil, func() error {
		if r.services.Sessions.CurrentSessionID() == id {
			return protocolError(ErrorConflict, "delete_inactive_session", "session is currently active")
		}
		err := r.services.Sessions.DeleteSession(id)
		if err == nil || mutationCommitted(err) {
			r.mu.Lock()
			delete(r.sessionDrafts, id)
			r.mu.Unlock()
		}
		return err
	})
}
