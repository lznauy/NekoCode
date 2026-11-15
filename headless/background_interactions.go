package headless

import (
	"context"
	"errors"
	"time"

	rt "nekocode/runtime"
	"nekocode/util/version"
)

// Background workers outlive the foreground turn. Route their interactions
// independently, after checking the task belongs to this connection's session.
func (s *server) backgroundInteraction(ctx context.Context, event rt.Event) error {
	switch event.Type {
	case rt.EventRunStarted:
		if s.active == nil && s.ownsCoordinatorRun(event) {
			s.active = &turn{input: userInput{id: string(event.RunID)}, runID: event.RunID, started: event.Time,
				cancel: func() { _ = s.backend.CancelRun(context.Background(), event.RunID) }, tools: make(map[toolKey]*toolState), wireIDs: make(map[string]bool)}
			if s.active.started.IsZero() {
				s.active.started = time.Now()
			}
			return s.sendTurn("system", map[string]any{"subtype": "init", "protocol_version": ProtocolVersion,
				"nekocode_version": version.Version, "cwd": s.cwd, "model": s.backend.CurrentModel().Model, "permissionMode": s.backend.PermissionMode(),
				"capabilities": s.capabilities(), "server": s.metadata(ctx), "nekocode_source": event.Source})
		}
	case rt.EventApprovalResolved:
		if p, ok := event.Payload.(rt.ApprovalView); ok {
			return s.resolved("approval", p.ID)
		}
	case rt.EventQuestionResolved:
		if p, ok := event.Payload.(rt.QuestionView); ok {
			return s.resolved("question", p.ID)
		}
	case rt.EventApprovalRequested:
		p, ok := event.Payload.(rt.ApprovalView)
		if !ok || !s.ownsBackgroundTask(p.SubAgentID) {
			return nil
		}
		return s.request(ctx, pendingRequest{runID: event.RunID, kind: "approval", id: p.ID, approval: &p}, map[string]any{
			"subtype": "can_use_tool", "tool_name": p.ToolName, "tool_use_id": p.CallID, "input": p.Args,
			"nekocode_subagent_id": p.SubAgentID, "nekocode_approval": p.Approval, "nekocode_run_id": event.RunID,
		})
	case rt.EventQuestionRequested:
		p, ok := event.Payload.(rt.QuestionView)
		if !ok || !s.ownsBackgroundTask(p.SubAgentID) {
			return nil
		}
		return s.request(ctx, pendingRequest{runID: event.RunID, kind: "question", id: p.ID}, map[string]any{
			"subtype": "nekocode_question", "questions": p.Questions, "nekocode_subagent_id": p.SubAgentID, "nekocode_run_id": event.RunID,
		})
	}
	return nil
}

// Automatic review runs are root actors. Bind only host-recorded task-completion
// runs whose plan belongs to this connection's session, then use ordinary turn
// routing for their output, controls, cancellation and final result.
func (s *server) ownsCoordinatorRun(event rt.Event) bool {
	if event.Source.Kind != "task_completion" || event.RunID == "" || s.backend.CurrentSessionID() != s.sessionID {
		return false
	}
	reader, ok := s.backend.(interface {
		LookupRun(rt.RunID) (rt.RunSnapshot, bool)
	})
	if !ok {
		return false
	}
	run, ok := reader.LookupRun(event.RunID)
	if !ok || run.ID != event.RunID || run.SessionID != s.sessionID || run.Source != event.Source {
		return false
	}
	source, ok := s.backend.(interface {
		BackgroundTasks(string) ([]rt.BackgroundTask, error)
	})
	if !ok {
		return false
	}
	tasks, err := source.BackgroundTasks(s.sessionID)
	if err != nil {
		return false
	}
	for _, task := range tasks {
		if task.SessionID == s.sessionID && task.GroupID == event.Source.ID {
			return true
		}
	}
	return false
}

func (s *server) ownsBackgroundTask(id string) bool {
	if id == "" || s.backend.CurrentSessionID() != s.sessionID {
		return false
	}
	source, ok := s.backend.(interface {
		BackgroundTasks(string) ([]rt.BackgroundTask, error)
	})
	if !ok {
		return false
	}
	tasks, err := source.BackgroundTasks(s.sessionID)
	if err != nil {
		return false
	}
	for _, task := range tasks {
		if task.ID == id && task.SessionID == s.sessionID {
			return true
		}
	}
	return false
}

// A stream has one session binding. Keep it until its workers settle so future
// interaction events cannot be orphaned by switching away before they arrive.
func (s *server) backgroundSessionBusy() error {
	if len(s.pending) > 0 {
		return errors.New("answer pending interactions before changing sessions")
	}
	source, ok := s.backend.(interface {
		BackgroundTasks(string) ([]rt.BackgroundTask, error)
	})
	if !ok {
		return nil
	}
	tasks, err := source.BackgroundTasks(s.sessionID)
	if err != nil {
		return err
	}
	for _, task := range tasks {
		switch task.Status {
		case "queued", "running", "waiting_dependencies", "waiting_approval", "waiting_question":
			return errors.New("wait for background tasks to settle or cancel them before changing sessions")
		}
	}
	return nil
}
