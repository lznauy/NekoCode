package a2aapi

import (
	"context"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"

	rt "nekocode/runtime"
)

func TestRejectsStaleApproval(t *testing.T) {
	r := &fakeRuntime{eventBatches: [][]rt.Event{{{Sequence: 1, Type: rt.EventRunDone}}}}
	e := newExecutor(r)
	b := e.binding("task")
	b.runID = "run_1"
	b.pending = &pendingInteraction{kind: pendingApproval, id: "approval_B"}
	msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewDataPart(map[string]any{"allowed": true, "interactionId": "approval_A"}))
	ec := &a2asrv.ExecutorContext{TaskID: "task", ContextID: "context", StoredTask: &a2a.Task{ID: "task", Status: a2a.TaskStatus{State: a2a.TaskStateInputRequired}}, Message: msg}
	for range e.Execute(context.Background(), ec) {
	}
	if r.approvalID == "approval_B" {
		t.Fatal("stale approval_A reply authorized approval_B")
	}
}

func TestInteractionRepliesBindToCurrentPrompt(t *testing.T) {
	for _, kind := range []pendingKind{pendingApproval, pendingQuestion} {
		for _, test := range []struct {
			name    string
			message *a2a.Message
			valid   bool
		}{
			{"metadata", interactionMessage("current", a2a.NewTextPart("allow")), true},
			{"data", a2a.NewMessage(a2a.MessageRoleUser, a2a.NewDataPart(map[string]any{"interactionId": "current", "allowed": true, "answers": [][]string{{"answer"}}})), true},
			{"missing", a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("allow")), false},
			{"stale", interactionMessage("previous", a2a.NewTextPart("allow")), false},
			{"conflicting", interactionMessage("current", a2a.NewDataPart(map[string]any{"interactionId": "previous", "allowed": true})), false},
			{"invalid type", a2a.NewMessage(a2a.MessageRoleUser, a2a.NewDataPart(map[string]any{"interactionId": 42, "allowed": true})), false},
		} {
			t.Run(string(kind)+"/"+test.name, func(t *testing.T) {
				r := &fakeRuntime{}
				err := newExecutor(r).resolvePending(t.Context(), &pendingInteraction{kind: kind, id: "current", questions: 1}, test.message, "allow")
				if (err == nil) != test.valid {
					t.Fatalf("valid=%v error=%v", test.valid, err)
				}
				if !test.valid && (r.approvalID != "" || r.questionID != "") {
					t.Fatal("invalid reply resolved an interaction")
				}
				if test.valid && r.approvalID != "current" && r.questionID != "current" {
					t.Fatal("valid reply did not reach its interaction")
				}
			})
		}
	}
}

func interactionMessage(id string, parts ...*a2a.Part) *a2a.Message {
	message := a2a.NewMessage(a2a.MessageRoleUser, parts...)
	message.Metadata = map[string]any{"interactionId": id}
	return message
}
