package a2aapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"

	controlruntime "nekocode/runtime"
)

func TestAgentCardAdvertisesRESTEndpointAndSecurity(t *testing.T) {
	server, err := New(&fakeRuntime{}, Options{
		Endpoint: "https://agent.example/a2a/",
		Secured:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	server.AgentCardHandler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/.well-known/agent-card.json", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var card map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &card); err != nil {
		t.Fatal(err)
	}
	interfaces := card["supportedInterfaces"].([]any)
	first := interfaces[0].(map[string]any)
	if first["url"] != "https://agent.example/a2a" || first["protocolVersion"] != "1.0" || first["protocolBinding"] != "HTTP+JSON" {
		t.Fatalf("interface = %#v", first)
	}
	if card["securitySchemes"] == nil || card["securityRequirements"] == nil {
		t.Fatalf("security declaration missing: %#v", card)
	}
}

func TestNewRejectsInvalidEndpoint(t *testing.T) {
	for _, endpoint := range []string{"", "localhost:8765/a2a", "ftp://example.com/a2a", "https://example.com/a2a?q=1", "https://user:secret@example.com/a2a"} {
		if _, err := New(&fakeRuntime{}, Options{Endpoint: endpoint}); err == nil {
			t.Fatalf("New(%q) succeeded", endpoint)
		}
	}
}

func TestRESTListTasksUsesDaemonIdentity(t *testing.T) {
	rt := &fakeRuntime{eventBatches: [][]controlruntime.Event{{
		{Sequence: 1, Type: controlruntime.EventRunDone, Payload: controlruntime.RunResult{Output: "done"}},
	}}}
	server, err := New(rt, Options{Endpoint: "http://127.0.0.1:8765/a2a"})
	if err != nil {
		t.Fatal(err)
	}
	sendBody, err := json.Marshal(a2a.SendMessageRequest{
		Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("test")),
	})
	if err != nil {
		t.Fatal(err)
	}
	serveA2ARequest(t, server, http.MethodPost, "/message:send", sendBody)

	recorder := serveA2ARequest(t, server, http.MethodGet, "/tasks", nil)
	var response map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	tasks, ok := response["tasks"].([]any)
	if !ok || len(tasks) != 1 {
		t.Fatalf("tasks response = %#v", response)
	}
}

func serveA2ARequest(t *testing.T, server *Server, method, path string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/a2a+json")
	request.Header.Set("A2A-Version", "1.0")
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("%s %s status = %d, body = %s", method, path, recorder.Code, recorder.Body.String())
	}
	return recorder
}

func TestRESTSendMessageRunsNekoCodeTask(t *testing.T) {
	rt := &fakeRuntime{eventBatches: [][]controlruntime.Event{{
		{Sequence: 1, Type: controlruntime.EventRunDone, Payload: controlruntime.RunResult{Output: "done"}},
	}}}
	server, err := New(rt, Options{Endpoint: "http://127.0.0.1:8765/a2a"})
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(a2a.SendMessageRequest{
		Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("inspect the repository")),
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/message:send", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/a2a+json")
	request.Header.Set("A2A-Version", "1.0")
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var response map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	task, ok := response["task"].(map[string]any)
	if !ok {
		t.Fatalf("response has no task: %#v", response)
	}
	status := task["status"].(map[string]any)
	if status["state"] != "TASK_STATE_COMPLETED" {
		t.Fatalf("task status = %#v", status)
	}
	if len(rt.startedInputs) != 1 || rt.startedInputs[0].Text != "inspect the repository" {
		t.Fatalf("runtime inputs = %#v", rt.startedInputs)
	}
}

func TestRESTRejectsOversizedRequest(t *testing.T) {
	rt := &fakeRuntime{}
	server, err := New(rt, Options{Endpoint: "http://127.0.0.1:8765/a2a"})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/message:send", bytes.NewReader(make([]byte, maxRequestBodyBytes+1)))
	request.Header.Set("Content-Type", "application/a2a+json")
	request.Header.Set("A2A-Version", "1.0")
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code == http.StatusOK {
		t.Fatalf("oversized request succeeded: %s", recorder.Body.String())
	}
	if len(rt.startedInputs) != 0 {
		t.Fatalf("runtime started for oversized request: %#v", rt.startedInputs)
	}
}

func TestRESTCanCancelRunningTask(t *testing.T) {
	rt := &fakeRuntime{
		liveEvents:    make(chan controlruntime.Event, 1),
		replayStarted: make(chan struct{}),
	}
	server, err := New(rt, Options{Endpoint: "http://127.0.0.1:8765/a2a"})
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(a2a.SendMessageRequest{
		Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("keep working")),
		Config:  &a2a.SendMessageConfig{ReturnImmediately: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	sent := serveA2ARequest(t, server, http.MethodPost, "/message:send", body)
	var response map[string]any
	if err := json.Unmarshal(sent.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	task := response["task"].(map[string]any)
	taskID := task["id"].(string)
	<-rt.replayStarted

	canceled := serveA2ARequest(t, server, http.MethodPost, "/tasks/"+taskID+":cancel", nil)
	if err := json.Unmarshal(canceled.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response["status"].(map[string]any)["state"] != "TASK_STATE_CANCELED" {
		t.Fatalf("cancel response = %s", canceled.Body.String())
	}
}

func TestRESTRejectsConcurrentExecutionsBeforeStartingRuntime(t *testing.T) {
	rt := &fakeRuntime{
		liveEvents:    make(chan controlruntime.Event, 1),
		replayStarted: make(chan struct{}),
	}
	server, err := New(rt, Options{Endpoint: "http://127.0.0.1:8765/a2a"})
	if err != nil {
		t.Fatal(err)
	}
	requestBody := func(text string) []byte {
		body, err := json.Marshal(a2a.SendMessageRequest{
			Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart(text)),
			Config:  &a2a.SendMessageConfig{ReturnImmediately: true},
		})
		if err != nil {
			t.Fatal(err)
		}
		return body
	}
	first := serveA2ARequest(t, server, http.MethodPost, "/message:send", requestBody("first"))
	var response map[string]any
	if err := json.Unmarshal(first.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	taskID := response["task"].(map[string]any)["id"].(string)
	<-rt.replayStarted

	request := httptest.NewRequest(http.MethodPost, "/message:send", bytes.NewReader(requestBody("second")))
	request.Header.Set("Content-Type", "application/a2a+json")
	request.Header.Set("A2A-Version", "1.0")
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code == http.StatusOK {
		t.Fatalf("concurrent execution succeeded: %s", recorder.Body.String())
	}
	rt.mu.Lock()
	started := len(rt.startedInputs)
	rt.mu.Unlock()
	if started != 1 {
		t.Fatalf("runtime starts = %d, want 1", started)
	}
	serveA2ARequest(t, server, http.MethodPost, "/tasks/"+taskID+":cancel", nil)
}

func TestRESTConcurrencyLimitAllowsInputRequiredFollowUp(t *testing.T) {
	rt := &fakeRuntime{eventBatches: [][]controlruntime.Event{
		{{Sequence: 1, Type: controlruntime.EventApprovalRequested, Payload: controlruntime.ApprovalView{ID: "approval_1", ToolName: "bash"}}},
		{{Sequence: 2, Type: controlruntime.EventRunDone, Payload: controlruntime.RunResult{Output: "done"}}},
	}}
	server, err := New(rt, Options{Endpoint: "http://127.0.0.1:8765/a2a"})
	if err != nil {
		t.Fatal(err)
	}
	firstBody, err := json.Marshal(a2a.SendMessageRequest{
		Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("run command")),
	})
	if err != nil {
		t.Fatal(err)
	}
	first := serveA2ARequest(t, server, http.MethodPost, "/message:send", firstBody)
	var response map[string]any
	if err := json.Unmarshal(first.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	task := response["task"].(map[string]any)
	if task["status"].(map[string]any)["state"] != "TASK_STATE_INPUT_REQUIRED" {
		t.Fatalf("first response = %s", first.Body.String())
	}
	reply := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("allow"))
	reply.TaskID = a2a.TaskID(task["id"].(string))
	reply.ContextID = task["contextId"].(string)
	secondBody, err := json.Marshal(a2a.SendMessageRequest{Message: reply})
	if err != nil {
		t.Fatal(err)
	}
	second := serveA2ARequest(t, server, http.MethodPost, "/message:send", secondBody)
	if err := json.Unmarshal(second.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response["task"].(map[string]any)["status"].(map[string]any)["state"] != "TASK_STATE_COMPLETED" {
		t.Fatalf("follow-up response = %s", second.Body.String())
	}
}

func TestRESTRejectsInvalidFollowUpBeforeTaskHistoryMutation(t *testing.T) {
	rt := &fakeRuntime{
		eventBatches: [][]controlruntime.Event{
			{{Sequence: 1, Type: controlruntime.EventApprovalRequested, Payload: controlruntime.ApprovalView{ID: "approval_1", ToolName: "bash"}}},
		},
	}
	server, err := New(rt, Options{Endpoint: "http://127.0.0.1:8765/a2a"})
	if err != nil {
		t.Fatal(err)
	}
	firstBody, err := json.Marshal(a2a.SendMessageRequest{
		Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("run command")),
	})
	if err != nil {
		t.Fatal(err)
	}
	first := serveA2ARequest(t, server, http.MethodPost, "/message:send", firstBody)
	var response map[string]any
	if err := json.Unmarshal(first.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	task := response["task"].(map[string]any)
	taskID := task["id"].(string)
	reply := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart(strings.Repeat("x", maxMessagePartBytes+1)))
	reply.TaskID = a2a.TaskID(taskID)
	reply.ContextID = task["contextId"].(string)
	replyBody, err := json.Marshal(a2a.SendMessageRequest{Message: reply})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/message:send", bytes.NewReader(replyBody))
	request.Header.Set("Content-Type", "application/a2a+json")
	request.Header.Set("A2A-Version", "1.0")
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code == http.StatusOK {
		t.Fatalf("oversized follow-up succeeded: %s", recorder.Body.String())
	}

	stored := serveA2ARequest(t, server, http.MethodGet, "/tasks/"+taskID, nil)
	if err := json.Unmarshal(stored.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	history := response["task"].(map[string]any)["history"].([]any)
	if len(history) != 1 {
		t.Fatalf("history length = %d, invalid follow-up was persisted", len(history))
	}
}
