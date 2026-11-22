package headless

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"nekocode/util/modelbudget"
)

func (s *server) respond(id string, body any, err error) error {
	response := map[string]any{"subtype": "success", "request_id": id, "response": body}
	if err != nil {
		response = map[string]any{"subtype": "error", "request_id": id, "error": err.Error()}
	}
	envelope := map[string]any{"type": "control_response", "response": response}
	data, marshalErr := json.Marshal(envelope)
	if marshalErr != nil || len(data) >= maxFrameSize {
		return s.conn.send(map[string]any{"type": "control_response", "response": map[string]any{
			"subtype": "error", "request_id": id,
			"error": "response exceeds frame limit or cannot be encoded; request a smaller page",
		}})
	}
	return s.conn.sendEncoded(data)
}

func (s *server) control(ctx context.Context, f frame) error {
	if f.RequestID == "" {
		return errors.New("control_request requires request_id")
	}
	var request struct {
		Subtype      string `json:"subtype"`
		CancelQueued bool   `json:"cancel_queued"`
	}
	if json.Unmarshal(f.Request, &request) != nil {
		return s.respond(f.RequestID, nil, errors.New("invalid control request"))
	}
	switch request.Subtype {
	case "initialize":
		if s.initialized || len(s.seen) != 0 || s.active != nil || len(s.queue) != 0 {
			return s.respond(f.RequestID, nil, errors.New("initialize must occur once, before user messages"))
		}
		var fields map[string]json.RawMessage
		_ = json.Unmarshal(f.Request, &fields)
		if raw, ok := fields["protocol_version"]; ok {
			var version string
			if json.Unmarshal(raw, &version) != nil || version != ProtocolVersion {
				return s.respond(f.RequestID, nil, fmt.Errorf("unsupported protocol_version; expected %s", ProtocolVersion))
			}
		}
		for key, value := range fields {
			switch key {
			case "hooks", "sdkMcpServers", "agents", "systemPrompt", "appendSystemPrompt", "jsonSchema", "skills", "promptSuggestions":
				var compact bytes.Buffer
				_ = json.Compact(&compact, value)
				v := compact.String()
				if v != "null" && v != "{}" && v != "[]" && v != "false" && v != `""` {
					return s.respond(f.RequestID, nil, fmt.Errorf("initialize option %s is unsupported", key))
				}
			}
		}
		shareSteps := false
		if raw, ok := fields["parallel_step_sharing"]; ok {
			if string(raw) == "null" || json.Unmarshal(raw, &shareSteps) != nil {
				return s.respond(f.RequestID, nil, errors.New("parallel_step_sharing must be a boolean"))
			}
			if shareSteps && fields["model_call_budget"] == nil {
				return s.respond(f.RequestID, nil, errors.New("parallel_step_sharing requires model_call_budget"))
			}
		}
		var externalTokens int64
		if raw, ok := fields["external_token_limit"]; ok {
			if fields["model_call_budget"] == nil || json.Unmarshal(raw, &externalTokens) != nil || externalTokens < 1 {
				return s.respond(f.RequestID, nil, errors.New("external_token_limit requires a positive integer and model_call_budget"))
			}
		}
		if raw, ok := fields["model_call_budget"]; ok {
			var limit int
			if json.Unmarshal(raw, &limit) != nil {
				return s.respond(f.RequestID, nil, errors.New("model_call_budget must be an integer"))
			}
			budget, err := modelbudget.NewWithLimits(limit, shareSteps, externalTokens)
			if err != nil {
				return s.respond(f.RequestID, nil, err)
			}
			s.callBudget = budget
		}
		s.initialized = true
		return s.respond(f.RequestID, map[string]any{"protocol_version": ProtocolVersion, "capabilities": s.capabilities(), "server": s.metadata(ctx)}, nil)
	case "interrupt":
		cancelled := []string{}
		result, err := s.backend.CancelRunWithResult(ctx, "")
		if err != nil {
			return s.respond(f.RequestID, nil, err)
		}
		if result.RunID != "" {
			if err := s.rejectRunPending(ctx, result.RunID); err != nil {
				return err
			}
		}
		if request.CancelQueued {
			var err error
			cancelled, err = s.cancelQueuedTurns(ctx)
			if err != nil {
				return s.respond(f.RequestID, nil, err)
			}
		}
		queued := []string{}
		for _, turn := range s.pendingTurns {
			queued = append(queued, turn.input.id)
		}
		for _, input := range s.queue {
			queued = append(queued, input.id)
		}
		return s.respond(f.RequestID, map[string]any{"queued_message_uuids": queued, "cancelled_message_uuids": cancelled}, nil)
	default:
		return s.manage(ctx, f, request.Subtype)
	}
}
