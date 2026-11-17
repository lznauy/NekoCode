package a2aapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/a2aproject/a2a-go/v2/a2asrv/taskstore"

	"nekocode/protocol"
	controlruntime "nekocode/runtime"
	"nekocode/runtime/internal/eventbus"
)

// Unlike the batch-only fake, this runtime replays retained events to every
// reader and keeps subscriptions open while an interaction is pending.
type interactiveRuntime struct {
	*fakeRuntime
	bus  *eventbus.EventBus
	kind string
}

func newInteractiveRuntime(t *testing.T, kind string) *interactiveRuntime {
	t.Helper()
	r := &interactiveRuntime{fakeRuntime: &fakeRuntime{}, bus: eventbus.NewEventBus(), kind: kind}
	t.Cleanup(r.bus.Close)
	return r
}

func (r *interactiveRuntime) publish(kind controlruntime.EventType, payload any) {
	r.bus.Publish(controlruntime.Event{RunID: "run_1", Type: kind, Payload: payload})
}

func (r *interactiveRuntime) StartRunInSession(ctx context.Context, sessionID string, input controlruntime.Input) (controlruntime.RunID, string, error) {
	id, sessionID, err := r.fakeRuntime.StartRunInSession(ctx, sessionID, input)
	if r.kind == "question" {
		r.publish(controlruntime.EventQuestionRequested, controlruntime.QuestionView{ID: "question_1", Questions: []protocol.QuestionItem{{Question: "Which option?"}}})
	} else {
		r.publish(controlruntime.EventApprovalRequested, controlruntime.ApprovalView{ID: "approval_1", ToolName: "bash"})
	}
	return id, sessionID, err
}

func (r *interactiveRuntime) ReplayEvents(ctx context.Context, filter controlruntime.EventFilter) (<-chan controlruntime.Event, error) {
	return r.bus.SubscribeReplay(ctx, filter)
}

func (r *interactiveRuntime) DecideApproval(ctx context.Context, id string, decision controlruntime.ApprovalDecision) error {
	if err := r.fakeRuntime.DecideApproval(ctx, id, decision); err != nil {
		return err
	}
	r.publish(controlruntime.EventApprovalResolved, controlruntime.ApprovalView{ID: id})
	r.publish(controlruntime.EventRunDone, controlruntime.RunResult{Output: "done"})
	return nil
}

func (r *interactiveRuntime) AnswerQuestion(ctx context.Context, id string, reply controlruntime.QuestionReply) error {
	if err := r.fakeRuntime.AnswerQuestion(ctx, id, reply); err != nil {
		return err
	}
	r.publish(controlruntime.EventQuestionResolved, controlruntime.QuestionView{ID: id})
	r.publish(controlruntime.EventRunDone, controlruntime.RunResult{Output: "done"})
	return nil
}

func (r *interactiveRuntime) CancelRun(ctx context.Context, id controlruntime.RunID) error {
	if err := r.fakeRuntime.CancelRun(ctx, id); err != nil {
		return err
	}
	r.publish(controlruntime.EventRunCancelled, controlruntime.RunResult{})
	return nil
}

func startInteractiveTask(t *testing.T, server *Server) *a2a.Task {
	t.Helper()
	body, err := json.Marshal(a2a.SendMessageRequest{Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("run command"))})
	if err != nil {
		t.Fatal(err)
	}
	response := serveA2ARequest(t, server, http.MethodPost, "/message:send", body)
	var result struct {
		Task *a2a.Task `json:"task"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Task == nil || result.Task.Status.State != a2a.TaskStateInputRequired {
		t.Fatalf("unexpected response: %s", response.Body.String())
	}
	return result.Task
}

func TestRESTTracksExternalInteractionResolution(t *testing.T) {
	for _, kind := range []string{"approval", "question", "cancel", "failure"} {
		t.Run(kind, func(t *testing.T) {
			r := newInteractiveRuntime(t, kind)
			server, err := New(r, Options{Endpoint: "http://127.0.0.1:8765/a2a"})
			if err != nil {
				t.Fatal(err)
			}
			task := startInteractiveTask(t, server)
			want := a2a.TaskStateCompleted
			switch kind {
			case "approval":
				err = r.DecideApproval(context.Background(), "approval_1", controlruntime.ApprovalDecision{Allowed: true})
			case "question":
				err = r.AnswerQuestion(context.Background(), "question_1", controlruntime.QuestionReply{})
			case "cancel":
				err = r.CancelRun(context.Background(), "run_1")
				want = a2a.TaskStateCanceled
			case "failure":
				r.publish(controlruntime.EventRunFailed, controlruntime.RunResult{Error: "external failure"})
				want = a2a.TaskStateFailed
			}
			if err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(2 * time.Second)
			for {
				response := serveA2ARequest(t, server, http.MethodGet, "/tasks/"+string(task.ID), nil)
				var result a2a.Task
				if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				if result.Status.State == want {
					if want == a2a.TaskStateCompleted && (len(result.Artifacts) == 0 || result.Artifacts[0].Parts[0].Text() != "done") {
						t.Fatalf("missing external result: %s", response.Body.String())
					}
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("task stuck after external resolution: %s", response.Body.String())
				}
				time.Sleep(time.Millisecond)
			}
		})
	}
}

func TestExecutorPreservesSystemCommandOutput(t *testing.T) {
	r := &fakeRuntime{eventBatches: [][]controlruntime.Event{{
		{Sequence: 1, Type: controlruntime.EventSystemMessage, Payload: controlruntime.MessagePayload{Role: "system", Content: "command result"}},
		{Sequence: 2, Type: controlruntime.EventRunDone, Payload: controlruntime.RunResult{}},
	}}}
	events := executeEvents(t, newExecutor(r), &a2asrv.ExecutorContext{TaskID: "command", ContextID: "context", Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("/help"))})
	for _, event := range events {
		if artifact, ok := event.(*a2a.TaskArtifactUpdateEvent); ok && artifact.Artifact.Parts[0].Text() == "command result" {
			return
		}
	}
	t.Fatal("command completed but its output was discarded")
}

func TestRejectedFollowUpDoesNotStealForegroundExecution(t *testing.T) {
	r := newInteractiveRuntime(t, "approval")
	e := newExecutor(r)
	e.store = taskstore.NewInMemory(nil)
	task := &a2a.Task{ID: "task", ContextID: "context", Status: a2a.TaskStatus{State: a2a.TaskStateInputRequired}}
	ctx := context.Background()
	if _, err := e.store.Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	binding := e.binding(task.ID)
	binding.runID, binding.contextID, binding.contextHeld = "run_1", task.ContextID, true
	e.contexts[task.ContextID] = &contextBinding{sessionID: "session", active: 1}
	i := localUserInterceptor{executor: e}
	request := func() (context.Context, *a2asrv.CallContext) {
		ctx, call := a2asrv.NewCallContext(context.Background(), nil)
		message := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("allow"))
		message.TaskID = task.ID
		ctx, _, err := i.Before(ctx, call, &a2asrv.Request{Payload: &a2a.SendMessageRequest{Message: message}})
		if err != nil {
			t.Fatal(err)
		}
		return ctx, call
	}
	first, firstCall := request()
	duplicate, duplicateCall := request()
	if err := i.After(duplicate, duplicateCall, &a2asrv.Response{Err: errors.New("execution in progress")}); err != nil {
		t.Fatal(err)
	}
	binding.mu.Lock()
	foreground, monitoring := binding.foreground, binding.monitorCancel != nil
	binding.mu.Unlock()
	if foreground != 1 || monitoring {
		t.Fatalf("duplicate stole execution: foreground=%d monitor=%v", foreground, monitoring)
	}
	// SDK cleanup happens before the successful request's After hook. The
	// pause must defer monitor startup until that request releases its handoff.
	e.Cleanup(first, &a2asrv.ExecutorContext{TaskID: task.ID, ContextID: task.ContextID, Message: a2a.NewMessage(a2a.MessageRoleUser)}, task, nil)
	if err := i.After(first, firstCall, &a2asrv.Response{Payload: task}); err != nil {
		t.Fatal(err)
	}
	binding.mu.Lock()
	done := binding.monitorDone
	binding.mu.Unlock()
	if done == nil {
		t.Fatal("monitor was not restored after foreground finished")
	}
	r.publish(controlruntime.EventRunDone, controlruntime.RunResult{Output: "done"})
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("monitor did not finish")
	}
	if e.lookup(task.ID) != nil || e.contexts[task.ContextID].active != 0 {
		t.Fatal("terminal run retained its binding or context reservation")
	}
	stored, err := e.store.Get(ctx, task.ID)
	if err != nil || stored.Task.Status.State != a2a.TaskStateCompleted {
		t.Fatalf("terminal state not persisted: %v, %v", stored, err)
	}
}

func TestSystemOutputDoesNotSuppressFinalAnswer(t *testing.T) {
	r := &fakeRuntime{eventBatches: [][]controlruntime.Event{{
		{Sequence: 1, Type: controlruntime.EventSystemMessage, Payload: controlruntime.MessagePayload{Content: "notice"}},
		{Sequence: 2, Type: controlruntime.EventAssistantMessage, Payload: controlruntime.MessagePayload{Content: "answer"}},
		{Sequence: 3, Type: controlruntime.EventRunDone, Payload: controlruntime.RunResult{Output: "answer"}},
	}}}
	events := executeEvents(t, newExecutor(r), &a2asrv.ExecutorContext{TaskID: "task", ContextID: "context", Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("hello"))})
	var output []string
	for _, event := range events {
		if artifact, ok := event.(*a2a.TaskArtifactUpdateEvent); ok {
			output = append(output, artifact.Artifact.Parts[0].Text())
		}
	}
	if len(output) != 2 || output[0] != "notice" || output[1] != "answer" {
		t.Fatalf("output=%v", output)
	}
}
