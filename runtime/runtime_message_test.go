package runtime

import (
	"context"
	"testing"

	"nekocode/protocol"
)

func TestCompletedMessageAndStructuredToolInput(t *testing.T) {
	r := New(RunnerFunc(func(ctx context.Context, input string, host RunHost) (string, error) {
		host.Text("preview")
		host.Step(protocol.StepEvent{Action: protocol.StepActionThink, Output: "canonical"})
		host.Step(protocol.StepEvent{Action: protocol.StepActionToolStart, CallID: "tool", ToolArgs: "n=3", ToolInput: []byte(`{"n":3}`)})
		return "done", nil
	}), Services{})
	defer r.Close()
	id, err := r.StartRun(context.Background(), Input{Text: "run"})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.WaitRun(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	events := r.events.History(EventFilter{RunID: id, Types: []EventType{EventAssistantMessage, EventToolStarted}})
	if len(events) != 2 || events[0].Payload.(MessagePayload).Content != "canonical" || string(events[1].Payload.(ToolPayload).Input) != `{"n":3}` {
		t.Fatal(events)
	}
}
