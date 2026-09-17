package headless

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"nekocode/protocol"
	rt "nekocode/runtime"
)

// Reverse control requests bridge runtime approvals/questions to the host.
// Their lifetime is tracked by the server's pending map.
func (s *server) request(ctx context.Context, pending pendingRequest, request map[string]any) error {
	if s.eof || s.cancelInteractions {
		s.deny(ctx, pending)
		return nil
	}
	id := "nc_" + uuid.NewString()
	s.pending[id] = pending
	return s.conn.send(map[string]any{"type": "control_request", "request_id": id, "request": request})
}

func (s *server) deny(ctx context.Context, p pendingRequest) {
	// A concurrent runtime cancellation can have resolved the broker already.
	if p.kind == "approval" {
		_ = s.backend.DecideApproval(ctx, p.id, rt.ApprovalDecision{})
	} else {
		_ = s.backend.AnswerQuestion(ctx, p.id, rt.QuestionReply{Rejected: true})
	}
}

func (s *server) rejectPending(ctx context.Context) error {
	for id, p := range s.pending {
		s.deny(ctx, p)
		delete(s.pending, id)
		if err := s.conn.send(map[string]any{"type": "control_cancel_request", "request_id": id}); err != nil {
			return err
		}
	}
	return nil
}

func (s *server) resolved(kind, brokerID string) error {
	for id, p := range s.pending {
		if p.kind == kind && p.id == brokerID {
			delete(s.pending, id)
			return s.conn.send(map[string]any{"type": "control_cancel_request", "request_id": id})
		}
	}
	return nil
}

func (s *server) resolve(ctx context.Context, f frame) error {
	id := f.Response.RequestID
	p, ok := s.pending[id]
	if !ok {
		return nil
	} // duplicate or late response
	delete(s.pending, id)
	if f.Response.Subtype != "success" {
		s.deny(ctx, p)
		return nil
	}
	var err error
	if p.kind == "approval" {
		var reply struct {
			Behavior           string            `json:"behavior"`
			UpdatedInput       json.RawMessage   `json:"updatedInput"`
			UpdatedPermissions []json.RawMessage `json:"updatedPermissions"`
		}
		err = json.Unmarshal(f.Response.Response, &reply)
		if err == nil && reply.Behavior != "allow" && reply.Behavior != "deny" {
			err = errors.New("permission behavior must be allow or deny")
		}
		if err == nil && len(reply.UpdatedPermissions) > 0 {
			err = errors.New("permission rule updates are unsupported")
		}
		if err == nil && len(reply.UpdatedInput) > 0 {
			original, _ := json.Marshal(protocol.ToolInputArgs(p.approval.Args))
			var value map[string]any
			decoder := json.NewDecoder(bytes.NewReader(reply.UpdatedInput))
			decoder.UseNumber()
			if decoder.Decode(&value) != nil || value == nil {
				err = errors.New("invalid updatedInput")
			} else {
				normalized, _ := json.Marshal(protocol.ToolInputArgs(value))
				if !bytes.Equal(original, normalized) {
					err = errors.New("tool argument replacement is unsupported")
				}
			}
		}
		if err == nil {
			err = s.backend.DecideApproval(ctx, p.id, rt.ApprovalDecision{Allowed: reply.Behavior == "allow"})
		}
	} else {
		var reply rt.QuestionReply
		err = json.Unmarshal(f.Response.Response, &reply)
		if err == nil && !reply.Rejected && len(reply.Answers) == 0 {
			err = errors.New("question response requires answers or rejected=true")
		}
		if err == nil {
			err = s.backend.AnswerQuestion(ctx, p.id, reply)
		}
	}
	if err != nil {
		s.deny(ctx, p)
		return s.send("system", map[string]any{"subtype": "error", "request_id": id, "error": "control response rejected: " + err.Error()})
	}
	return nil
}
