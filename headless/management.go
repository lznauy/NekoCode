package headless

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	rt "nekocode/runtime"
)

func (s *server) idle() error {
	if s.active != nil || len(s.queue) != 0 {
		return errors.New("busy: wait for the active turn and input queue to settle")
	}
	if s.shuttingDown {
		return errors.New("connection is shutting down")
	}
	return nil
}

func (s *server) manage(ctx context.Context, f frame, method string) error {
	var request struct {
		Model        string `json:"model"`
		SessionID    string `json:"session_id"`
		CheckpointID string `json:"checkpoint_id"`
		FullAccess   *bool  `json:"full_access"`
		Text         string `json:"text"`
		Offset       int    `json:"offset"`
		Limit        int    `json:"limit"`
		Mode         string `json:"mode"`
		State        string `json:"state"`
		Source       string `json:"source"`
		RunID        string `json:"run_id"`
	}
	if err := json.Unmarshal(f.Request, &request); err != nil {
		return s.respond(f.RequestID, nil, errors.New("invalid request fields"))
	}
	if method == "server.info" {
		return s.respond(f.RequestID, s.metadata(ctx), nil)
	}
	if method == "shutdown" {
		if request.Mode != "" && request.Mode != "drain" && request.Mode != "cancel" {
			return s.respond(f.RequestID, nil, errors.New("shutdown mode must be drain or cancel"))
		}
		cancelled := []string{}
		if request.Mode == "cancel" {
			if s.active != nil {
				if err := s.backend.CancelRun(ctx, s.active.runID); err != nil {
					return s.respond(f.RequestID, nil, err)
				}
			}
			cancelled = s.clearQueue()
			s.cancelInteractions = true
			if err := s.rejectPending(ctx); err != nil {
				return err
			}
		}
		s.shuttingDown = true
		return s.respond(f.RequestID, map[string]any{"state": "shutting_down", "cancelled_message_uuids": cancelled}, nil)
	}
	m, ok := s.backend.(Management)
	if !ok {
		return s.respond(f.RequestID, nil, errors.New("backend does not support management"))
	}
	if !slices.Contains(s.capabilities(), method) {
		return s.respond(f.RequestID, nil, fmt.Errorf("unsupported method %q", method))
	}
	// Observation and context reads are read-only and safe during an active run:
	// hosts poll them to render live state without waiting for the turn to end.
	// They also carry the run binding, which is why they answer here rather
	// than through the state-mutating commands below.
	switch method {
	case "context.snapshot":
		body := map[string]any{"session_id": s.sessionID, "context": m.ContextSnapshot()}
		if s.active != nil {
			body["run_id"] = s.active.runID
			body["user_message_uuid"] = s.active.input.id
		}
		return s.respond(f.RequestID, body, nil)
	case "observe.overview":
		return s.respond(f.RequestID, m.ObservedOverview(), nil)
	case "observe.runs":
		return s.respond(f.RequestID, map[string]any{
			"runs": m.ObservedRuns(rt.ObservedRunFilter{
				Limit: request.Limit, State: rt.ObservedRunState(request.State),
				Source: request.Source,
			}),
		}, nil)
	case "observe.run":
		if request.RunID == "" {
			return s.respond(f.RequestID, nil, errors.New("run_id is required"))
		}
		detail, ok := m.ObservedRun(rt.RunID(request.RunID))
		if !ok {
			return s.respond(f.RequestID, nil, errors.New("run not found"))
		}
		return s.respond(f.RequestID, detail, nil)
	}
	var result any
	var err error
	switch method {
	case "models.list":
		models, active := m.ModelOptions()
		result = map[string]any{"models": models, "active": active}
	case "sessions.list":
		result = map[string]any{"sessions": m.ListSessions()}
	case "extensions.list":
		result = extensionSummary(m.SkillManagementView())
	case "run.steer":
		if s.active == nil {
			err = errors.New("no active turn")
		} else if s.shuttingDown {
			err = errors.New("connection is shutting down")
		} else if strings.TrimSpace(request.Text) == "" {
			err = errors.New("text is required")
		} else {
			err = m.SteerRun(ctx, s.active.runID, rt.Input{Source: rt.SourceRef{Kind: "headless", ID: f.RequestID}, Text: request.Text})
			result = map[string]any{"run_id": s.active.runID, "accepted": err == nil}
		}
	default:
		if err = s.idle(); err != nil {
			return s.respond(f.RequestID, nil, err)
		}
		switch method {
		case "model.set":
			if strings.TrimSpace(request.Model) == "" {
				err = errors.New("model is required")
			} else {
				result, err = m.SwitchSessionModel(request.Model)
			}
		case "permissions.set":
			if request.FullAccess == nil {
				err = errors.New("full_access boolean is required")
			} else {
				err = m.SetFullAccess(*request.FullAccess)
				result = map[string]any{"permission_mode": s.backend.PermissionMode()}
			}
		case "session.history":
			if request.SessionID != "" && request.SessionID != s.sessionID {
				err = errors.New("resume the requested session before reading its history")
			} else {
				limit := request.Limit
				if limit == 0 {
					limit = 100
				}
				var page rt.SessionMessagePage
				if request.Offset < 0 || limit < 1 || limit > 1000 {
					err = errors.New("offset must be within history; limit must be 1..1000")
				} else if paged, ok := s.backend.(interface {
					SessionMessagesPage(int, int) (rt.SessionMessagePage, error)
				}); ok {
					page, err = paged.SessionMessagesPage(request.Offset, limit)
				} else {
					messages := m.SessionMessages()
					if request.Offset > len(messages) {
						err = errors.New("offset must be within history; limit must be 1..1000")
					} else {
						end := request.Offset + min(limit, len(messages)-request.Offset)
						page = rt.SessionMessagePage{Messages: messages[request.Offset:end], NextOffset: end, HasMore: end < len(messages), Total: len(messages)}
					}
				}
				if err == nil {
					result = map[string]any{"session_id": s.sessionID, "messages": page.Messages, "next_offset": page.NextOffset, "has_more": page.HasMore, "total": page.Total}
				}
			}
		case "session.new":
			result, err = s.backend.NewSession()
		case "session.resume":
			if request.SessionID == "" {
				err = errors.New("session_id is required")
			} else {
				err = s.backend.ResumeSession(request.SessionID)
				result = map[string]any{"session_id": s.backend.CurrentSessionID()}
			}
		case "session.delete":
			if request.SessionID == "" || request.SessionID == s.sessionID {
				err = errors.New("a non-current session_id is required")
			} else {
				err = m.DeleteSession(request.SessionID)
				result = map[string]any{"deleted": request.SessionID}
			}
		case "workspace.checkpoints":
			var points []rt.CheckpointInfo
			points, err = m.Checkpoints()
			if points == nil {
				points = []rt.CheckpointInfo{}
			}
			result = map[string]any{"session_id": s.sessionID, "checkpoints": points}
		case "workspace.rewind":
			if request.CheckpointID == "" {
				err = errors.New("checkpoint_id is required")
			} else {
				var message string
				message, err = m.Rewind(request.CheckpointID)
				result = map[string]any{"message": message, "workspace": m.WorkspaceChanges()}
			}
		default:
			err = fmt.Errorf("unsupported method %q", method)
		}
		if (method == "session.new" || method == "session.resume") && (err == nil || s.backend.CurrentSessionID() != s.sessionID) {
			s.sessionID = s.backend.CurrentSessionID()
			if s.sessionID == "" {
				return errors.New("backend lost session binding")
			}
			if sendErr := s.send("system", map[string]any{"subtype": "session_changed", "session_id": s.sessionID}); sendErr != nil {
				return sendErr
			}
		}
	}
	return s.respond(f.RequestID, result, err)
}
