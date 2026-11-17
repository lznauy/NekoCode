package a2aapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	rt "nekocode/runtime"
)

func TestRESTReplacesResetAssistantOutput(t *testing.T) {
	for _, kind := range []rt.EventType{rt.EventAssistantDelta, rt.EventAssistantMessage, rt.EventRunDone} {
		t.Run(string(kind), func(t *testing.T) {
			batch := []rt.Event{
				{Sequence: 1, Type: rt.EventAssistantDelta, Payload: rt.DeltaPayload{Delta: "discarded"}},
				{Sequence: 2, Type: rt.EventAssistantReset},
			}
			switch kind {
			case rt.EventAssistantDelta:
				batch = append(batch, rt.Event{Sequence: 3, Type: kind, Payload: rt.DeltaPayload{Delta: "answer"}})
			case rt.EventAssistantMessage:
				batch = append(batch, rt.Event{Sequence: 3, Type: kind, Payload: rt.MessagePayload{Content: "answer"}})
			}
			batch = append(batch, rt.Event{Sequence: 4, Type: rt.EventRunDone, Payload: rt.RunResult{Output: "answer"}})
			r := &fakeRuntime{eventBatches: [][]rt.Event{batch}}
			server, err := New(r, Options{Endpoint: "http://localhost/a2a"})
			if err != nil {
				t.Fatal(err)
			}
			body, err := json.Marshal(a2a.SendMessageRequest{Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("hello"))})
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
			if output := taskText(result.Task); output != "answer" {
				t.Fatalf("reset was ignored: output=%q", output)
			}
		})
	}
}

func taskText(task *a2a.Task) string {
	var output strings.Builder
	for _, artifact := range task.Artifacts {
		for _, part := range artifact.Parts {
			output.WriteString(part.Text())
		}
	}
	return output.String()
}

func TestMonitorReplacesResetAssistantOutput(t *testing.T) {
	r := newInteractiveRuntime(t, "approval")
	e := newExecutor(r)
	e.store = newRetainedTaskStore(256, func(context.Context) (string, error) { return "owner", nil })
	task := storedTaskForTest("task", a2a.TaskStateInputRequired)
	task.Artifacts = []*a2a.Artifact{{ID: "result", Parts: []*a2a.Part{a2a.NewTextPart("discarded")}}}
	if _, err := e.store.Create(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	binding := e.binding(task.ID)
	binding.runID = "run_1"
	binding.artifactID = "result"
	e.Cleanup(context.Background(), &a2asrv.ExecutorContext{TaskID: task.ID, ContextID: task.ContextID}, task, nil)
	binding.mu.Lock()
	done := binding.monitorDone
	binding.mu.Unlock()
	r.publish(rt.EventAssistantReset, nil)
	r.publish(rt.EventRunDone, rt.RunResult{Output: "answer"})
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("reset monitor did not finish")
	}
	stored, err := e.store.Get(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if output := taskText(stored.Task); output != "answer" || stored.Task.Status.State != a2a.TaskStateCompleted {
		t.Fatalf("reset lost final output: %q state=%s", output, stored.Task.Status.State)
	}
}

func TestMonitorStoreOverflowFailsTask(t *testing.T) {
	r := newInteractiveRuntime(t, "approval")
	r.cancelCalled = make(chan struct{})
	e := newExecutor(r)
	e.store = newRetainedTaskStoreWithBudget(256, 1024, func(context.Context) (string, error) { return "owner", nil })
	task := storedTaskForTest("task", a2a.TaskStateInputRequired)
	if _, err := e.store.Create(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	b := e.binding(task.ID)
	b.runID = "run_1"
	b.contextID, b.contextHeld = task.ContextID, true
	e.contexts[task.ContextID] = &contextBinding{sessionID: "session", active: 1}
	e.Cleanup(context.Background(), &a2asrv.ExecutorContext{TaskID: task.ID, ContextID: task.ContextID}, task, nil)
	b.mu.Lock()
	done := b.monitorDone
	b.mu.Unlock()
	r.publish(rt.EventAssistantDelta, rt.DeltaPayload{Delta: strings.Repeat("x", 2048)})
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("monitor hung")
	}
	select {
	case <-r.cancelCalled:
	case <-time.After(time.Second):
		t.Fatal("runtime not canceled")
	}
	stored, err := e.store.Get(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !stored.Task.Status.State.Terminal() {
		t.Fatalf("canceled runtime left non-evictable task: state=%s", stored.Task.Status.State)
	}
	deadline := time.Now().Add(time.Second)
	for e.lookup(task.ID) != nil {
		if time.Now().After(deadline) {
			t.Fatal("failed task binding was retained")
		}
		time.Sleep(time.Millisecond)
	}
	e.mu.Lock()
	active := e.contexts[task.ContextID].active
	e.mu.Unlock()
	if active != 0 {
		t.Fatal("failed task retained its context reservation")
	}
}

func TestSubscribeAfterExternalResolution(t *testing.T) {
	r := newInteractiveRuntime(t, "approval")
	server, err := New(r, Options{Endpoint: "http://localhost/a2a"})
	if err != nil {
		t.Fatal(err)
	}
	task := startInteractiveTask(t, server)
	r.publish(rt.EventApprovalResolved, rt.ApprovalView{ID: "approval_1"})
	deadline := time.Now().Add(time.Second)
	for {
		response := serveA2ARequest(t, server, http.MethodGet, "/tasks/"+string(task.ID), nil)
		var stored a2a.Task
		if err := json.Unmarshal(response.Body.Bytes(), &stored); err != nil {
			t.Fatal(err)
		}
		if stored.Status.State == a2a.TaskStateWorking {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("task did not resume")
		}
		time.Sleep(time.Millisecond)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	req := httptest.NewRequest(http.MethodPost, "/tasks/"+string(task.ID)+":subscribe", nil).WithContext(ctx)
	req.Header.Set("A2A-Version", "1.0")
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { defer close(done); server.Handler().ServeHTTP(rec, req) }()
	r.publish(rt.EventAssistantDelta, rt.DeltaPayload{Delta: "resumed answer"})
	r.publish(rt.EventRunDone, rt.RunResult{Output: "resumed answer"})
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("subscription did not terminate")
	}
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), `"error"`) {
		t.Fatalf("working task cannot be subscribed: status=%d body=%s", rec.Code, rec.Body.String())
	}
	if ctx.Err() != nil || !strings.Contains(rec.Body.String(), "resumed answer") || !strings.Contains(rec.Body.String(), string(a2a.TaskStateCompleted)) {
		t.Fatalf("subscription lost final output: %s", rec.Body.String())
	}
}

func TestInputRequiredStoreFailureReleasesRun(t *testing.T) {
	r := &fakeRuntime{cancelCalled: make(chan struct{}), eventBatches: [][]rt.Event{{{Sequence: 1, Type: rt.EventApprovalRequested, Payload: rt.ApprovalView{ID: "approval", ToolName: "bash", Args: map[string]any{"data": strings.Repeat("x", 2048)}}}}}}
	e := newExecutor(r)
	e.store = newRetainedTaskStoreWithBudget(256, 1024, a2asrv.NewTaskStoreAuthenticator())
	handler := a2asrv.NewRESTHandler(a2asrv.NewHandler(e, a2asrv.WithCallInterceptors(localUserInterceptor{executor: e}), a2asrv.WithTaskStore(e.store)))
	body, err := json.Marshal(a2a.SendMessageRequest{Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("work"))})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/message:send", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/a2a+json")
	req.Header.Set("A2A-Version", "1.0")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), string(a2a.TaskStateFailed)) {
		t.Fatalf("unexpected response: %s", rec.Body.String())
	}
	select {
	case <-r.cancelCalled:
	case <-time.After(time.Second):
		t.Fatal("SDK persistence failure left Runtime waiting for approval")
	}
	deadline := time.Now().Add(time.Second)
	for {
		e.mu.Lock()
		remaining := len(e.tasks)
		active := 0
		for _, binding := range e.contexts {
			active += binding.active
		}
		e.mu.Unlock()
		if remaining == 0 && active == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("leaked bindings=%d active contexts=%d", remaining, active)
		}
		time.Sleep(time.Millisecond)
	}
}
