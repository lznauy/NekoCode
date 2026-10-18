package headless

import (
	"context"
	"encoding/json"
	"nekocode/protocol"
	rt "nekocode/runtime"
	"testing"
)

func TestContextEventForwardsItsSampleWithoutQueryingBackend(t *testing.T) {
	conn := &transport{ctx: context.Background(), writes: make(chan writeItem, 1), errors: make(chan error, 1)}
	s := &server{conn: conn, active: &turn{runID: "run_1"}}
	if err := s.event(context.Background(), rt.Event{Type: rt.EventContextUpdated, Payload: rt.ContextSnapshot{Used: 123}}); err != nil {
		t.Fatal(err)
	}
	select {
	case frame := <-conn.writes:
		var body struct {
			Payload struct {
				Context rt.ContextSnapshot `json:"context"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(frame.data, &body); err != nil {
			t.Fatal(err)
		}
		if body.Payload.Context.Used != 123 {
			t.Fatalf("context = %+v", body.Payload.Context)
		}
	default:
		t.Fatal("context event was not forwarded")
	}
}

func TestAssistantResetBeforeToolDoesNotCommitPrefix(t *testing.T) {
	h := newHarness(t, func(_ context.Context, _ string, host rt.RunHost) (string, error) {
		host.Text("injected")
		host.(rt.RunTextResetter).ResetText()
		host.Step(protocol.StepEvent{Action: protocol.StepActionToolStart, CallID: "c", ToolName: "read"})
		return "", nil
	}, Options{IncludePartialMessages: true})
	h.send(t, user("one", "run"))
	for {
		f := h.next(t)
		if f["type"] == "result" {
			break
		}
		if f["type"] == "assistant" {
			block := f["message"].(map[string]any)["content"].([]any)[0].(map[string]any)
			if block["type"] == "text" && block["text"] == "injected" {
				t.Error("reset prefix committed before tool")
			}
		}
	}
	h.finish(t)
}
