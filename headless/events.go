package headless

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"
	rt "nekocode/runtime"
)

// Blocks have adapter-owned IDs and boundaries. These are synthesized content
// events, never provider-native Anthropic message events or thinking signatures.
func (s *server) partial(event map[string]any) error {
	if !s.options.IncludePartialMessages {
		return nil
	}
	return s.sendTurn("stream_event", map[string]any{"event": event, "message_id": s.active.messageID, "parent_tool_use_id": nil})
}

func (s *server) delta(kind, text string) error {
	t := s.active
	if text == "" {
		return nil
	}
	if t.kind != "" && t.kind != kind {
		if err := s.flushBlock(); err != nil {
			return err
		}
	}
	if t.kind == "" {
		t.kind = kind
		t.messageID = uuid.NewString()
		block := map[string]any{"type": kind, kind: ""}
		if err := s.partial(map[string]any{"type": "content_block_start", "index": 0, "content_block": block}); err != nil {
			return err
		}
	}
	if t.text.Len()+len(text) > maxFrameSize/2 {
		return errors.New("assistant block exceeds 4 MiB")
	}
	t.text.WriteString(text)
	return s.partial(map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": kind + "_delta", kind: text}})
}

func (s *server) assistant(id string, block map[string]any, extra map[string]any) error {
	fields := map[string]any{"parent_tool_use_id": nil, "message": map[string]any{
		"id": id, "role": "assistant", "model": s.backend.CurrentModel().Model, "content": []any{block},
	}}
	for k, v := range extra {
		fields[k] = v
	}
	return s.sendTurn("assistant", fields)
}

func (s *server) flushBlock() error {
	t := s.active
	if t.kind == "" {
		return nil
	}
	if err := s.partial(map[string]any{"type": "content_block_stop", "index": 0}); err != nil {
		return err
	}
	if err := s.assistant(t.messageID, map[string]any{"type": t.kind, t.kind: t.text.String()}, nil); err != nil {
		return err
	}
	if t.kind == "text" {
		if t.committedText.Len()+t.text.Len() > maxFrameSize/2 {
			return errors.New("assistant message exceeds 4 MiB")
		}
		t.committedText.WriteString(t.text.String())
		t.committedIDs = append(t.committedIDs, t.messageID)
		t.lastText = t.committedText.String()
	}
	t.kind = ""
	t.text.Reset()
	t.messageID = ""
	return nil
}

func (s *server) canonical(text string) error {
	if len(text) > maxFrameSize/2 {
		return errors.New("assistant block exceeds 4 MiB")
	}
	t := s.active
	if t.kind == "thinking" {
		if err := s.flushBlock(); err != nil {
			return err
		}
	}
	remaining := text
	prefix := t.committedText.String()
	if strings.HasPrefix(text, prefix) {
		remaining = strings.TrimPrefix(text, prefix)
	} else {
		// A corrected canonical message replaces previously committed previews
		// using their original IDs, so a reducer can retract the old text.
		for _, id := range t.committedIDs {
			if err := s.assistant(id, map[string]any{"type": "text", "text": ""}, nil); err != nil {
				return err
			}
		}
	}
	t.committedText.Reset()
	t.committedIDs = nil
	if t.kind == "text" {
		t.text.Reset()
		t.text.WriteString(remaining)
	} else if remaining != "" {
		if err := s.delta("text", remaining); err != nil {
			return err
		}
	}
	if err := s.flushBlock(); err != nil {
		return err
	}
	t.committedText.Reset()
	t.committedIDs = nil
	t.lastText = text
	return nil
}

func (s *server) boundary() error {
	if err := s.flushBlock(); err != nil {
		return err
	}
	s.active.committedText.Reset()
	s.active.committedIDs = nil
	return nil
}

func (s *server) event(ctx context.Context, event rt.Event) error {
	switch event.Type {
	case rt.EventRunSummary:
		if p, ok := event.Payload.(rt.RunSummary); ok {
			s.active.summary = &p
		}
		return nil
	case rt.EventSubAgentOutput:
		p, ok := event.Payload.(rt.SubAgentOutput)
		if !ok {
			return errors.New("invalid subagent output")
		}
		if p.Kind != "message" && !s.options.IncludePartialMessages {
			return nil
		}
		return s.sendTurn("subagent", map[string]any{"subagent_id": p.ID, "event": p.Kind, "text": p.Text})
	case rt.EventInputAccepted:
		return s.sendTurn("system", map[string]any{"subtype": "input_accepted", "input": event.Payload})

	case rt.EventAssistantDelta, rt.EventReasoningDelta:
		p, ok := event.Payload.(rt.DeltaPayload)
		if !ok {
			return errors.New("invalid delta payload")
		}
		kind := "text"
		if event.Type == rt.EventReasoningDelta {
			kind = "thinking"
		}
		return s.delta(kind, p.Delta)
	case rt.EventAssistantMessage:
		p, ok := event.Payload.(rt.MessagePayload)
		if !ok {
			return errors.New("invalid message payload")
		}
		return s.canonical(p.Content)
	case rt.EventSystemMessage:
		p, ok := event.Payload.(rt.MessagePayload)
		if !ok {
			return errors.New("invalid system payload")
		}
		return s.sendTurn("system", map[string]any{"subtype": "message", "message": p.Content})
	case rt.EventToolStarted, rt.EventToolBlocked, rt.EventToolCompleted:
		p, ok := event.Payload.(rt.ToolPayload)
		if !ok || p.CallID == "" {
			return errors.New("tool event requires stable call_id")
		}
		if err := s.boundary(); err != nil {
			return err
		}
		extra := map[string]any{}
		if p.SubAgentID != "" {
			extra["nekocode_subagent_id"] = p.SubAgentID
		}
		tool := s.tool(toolKey{subagent: p.SubAgentID, call: p.CallID}, event.Type != rt.EventToolCompleted)
		if !tool.started {
			if err := s.assistant(uuid.NewString(), map[string]any{"type": "tool_use", "id": tool.id, "name": p.ToolName, "input": toolInput(p)}, extra); err != nil {
				return err
			}
			tool.started = true
		}
		if event.Type == rt.EventToolStarted || tool.completed {
			return nil
		}
		tool.completed = true
		extra["parent_tool_use_id"] = nil
		extra["message"] = map[string]any{"role": "user", "content": []any{map[string]any{"type": "tool_result", "tool_use_id": tool.id, "content": p.Output, "is_error": p.IsError || event.Type == rt.EventToolBlocked}}}
		return s.sendTurn("user", extra)
	case rt.EventApprovalRequested:
		p, ok := event.Payload.(rt.ApprovalView)
		if !ok {
			return errors.New("invalid approval payload")
		}
		if err := s.boundary(); err != nil {
			return err
		}
		toolID := ""
		if p.CallID != "" {
			toolID = s.tool(toolKey{subagent: p.SubAgentID, call: p.CallID}, true).id
		}
		return s.request(ctx, pendingRequest{kind: "approval", id: p.ID, approval: &p}, map[string]any{
			"subtype": "can_use_tool", "tool_name": p.ToolName, "tool_use_id": toolID, "input": p.Args, "nekocode_subagent_id": p.SubAgentID, "nekocode_approval": p.Approval,
		})
	case rt.EventQuestionRequested:
		p, ok := event.Payload.(rt.QuestionView)
		if !ok {
			return errors.New("invalid question payload")
		}
		if err := s.boundary(); err != nil {
			return err
		}
		return s.request(ctx, pendingRequest{kind: "question", id: p.ID}, map[string]any{"subtype": "nekocode_question", "questions": p.Questions})
	case rt.EventApprovalResolved:
		p, ok := event.Payload.(rt.ApprovalView)
		if ok {
			if p.Status == rt.ApprovalRejected || p.Status == rt.ApprovalExpired {
				s.active.denials = append(s.active.denials, map[string]any{"approval_id": p.ID, "tool_name": p.ToolName, "call_id": p.CallID, "subagent_id": p.SubAgentID, "status": p.Status})
			}
			return s.resolved("approval", p.ID)
		}
	case rt.EventQuestionResolved:
		p, ok := event.Payload.(rt.QuestionView)
		if ok {
			return s.resolved("question", p.ID)
		}
	case rt.EventMetricsUpdated:
		p, ok := event.Payload.(rt.MetricsSnapshot)
		if ok {
			s.active.metrics = &p
		}
	case rt.EventSubAgentStarted, rt.EventSubAgentEnded, rt.EventTodosUpdated, rt.EventPhaseChanged, rt.EventToolPreview:
		return s.sendTurn("system", map[string]any{"subtype": "nekocode_event", "event_type": event.Type, "payload": event.Payload})
	case rt.EventSessionChanged:
		return errors.New("session switching inside a stream-json turn is unsupported; use --resume on a new connection")
	case rt.EventRunDone, rt.EventRunFailed, rt.EventRunCancelled:
		if err := s.backend.WaitRun(ctx, s.active.runID); err != nil {
			return err
		}
		if err := s.rejectPending(ctx); err != nil {
			return err
		}
		p, ok := event.Payload.(rt.RunResult)
		if !ok {
			return errors.New("invalid run result")
		}
		if p.Output != "" && (s.active.kind == "text" || s.active.committedText.Len() > 0 || p.Output != s.active.lastText) {
			if err := s.canonical(p.Output); err != nil {
				return err
			}
		}
		if err := s.boundary(); err != nil {
			return err
		}
		subtype := "success"
		if event.Type == rt.EventRunFailed {
			subtype = "error_during_execution"
		}
		if event.Type == rt.EventRunCancelled {
			subtype = "error_cancelled"
		}
		for key, tool := range s.active.tools {
			if tool.completed || !tool.started {
				continue
			}
			if err := s.sendTurn("user", map[string]any{"parent_tool_use_id": nil, "nekocode_subagent_id": key.subagent, "message": map[string]any{"role": "user", "content": []any{map[string]any{"type": "tool_result", "tool_use_id": tool.id, "content": "run ended before tool completion", "is_error": true}}}}); err != nil {
				return err
			}
		}
		if err := s.result(p, subtype); err != nil {
			return err
		}
		s.active.cancel()
		s.active = nil
	}
	return nil
}

// toolInput projects a tool call's structured arguments. Some producers only
// carry the human-readable Args summary, which is not required to be JSON, and a
// call may legitimately have no arguments. Neither case may fail the stream: the
// runtime has already executed the call, so project an empty object rather than
// tearing down the connection mid-turn.
func toolInput(p rt.ToolPayload) any {
	raw := p.Input
	if len(raw) == 0 {
		raw = json.RawMessage(p.Args)
	}
	var input map[string]json.RawMessage
	if len(raw) == 0 || json.Unmarshal(raw, &input) != nil || input == nil {
		return json.RawMessage(`{}`)
	}
	return json.RawMessage(bytes.Clone(raw))
}

type toolKey struct{ subagent, call string }
type toolState struct {
	id                 string
	started, completed bool
}

func (s *server) tool(key toolKey, newInvocation bool) *toolState {
	tool := s.active.tools[key]
	if tool == nil || (newInvocation && tool.completed) {
		id := key.call
		if s.active.wireIDs[id] {
			id = uuid.NewString()
		}
		s.active.wireIDs[id] = true
		tool = &toolState{id: id}
		s.active.tools[key] = tool
	}
	return tool
}
