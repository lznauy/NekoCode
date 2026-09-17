package headless

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"nekocode/protocol"
	rt "nekocode/runtime"
)

func TestReviewToolInputPreservesLargeInteger(t *testing.T) {
	h := newHarness(t, func(_ context.Context, _ string, host rt.RunHost) (string, error) {
		host.Step(protocol.StepEvent{Action: protocol.StepActionToolStart, CallID: "large", ToolName: "read", ToolInput: json.RawMessage(`{"id":9007199254740993}`)})
		return "done", nil
	}, Options{})
	h.send(t, user("one", "run"))
	// The harness decodes JSON as float64; inspect it through a string-valued
	// json.Number test in the projection directly instead of rounding twice.
	h.until(t, "result")
	h.finish(t)
	value := toolInput(rt.ToolPayload{Input: json.RawMessage(`{"id":9007199254740993}`)})
	data, _ := json.Marshal(value)
	if string(data) != `{"id":9007199254740993}` {
		t.Fatalf("input rounded: %s", data)
	}
}

func TestReviewUnchangedLargeIntegerApproval(t *testing.T) {
	h := newHarness(t, func(_ context.Context, _ string, host rt.RunHost) (string, error) {
		if host.Confirm(rt.ConfirmRequest{ToolName: "read", Args: map[string]any{"id": int64(9007199254740993)}}).Allowed {
			return "allowed", nil
		}
		return "denied", nil
	}, Options{})
	h.send(t, user("one", "run"))
	request := h.until(t, "control_request")
	h.send(t, reply(request["request_id"], map[string]any{"behavior": "allow", "updatedInput": json.RawMessage(`{"id":9007199254740993}`)}))
	if f := h.until(t, "result"); f["result"] != "allowed" {
		t.Fatal(f)
	}
	h.finish(t)
}

func TestReviewInitializeAfterCompletedTurnRejected(t *testing.T) {
	h := newHarness(t, func(context.Context, string, rt.RunHost) (string, error) { return "done", nil }, Options{})
	h.send(t, user("one", "run"))
	h.until(t, "result")
	h.send(t, control("late", "initialize"))
	if f := h.until(t, "control_response")["response"].(map[string]any); f["subtype"] != "error" {
		t.Fatal(f)
	}
	h.finish(t)
}

func TestReviewInterleavedTextCanonicalDoesNotDuplicate(t *testing.T) {
	h := newHarness(t, func(_ context.Context, _ string, host rt.RunHost) (string, error) {
		host.Text("A")
		host.Reason("think")
		host.Text("B")
		host.Step(protocol.StepEvent{Action: protocol.StepActionChat, Output: "AB"})
		return "AB", nil
	}, Options{})
	h.send(t, user("one", "run"))
	text := ""
	for {
		f := h.next(t)
		if f["type"] == "result" {
			break
		}
		if f["type"] == "assistant" {
			b := f["message"].(map[string]any)["content"].([]any)[0].(map[string]any)
			if b["type"] == "text" {
				text += b["text"].(string)
			}
		}
	}
	if text != "AB" {
		t.Fatalf("text=%q", text)
	}
	h.finish(t)
}

func TestReviewReusedToolIDsRemainPaired(t *testing.T) {
	h := newHarness(t, func(_ context.Context, _ string, host rt.RunHost) (string, error) {
		for _, sub := range []string{"", "", "sub-a", "sub-b"} {
			host.Step(protocol.StepEvent{Action: protocol.StepActionToolStart, CallID: "same", SubAgentID: sub, ToolName: "read"})
			host.Step(protocol.StepEvent{Action: protocol.StepActionExecuteTool, CallID: "same", SubAgentID: sub, Output: "ok"})
		}
		return "done", nil
	}, Options{})
	h.send(t, user("one", "run"))
	uses := map[string]bool{}
	results := 0
	for {
		f := h.next(t)
		if f["type"] == "result" {
			break
		}
		if f["type"] == "assistant" {
			b := f["message"].(map[string]any)["content"].([]any)[0].(map[string]any)
			if b["type"] == "tool_use" {
				id := b["id"].(string)
				if uses[id] {
					t.Fatalf("duplicate wire ID %s", id)
				}
				uses[id] = true
			}
		}
		if f["type"] == "user" {
			b := f["message"].(map[string]any)["content"].([]any)[0].(map[string]any)
			if !uses[b["tool_use_id"].(string)] {
				t.Fatal(f)
			}
			results++
		}
	}
	if len(uses) != 4 || results != 4 {
		t.Fatalf("uses=%d results=%d", len(uses), results)
	}
	h.finish(t)
}

func TestReviewSubagentPermissionUsesScopedWireID(t *testing.T) {
	h := newHarness(t, func(_ context.Context, _ string, host rt.RunHost) (string, error) {
		for _, id := range []string{"sub-a", "sub-b"} {
			host.Step(protocol.StepEvent{Action: protocol.StepActionToolStart, CallID: "same", SubAgentID: id, ToolName: "read"})
		}
		for _, id := range []string{"sub-a", "sub-b"} {
			if !host.Confirm(rt.ConfirmRequest{ToolName: "read", CallID: "same", SubAgentID: id}).Allowed {
				return "denied", nil
			}
			host.Step(protocol.StepEvent{Action: protocol.StepActionExecuteTool, CallID: "same", SubAgentID: id, Output: "ok"})
		}
		return "done", nil
	}, Options{})
	h.send(t, user("one", "run"))
	ids := map[string]string{}
	for len(ids) < 2 {
		f := h.until(t, "assistant")
		ids[f["nekocode_subagent_id"].(string)] = f["message"].(map[string]any)["content"].([]any)[0].(map[string]any)["id"].(string)
	}
	if ids["sub-a"] == ids["sub-b"] {
		t.Fatal(ids)
	}
	for _, id := range []string{"sub-a", "sub-b"} {
		request := h.until(t, "control_request")
		if request["request"].(map[string]any)["tool_use_id"] != ids[id] {
			t.Fatal(request)
		}
		h.send(t, reply(request["request_id"], map[string]any{"behavior": "allow"}))
	}
	if f := h.until(t, "result"); f["result"] != "done" {
		t.Fatal(f)
	}
	h.finish(t)
}

func TestReviewCorrectedCanonicalReplacesEarlierText(t *testing.T) {
	h := newHarness(t, func(_ context.Context, _ string, host rt.RunHost) (string, error) {
		host.Text("old")
		host.Reason("think")
		host.Text("tail")
		host.Step(protocol.StepEvent{Action: protocol.StepActionChat, Output: "correct"})
		return "correct", nil
	}, Options{})
	h.send(t, user("one", "run"))
	texts := map[string]string{}
	order := []string{}
	for {
		f := h.next(t)
		if f["type"] == "result" {
			break
		}
		if f["type"] == "assistant" {
			m := f["message"].(map[string]any)
			b := m["content"].([]any)[0].(map[string]any)
			if b["type"] == "text" {
				id := m["id"].(string)
				if _, ok := texts[id]; !ok {
					order = append(order, id)
				}
				texts[id] = b["text"].(string)
			}
		}
	}
	text := ""
	for _, id := range order {
		text += texts[id]
	}
	if text != "correct" {
		t.Fatalf("canonical=%q", text)
	}
	h.finish(t)
}

// A queued input must not inherit the running turn's user_message_uuid; its
// documented correlation key is accepted_uuid.
func TestReviewQueuedInputIsNotAttributedToTheRunningTurn(t *testing.T) {
	h := newHarness(t, func(ctx context.Context, _ string, _ rt.RunHost) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	}, Options{})
	h.send(t, user("active", "run"))
	h.until(t, "system")
	h.send(t, user("queued", "later"))
	for {
		f := h.next(t)
		if f["subtype"] != "input_queued" {
			continue
		}
		if f["accepted_uuid"] != "queued" {
			t.Fatalf("accepted_uuid = %v", f["accepted_uuid"])
		}
		if id, ok := f["user_message_uuid"]; ok {
			t.Fatalf("queued input attributed to running turn: %v", id)
		}
		break
	}
	request := control("stop", "interrupt")
	request["request"].(map[string]any)["cancel_queued"] = true
	h.send(t, request)
	h.until(t, "result")
	h.finish(t)
}

// A tool event that only carries the human-readable Args summary must project an
// empty input object rather than fail the connection.
func TestReviewToolArgsWithoutStructuredInputStaysConnected(t *testing.T) {
	h := newHarness(t, func(_ context.Context, _ string, host rt.RunHost) (string, error) {
		host.Step(protocol.StepEvent{Action: protocol.StepActionToolStart, CallID: "t1", ToolName: "read", ToolArgs: "path=a.go"})
		host.Step(protocol.StepEvent{Action: protocol.StepActionExecuteTool, CallID: "t1", ToolName: "read", ToolArgs: "path=a.go", Output: "ok"})
		return "done", nil
	}, Options{})
	h.send(t, user("one", "run"))
	block := h.until(t, "assistant")["message"].(map[string]any)["content"].([]any)[0].(map[string]any)
	input, ok := block["input"].(map[string]any)
	if block["type"] != "tool_use" || !ok || len(input) != 0 {
		t.Fatalf("tool_use = %v", block)
	}
	result := h.until(t, "user")["message"].(map[string]any)["content"].([]any)[0].(map[string]any)
	if result["tool_use_id"] != block["id"] || result["content"] != "ok" {
		t.Fatalf("tool_result = %v", result)
	}
	if f := h.until(t, "result"); f["subtype"] != "success" || f["result"] != "done" {
		t.Fatalf("result = %v", f)
	}
	h.finish(t)
}

func TestReviewInitCarriesAllocatedRunID(t *testing.T) {
	h := newHarness(t, func(context.Context, string, rt.RunHost) (string, error) { return "done", nil }, Options{})
	h.send(t, user("one", "run"))
	var init map[string]any
	for {
		f := h.next(t)
		if f["type"] == "system" && f["subtype"] == "init" {
			init = f
		}
		if f["type"] == "result" {
			if init == nil || init["run_id"] == "" || init["run_id"] != f["run_id"] || init["user_message_uuid"] != "one" {
				t.Fatalf("init=%v result=%v", init, f)
			}
			break
		}
	}
	h.finish(t)
}

// A requested event can already be queued when shutdown/cancel rejects the
// broker. Processing that event afterward must not reopen a host prompt.
func TestReviewCancelShutdownRejectsBufferedInteractions(t *testing.T) {
	for _, mode := range []string{"drain", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			backend := rt.New(rt.RunnerFunc(func(context.Context, string, rt.RunHost) (string, error) { return "", nil }), rt.Services{})
			defer backend.Close()
			s := &server{backend: backend, conn: &transport{ctx: ctx, writes: make(chan writeItem, 8)}, pending: make(map[string]pendingRequest)}
			request, _ := json.Marshal(map[string]any{"subtype": "shutdown", "mode": mode})
			if err := s.manage(ctx, frame{RequestID: "close", Request: request}, "shutdown"); err != nil {
				t.Fatal(err)
			}
			if err := s.request(ctx, pendingRequest{kind: "question", id: "buffered"}, map[string]any{"subtype": "nekocode_question"}); err != nil {
				t.Fatal(err)
			}
			want := 0
			if mode == "drain" {
				want = 1
			}
			if len(s.pending) != want {
				t.Fatalf("pending=%d, want %d", len(s.pending), want)
			}
		})
	}
}

type rejectingCancelBackend struct{ Backend }

func (rejectingCancelBackend) CancelRun(context.Context, rt.RunID) error {
	return errors.New("cancel rejected")
}

func TestReviewFailedInterruptPreservesQueue(t *testing.T) {
	ctx := context.Background()
	s := &server{
		backend: rejectingCancelBackend{},
		conn:    &transport{ctx: ctx, writes: make(chan writeItem, 8)},
		active:  &turn{runID: "active"},
		queue:   []userInput{{id: "queued", text: "next"}}, queueBytes: 4,
	}
	request := json.RawMessage(`{"subtype":"interrupt","cancel_queued":true}`)
	if err := s.control(ctx, frame{RequestID: "stop", Request: request}); err != nil {
		t.Fatal(err)
	}
	if len(s.queue) != 1 || s.queue[0].id != "queued" || s.queueBytes != 4 {
		t.Fatalf("failed cancellation lost queued input: queue=%v bytes=%d", s.queue, s.queueBytes)
	}
}

func TestInvalidUserPreservesBodyError(t *testing.T) {
	h := newHarness(t, func(context.Context, string, rt.RunHost) (string, error) {
		t.Error("invalid input ran")
		return "", nil
	}, Options{})
	f := user("invalid", "")
	f["session_id"] = "wrong-session"
	h.send(t, f)
	if got := h.until(t, "system"); got["error"] != "empty user message" {
		t.Fatal(got)
	}
	h.finish(t)
}

func TestApprovalEchoComparesOnlyToolInput(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		allowed     bool
	}{
		{"full_echo", `{"id":9007199254740993,"_id":"public","_preview":"diff"}`, true},
		{"tool_input_echo", `{"id":9007199254740993,"_id":"public"}`, true},
		{"different_display", `{"id":9007199254740993,"_id":"public","_preview":"another display"}`, true},
		{"changed_real_arg", `{"id":9007199254740992,"_id":"public"}`, false},
		{"changed_underscore_arg", `{"id":9007199254740993,"_id":"changed"}`, false},
		{"array", `[]`, false},
		{"null", `null`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, func(_ context.Context, _ string, host rt.RunHost) (string, error) {
				args := map[string]any{"id": int64(9007199254740993), "_id": "public", "_preview": "diff"}
				if host.Confirm(rt.ConfirmRequest{ToolName: "read", Args: args}).Allowed {
					return "allowed", nil
				}
				return "denied", nil
			}, Options{})
			h.send(t, user("one", "run"))
			req := h.until(t, "control_request")
			h.send(t, reply(req["request_id"], map[string]any{"behavior": "allow", "updatedInput": json.RawMessage(tc.input)}))
			result := h.until(t, "result")
			want := "denied"
			if tc.allowed {
				want = "allowed"
			}
			if result["result"] != want {
				t.Fatal(result)
			}
			h.finish(t)
		})
	}
}
