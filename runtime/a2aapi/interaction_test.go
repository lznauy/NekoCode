package a2aapi

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/a2aproject/a2a-go/v2/a2asrv/taskstore"

	rt "nekocode/runtime"
)

func TestRejectsStaleApproval(t *testing.T) {
	r := &fakeRuntime{eventBatches: [][]rt.Event{{{Sequence: 1, Type: rt.EventRunDone}}}}
	e := newExecutor(r)
	b := e.binding("task")
	b.runID = "run_1"

	msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewDataPart(map[string]any{"allowed": true, "interactionId": "approval_A"}))
	ec := &a2asrv.ExecutorContext{TaskID: "task", ContextID: "context", StoredTask: &a2a.Task{ID: "task", Status: a2a.TaskStatus{State: a2a.TaskStateInputRequired}}, Message: msg}
	request := interactionRequest(ec, rt.Event{Type: rt.EventApprovalRequested, Payload: rt.ApprovalView{ID: "approval_B", ToolName: "bash"}})
	b.pending = []*pendingInteraction{request}
	for event, err := range e.Execute(context.Background(), ec) {
		if err != nil {
			t.Fatal(err)
		}
		update := event.(*a2a.TaskStatusUpdateEvent)
		prompt, err := json.Marshal(update.Status.Message)
		if err != nil || !strings.Contains(string(prompt), `"toolName":"bash"`) {
			t.Fatalf("invalid reply lost current approval details: %s (%v)", prompt, err)
		}
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

func TestStoredApprovalKeepsExactToolArguments(t *testing.T) {
	ec := &a2asrv.ExecutorContext{TaskID: "task", ContextID: "context"}
	id := json.Number("9007199254740993")
	request := interactionRequest(ec, rt.Event{Type: rt.EventApprovalRequested, Payload: rt.ApprovalView{
		ID: "approval", ToolName: "update", Args: map[string]any{"id": id},
	}})
	task := &a2a.Task{ID: ec.TaskID, ContextID: ec.ContextID, Status: a2a.TaskStatus{
		State: a2a.TaskStateInputRequired, Message: request.message,
	}}
	store := taskstore.NewInMemory(nil)
	if _, err := store.Create(t.Context(), task); err != nil {
		t.Fatal(err)
	}
	stored, err := store.Get(t.Context(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(stored.Task.Status.Message)
	if err != nil || !strings.Contains(string(data), `"id":9007199254740993`) {
		t.Fatalf("approval changed the tool argument: %s (%v)", data, err)
	}
}
