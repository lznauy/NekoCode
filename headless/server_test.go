package headless

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	toolcore "nekocode/bot/extension/tool/runtime/core"
	"nekocode/protocol"
	rt "nekocode/runtime"
)

type harness struct {
	in     *io.PipeWriter
	frames chan map[string]any
	done   chan error
	cancel context.CancelFunc
}

func newHarness(t *testing.T, runner rt.RunnerFunc, options Options) *harness {
	return newHarnessWithServices(t, runner, options, rt.Services{})
}
func newHarnessWithServices(t *testing.T, runner rt.RunnerFunc, options Options, services rt.Services) *harness {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	in, input := io.Pipe()
	output, out := io.Pipe()
	if services.NewSession == nil {
		services.NewSession = func() (rt.SessionMeta, error) { return rt.SessionMeta{ID: "session-test"}, nil }
	}
	if services.CurrentSessionID == nil {
		services.CurrentSessionID = func() string { return "session-test" }
	}
	if services.ListSessions == nil {
		services.ListSessions = func() []rt.SessionMeta { return nil }
	}
	if services.SessionMessages == nil {
		services.SessionMessages = func() []rt.DisplayMessage { return nil }
	}
	if services.CurrentModel == nil {
		services.CurrentModel = func() rt.ModelSelection { return rt.ModelSelection{Model: "test-model"} }
	}
	backend := rt.New(runner, services)
	h := &harness{in: input, frames: make(chan map[string]any, 1024), done: make(chan error, 1), cancel: cancel}
	go func() {
		defer close(h.frames)
		decoder := json.NewDecoder(output)
		for {
			var frame map[string]any
			if decoder.Decode(&frame) != nil {
				return
			}
			select {
			case h.frames <- frame:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() { h.done <- Serve(ctx, in, out, backend, "/workspace", options) }()
	t.Cleanup(func() { cancel(); _ = input.Close(); _ = output.Close(); _ = backend.Close() })
	return h
}

func (h *harness) send(t *testing.T, value any) {
	t.Helper()
	if err := json.NewEncoder(h.in).Encode(value); err != nil {
		t.Fatal(err)
	}
}

func (h *harness) next(t *testing.T) map[string]any {
	t.Helper()
	select {
	case frame, ok := <-h.frames:
		if !ok {
			t.Fatal("output closed unexpectedly")
		}
		return frame
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for output")
		return nil
	}
}

func (h *harness) until(t *testing.T, kind string) map[string]any {
	t.Helper()
	for {
		f := h.next(t)
		if f["type"] == kind {
			return f
		}
	}
}

func (h *harness) finish(t *testing.T) {
	t.Helper()
	_ = h.in.Close()
	select {
	case err := <-h.done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server did not stop")
	}
}

func user(id, text string) map[string]any {
	return map[string]any{"type": "user", "uuid": id, "message": map[string]any{"role": "user", "content": text}}
}

func control(id, subtype string) map[string]any {
	return map[string]any{"type": "control_request", "request_id": id, "request": map[string]any{"subtype": subtype}}
}

func reply(id any, body any) map[string]any {
	return map[string]any{"type": "control_response", "response": map[string]any{"subtype": "success", "request_id": id, "response": body}}
}

func TestMultipleTurnsAndCanonicalBlocks(t *testing.T) {
	h := newHarness(t, func(ctx context.Context, input string, host rt.RunHost) (string, error) {
		host.Reason("consider")
		host.Text("pre")
		host.Text(input)
		host.Step(protocol.StepEvent{Action: protocol.StepActionChat, Output: "pre" + input})
		return "pre" + input, nil
	}, Options{IncludePartialMessages: true})
	h.send(t, control("init", "initialize"))
	if f := h.until(t, "control_response"); f["response"].(map[string]any)["subtype"] != "success" {
		t.Fatal(f)
	}
	h.send(t, user("one", "first"))
	h.send(t, user("two", "second"))
	_ = h.in.Close()
	var results []string
	texts := []string{}
	starts, stops := 0, 0
	for f := range h.frames {
		switch f["type"] {
		case "assistant":
			block := f["message"].(map[string]any)["content"].([]any)[0].(map[string]any)
			if block["type"] == "text" {
				texts = append(texts, block["text"].(string))
			}
		case "stream_event":
			switch f["event"].(map[string]any)["type"] {
			case "content_block_start":
				starts++
			case "content_block_stop":
				stops++
			}
		case "result":
			if f["is_error"] != false {
				t.Fatal(f)
			}
			results = append(results, f["user_message_uuid"].(string))
		}
	}
	if strings.Join(results, ",") != "one,two" || strings.Join(texts, ",") != "prefirst,presecond" || starts != 4 || stops != starts {
		t.Fatalf("results=%v texts=%v starts=%d stops=%d", results, texts, starts, stops)
	}
	if err := <-h.done; err != nil {
		t.Fatal(err)
	}
}

func TestPermissionAndQuestionRoundTrip(t *testing.T) {
	h := newHarness(t, func(ctx context.Context, _ string, host rt.RunHost) (string, error) {
		if !host.Confirm(rt.ConfirmRequest{ToolName: "shell", CallID: "tool-1", Args: map[string]any{"command": "test"}}).Allowed {
			return "", errors.New("denied")
		}
		host.Step(protocol.StepEvent{Action: protocol.StepActionToolStart, CallID: "tool-1", ToolName: "shell", ToolArgs: `{"command":"test"}`})
		host.Step(protocol.StepEvent{Action: protocol.StepActionExecuteTool, CallID: "tool-1", ToolName: "shell", Output: "ok"})
		answer := host.Ask(rt.QuestionRequest{Questions: []rt.QuestionItem{{Question: "Continue?", Custom: true}}})
		if answer.Rejected || len(answer.Answers) != 1 || answer.Answers[0][0] != "yes" {
			return "", errors.New("bad answer")
		}
		return "done", nil
	}, Options{})
	h.send(t, user("one", "run"))
	permission := h.until(t, "control_request")
	if permission["request"].(map[string]any)["subtype"] != "can_use_tool" {
		t.Fatal(permission)
	}
	h.send(t, reply(permission["request_id"], map[string]any{"behavior": "allow", "updatedInput": map[string]any{"command": "test"}}))
	tool := h.until(t, "assistant")["message"].(map[string]any)["content"].([]any)[0].(map[string]any)
	result := h.until(t, "user")["message"].(map[string]any)["content"].([]any)[0].(map[string]any)
	if tool["id"] != result["tool_use_id"] || result["content"] != "ok" {
		t.Fatalf("tool=%v result=%v", tool, result)
	}
	question := h.until(t, "control_request")
	if question["request"].(map[string]any)["subtype"] != "nekocode_question" {
		t.Fatal(question)
	}
	h.send(t, reply(question["request_id"], map[string]any{"answers": [][]string{{"yes"}}}))
	if f := h.until(t, "result"); f["result"] != "done" || f["is_error"] != false {
		t.Fatal(f)
	}
	h.finish(t)
}

func TestEOFRejectsPendingAndFutureInteractions(t *testing.T) {
	h := newHarness(t, func(ctx context.Context, _ string, host rt.RunHost) (string, error) {
		if host.Confirm(rt.ConfirmRequest{ToolName: "shell"}).Allowed {
			return "", errors.New("unexpected allow")
		}
		if !host.Ask(rt.QuestionRequest{Questions: []rt.QuestionItem{{Question: "yes?"}}}).Rejected {
			return "", errors.New("unexpected answer")
		}
		return "denied safely", nil
	}, Options{})
	h.send(t, user("one", "run"))
	h.until(t, "control_request")
	_ = h.in.Close()
	if f := h.until(t, "result"); f["result"] != "denied safely" {
		t.Fatal(f)
	}
	if err := <-h.done; err != nil {
		t.Fatal(err)
	}
}

func TestInterruptCancelsCurrentAndQueuedTurns(t *testing.T) {
	started := make(chan string, 3)
	h := newHarness(t, func(ctx context.Context, input string, host rt.RunHost) (string, error) {
		started <- input
		if input == "first" {
			<-ctx.Done()
			return "", ctx.Err()
		}
		return input, nil
	}, Options{})
	h.send(t, user("one", "first"))
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("run not started")
	}
	h.send(t, user("two", "second"))
	request := control("stop", "interrupt")
	request["request"].(map[string]any)["cancel_queued"] = true
	h.send(t, request)
	ack := h.until(t, "control_response")["response"].(map[string]any)["response"].(map[string]any)
	if ids := ack["cancelled_message_uuids"].([]any); len(ids) != 1 || ids[0] != "two" {
		t.Fatal(ack)
	}
	if f := h.until(t, "result"); f["subtype"] != "error_cancelled" || f["user_message_uuid"] != "one" {
		t.Fatal(f)
	}
	h.send(t, user("three", "third"))
	if f := h.until(t, "result"); f["result"] != "third" {
		t.Fatal(f)
	}
	if input := <-started; input != "third" {
		t.Fatal(input)
	}
	h.finish(t)
}

func TestChangedPermissionArgumentsAreRejected(t *testing.T) {
	h := newHarness(t, func(ctx context.Context, _ string, host rt.RunHost) (string, error) {
		allowed := host.Confirm(rt.ConfirmRequest{ToolName: "shell", Args: map[string]any{"command": "safe"}}).Allowed
		if allowed {
			return "unsafe", nil
		}
		return "denied", nil
	}, Options{})
	h.send(t, user("one", "run"))
	request := h.until(t, "control_request")
	h.send(t, reply(request["request_id"], map[string]any{"behavior": "allow", "updatedInput": map[string]any{"command": "changed"}}))
	if f := h.until(t, "result"); f["result"] != "denied" {
		t.Fatal(f)
	}
	h.finish(t)
}

func TestUnknownControlAndInvalidUserDoNotRun(t *testing.T) {
	h := newHarness(t, func(context.Context, string, rt.RunHost) (string, error) { return "unexpected", nil }, Options{})
	h.send(t, control("x", "unsupported"))
	if f := h.until(t, "control_response")["response"].(map[string]any); f["subtype"] != "error" || f["request_id"] != "x" {
		t.Fatal(f)
	}
	f := user("bad", "run")
	f["message"].(map[string]any)["content"] = []any{map[string]any{"type": "image"}}
	h.send(t, f)
	if f := h.until(t, "system"); f["subtype"] != "error" {
		t.Fatal(f)
	}
	h.finish(t)
}

func TestDuplicateInputIsNotExecutedTwice(t *testing.T) {
	h := newHarness(t, func(context.Context, string, rt.RunHost) (string, error) { return "done", nil }, Options{})
	h.send(t, user("same", "run"))
	h.until(t, "result")
	h.send(t, user("same", "run"))
	if f := h.until(t, "system"); f["subtype"] != "input_duplicate" {
		t.Fatal(f)
	}
	h.finish(t)
}

func TestSinglePromptDoesNotWaitForStdin(t *testing.T) {
	h := newHarness(t, func(context.Context, string, rt.RunHost) (string, error) { return "done", nil }, Options{Prompt: "run"})
	if f := h.until(t, "result"); f["result"] != "done" {
		t.Fatal(f)
	}
	select {
	case err := <-h.done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("prompt waited for stdin")
	}
}

func TestMalformedFrameFailsConnection(t *testing.T) {
	h := newHarness(t, func(context.Context, string, rt.RunHost) (string, error) { return "", nil }, Options{})
	_, _ = io.WriteString(h.in, "{bad}\n")
	if f := h.until(t, "system"); f["fatal"] != true {
		t.Fatal(f)
	}
	if err := <-h.done; err == nil {
		t.Fatal("malformed input succeeded")
	}
}

func TestStructuredToolInputAndInterruptSettlement(t *testing.T) {
	h := newHarness(t, func(ctx context.Context, _ string, host rt.RunHost) (string, error) {
		args := map[string]any{"command": "echo hello", "nested": map[string]any{"n": 3}}
		host.Step(protocol.StepEvent{Action: protocol.StepActionToolStart, CallID: "pending", ToolName: "shell", ToolArgs: toolcore.FormatArgs(args), ToolInput: toolcore.JSONArgs(args)})
		<-ctx.Done()
		return "", ctx.Err()
	}, Options{})
	h.send(t, user("one", "run"))
	block := h.until(t, "assistant")["message"].(map[string]any)["content"].([]any)[0].(map[string]any)
	if block["input"].(map[string]any)["command"] != "echo hello" {
		t.Fatal(block)
	}
	h.send(t, control("stop", "interrupt"))
	toolResult := h.until(t, "user")["message"].(map[string]any)["content"].([]any)[0].(map[string]any)
	if toolResult["tool_use_id"] != "pending" || toolResult["is_error"] != true {
		t.Fatal(toolResult)
	}
	if f := h.until(t, "result"); f["subtype"] != "error_cancelled" {
		t.Fatal(f)
	}
	h.finish(t)
}

func TestOversizedIDRejectedBeforeRetention(t *testing.T) {
	h := newHarness(t, func(context.Context, string, rt.RunHost) (string, error) { return "unexpected", nil }, Options{})
	h.send(t, user(strings.Repeat("a", 257), "x"))
	if f := h.until(t, "system"); f["subtype"] != "error" || !strings.Contains(f["error"].(string), "256") {
		t.Fatal(f)
	}
	h.finish(t)
}

func TestDuplicateQueuedInputKeepsItsOwnID(t *testing.T) {
	h := newHarness(t, func(ctx context.Context, _ string, _ rt.RunHost) (string, error) { <-ctx.Done(); return "", ctx.Err() }, Options{})
	h.send(t, user("active", "run"))
	h.until(t, "system")
	h.send(t, user("queued", "later"))
	h.send(t, user("queued", "later"))
	for {
		f := h.next(t)
		if f["subtype"] == "input_duplicate" {
			if f["user_message_uuid"] != "queued" {
				t.Fatal(f)
			}
			break
		}
	}
	request := control("stop", "interrupt")
	request["request"].(map[string]any)["cancel_queued"] = true
	h.send(t, request)
	h.until(t, "result")
	h.finish(t)
}

func TestCancellationUnblocksIdleInput(t *testing.T) {
	h := newHarness(t, func(context.Context, string, rt.RunHost) (string, error) { return "", nil }, Options{})
	h.cancel()
	select {
	case err := <-h.done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("blocked on stdin after cancellation")
	}
}

func TestCanonicalSizeLimitAppliesAfterPreview(t *testing.T) {
	h := newHarness(t, func(_ context.Context, _ string, host rt.RunHost) (string, error) {
		host.Text("preview")
		host.Step(protocol.StepEvent{Action: protocol.StepActionChat, Output: strings.Repeat("x", maxFrameSize/2+1)})
		return "done", nil
	}, Options{})
	h.send(t, user("one", "run"))
	for {
		f := h.next(t)
		if f["type"] == "result" {
			t.Fatal("oversized canonical block completed successfully")
		}
		if f["fatal"] == true {
			break
		}
	}
	if err := <-h.done; err == nil {
		t.Fatal("size limit did not fail connection")
	}
}

type rejectingStartBackend struct{ Backend }

func (rejectingStartBackend) StartRun(context.Context, rt.Input) (rt.RunID, error) {
	return "", errors.New("start rejected")
}
func (rejectingStartBackend) CurrentSessionID() string        { return "session-test" }
func (rejectingStartBackend) CurrentModel() rt.ModelSelection { return rt.ModelSelection{} }
func (rejectingStartBackend) PermissionMode() string          { return "manual" }
func (rejectingStartBackend) Events(context.Context, rt.EventFilter) (<-chan rt.Event, error) {
	return make(chan rt.Event), nil
}

func TestQueuedStartFailuresDoNotHangAfterEOF(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var output bytes.Buffer
	transport := newTransport(ctx, strings.NewReader(""), &output)
	s := &server{backend: rejectingStartBackend{}, conn: transport, sessionID: "session-test", eof: true,
		queue: []userInput{{id: "one", text: "first"}, {id: "two", text: "second"}}, queueBytes: 11}
	if err := s.serve(ctx); err != nil {
		t.Fatal(err)
	}
	if err := transport.flush(ctx); err != nil {
		t.Fatal(err)
	}
	cancel()
	transport.wg.Wait()
	if n := strings.Count(output.String(), `"type":"result"`); n != 2 {
		t.Fatalf("result count=%d: %s", n, output.String())
	}
}
