package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"

	controlruntime "nekocode/runtime"
	"nekocode/runtime/internal/eventbus"
	"nekocode/runtime/internal/runstore"
)

type fakeRuntime struct {
	bus   *eventbus.EventBus
	store *runstore.RunStore

	mu              sync.Mutex
	nextRun         int
	lastInput       controlruntime.Input
	approved        string
	approvalAllowed bool
	answered        string
	answerRejected  bool
	cancelled       controlruntime.RunID
	connectName     string
	connectArgs     []string
	disconnectName  string
}

func newFakeRuntime() *fakeRuntime {
	bus := eventbus.NewEventBus()
	store := runstore.NewRunStore(0)
	bus.AddObserver(store.Record)
	return &fakeRuntime{bus: bus, store: store}
}

func (f *fakeRuntime) Publish(ev controlruntime.Event) {
	f.bus.Publish(ev)
}

func (f *fakeRuntime) StartRun(_ context.Context, input controlruntime.Input) (controlruntime.RunID, error) {
	f.mu.Lock()
	f.nextRun++
	f.lastInput = input
	runID := controlruntime.RunID("run_test")
	if f.nextRun > 1 {
		runID = controlruntime.RunID("run_test_" + string(rune('0'+f.nextRun)))
	}
	f.mu.Unlock()
	f.bus.Publish(controlruntime.Event{
		RunID:  runID,
		Type:   controlruntime.EventInputAccepted,
		Source: input.Source,
		Payload: controlruntime.MessagePayload{
			Role:    "user",
			Content: input.Text,
			Source:  input.Source,
			Sender:  input.Sender,
		},
	})
	f.bus.Publish(controlruntime.Event{RunID: runID, Type: controlruntime.EventRunStarted})
	return runID, nil
}

func (f *fakeRuntime) CancelRun(_ context.Context, runID controlruntime.RunID) error {
	f.mu.Lock()
	f.cancelled = runID
	f.mu.Unlock()
	f.bus.Publish(controlruntime.Event{RunID: runID, Type: controlruntime.EventRunCancelled})
	return nil
}

func (f *fakeRuntime) DecideApproval(_ context.Context, approvalID string, decision controlruntime.ApprovalDecision) error {
	f.mu.Lock()
	f.approved = approvalID
	f.approvalAllowed = decision.Allowed
	f.mu.Unlock()
	return nil
}

func (f *fakeRuntime) AnswerQuestion(_ context.Context, questionID string, reply controlruntime.QuestionReply) error {
	f.mu.Lock()
	f.answered = questionID
	f.answerRejected = reply.Rejected
	f.mu.Unlock()
	return nil
}

func (f *fakeRuntime) Events(ctx context.Context, filter controlruntime.EventFilter) (<-chan controlruntime.Event, error) {
	return f.bus.Subscribe(ctx, filter)
}

func (f *fakeRuntime) ReplayEvents(ctx context.Context, filter controlruntime.EventFilter) (<-chan controlruntime.Event, error) {
	return f.bus.SubscribeReplay(ctx, filter)
}

func (f *fakeRuntime) CurrentRun() (controlruntime.RunSnapshot, bool) {
	return f.store.Current()
}

func (f *fakeRuntime) LookupRun(runID controlruntime.RunID) (controlruntime.RunSnapshot, bool) {
	return f.store.Lookup(runID)
}

func (f *fakeRuntime) Runs(limit int) []controlruntime.RunSnapshot {
	return f.store.List(limit)
}

func (f *fakeRuntime) ConnectView() controlruntime.ConnectView {
	return controlruntime.ConnectView{
		Connectors: []controlruntime.ConnectorView{
			{
				Name:        "telegram",
				Registered:  true,
				Initialized: true,
				Configured:  true,
				Running:     true,
				Status:      "running",
				Devices: []controlruntime.ConnectorDeviceView{
					{ID: "1", Username: "alice"},
				},
			},
		},
	}
}

func (f *fakeRuntime) Connect(_ context.Context, name string, args []string) (string, error) {
	f.mu.Lock()
	f.connectName = name
	f.connectArgs = append([]string(nil), args...)
	f.mu.Unlock()
	return "connected " + name, nil
}

func (f *fakeRuntime) Disconnect(name string) (string, error) {
	f.mu.Lock()
	f.disconnectName = name
	f.mu.Unlock()
	return "disconnected " + name, nil
}

func (f *fakeRuntime) Metrics() controlruntime.MetricsSnapshot {
	return controlruntime.MetricsSnapshot{PromptTokens: 10, CompletionTokens: 5}
}

func (f *fakeRuntime) Status() controlruntime.RuntimeStatus {
	return controlruntime.RuntimeStatus{State: controlruntime.RuntimeReady}
}

func (f *fakeRuntime) Capabilities() controlruntime.CapabilityManifest {
	return controlruntime.CapabilityManifest{
		Protocol: controlruntime.ProtocolVersion,
		Commands: true, Metrics: true, Models: true,
		Context: true, Sessions: true, Connectors: true,
	}
}

func (f *fakeRuntime) ContextSnapshot() controlruntime.ContextSnapshot {
	return controlruntime.ContextSnapshot{Budget: 100, Used: 25, Free: 75}
}

func (f *fakeRuntime) MemoryView(scope controlruntime.MemoryScope) controlruntime.MemoryView {
	if scope == "" {
		scope = controlruntime.MemoryScopeProject
	}
	return controlruntime.MemoryView{
		Scope:   scope,
		Path:    "/tmp/memory.md",
		Content: "[Project Memory]\n## Active Goals\n- converge runtime",
		Sections: []controlruntime.MemorySection{
			{Key: "tech_stack", Title: "## Tech Stack", Empty: true},
			{Key: "active_goals", Title: "## Active Goals", Content: "- converge runtime"},
			{Key: "completed_tasks", Title: "## Completed Tasks", Empty: true},
			{Key: "architecture_map", Title: "## Key Architecture Map", Empty: true},
			{Key: "preferences", Title: "## User Preferences", Empty: true},
		},
	}
}

func (f *fakeRuntime) ListSessions() []controlruntime.SessionMeta {
	return []controlruntime.SessionMeta{{ID: "session_1", CWD: "/tmp", MsgCount: 2}}
}

func (f *fakeRuntime) SessionMessages() []controlruntime.DisplayMessage {
	return []controlruntime.DisplayMessage{{Role: "user", Content: "hello"}}
}

func (f *fakeRuntime) CurrentSessionID() string   { return "session_1" }
func (f *fakeRuntime) ResumeSession(string) error { return nil }
func (f *fakeRuntime) NewSession() (controlruntime.SessionMeta, error) {
	return controlruntime.SessionMeta{ID: "session_2"}, nil
}
func (f *fakeRuntime) DeleteSession(string) error { return nil }

func (f *fakeRuntime) CommandCatalog() []string {
	return []string{"help", "model", "connect"}
}

func (f *fakeRuntime) CommandMenu(_ context.Context, input string) (controlruntime.CommandMenu, bool) {
	if input != "/" {
		return controlruntime.CommandMenu{}, false
	}
	return controlruntime.CommandMenu{Title: "Commands", Items: []controlruntime.CommandMenuItem{{Value: "/help", Label: "/help"}}}, true
}

func (f *fakeRuntime) CurrentModel() controlruntime.ModelSelection {
	return controlruntime.ModelSelection{Provider: "openai", Model: "gpt-test"}
}

func (f *fakeRuntime) SwitchModel(string) (controlruntime.ModelSelection, error) {
	return f.CurrentModel(), nil
}

func TestServerSubmitAndQueryRun(t *testing.T) {
	rt := newFakeRuntime()
	server := httptest.NewServer(New(rt).Handler())
	defer server.Close()

	resp := postJSON(t, server.URL+"/input", map[string]any{
		"text": "hello",
		"source": map[string]any{
			"kind": "test",
		},
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var submitted submitResponse
	if err := json.NewDecoder(resp.Body).Decode(&submitted); err != nil {
		t.Fatalf("decode submit: %v", err)
	}
	if submitted.RunID != "run_test" {
		t.Fatalf("run id = %q", submitted.RunID)
	}

	resp, err := http.Get(server.URL + "/runs/run_test")
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("run status = %d", resp.StatusCode)
	}
	var run controlruntime.RunSnapshot
	if err := json.NewDecoder(resp.Body).Decode(&run); err != nil {
		t.Fatalf("decode run: %v", err)
	}
	if run.Input != "hello" || run.Source.Kind != "test" {
		t.Fatalf("run view = %#v", run)
	}
}

func TestSubmitRequestOmitsOptionalNestedObjects(t *testing.T) {
	data, err := json.Marshal(submitRequest{Text: "hello"})
	if err != nil {
		t.Fatalf("marshal submit request: %v", err)
	}
	got := string(data)
	if strings.Contains(got, `"source"`) || strings.Contains(got, `"sender"`) {
		t.Fatalf("empty optional nested objects should be omitted: %s", got)
	}

	source := controlruntime.SourceRef{Kind: "test"}
	sender := controlruntime.SenderRef{Username: "alice"}
	data, err = json.Marshal(submitRequest{
		Text:   "hello",
		Source: &source,
		Sender: &sender,
	})
	if err != nil {
		t.Fatalf("marshal populated submit request: %v", err)
	}
	got = string(data)
	if !strings.Contains(got, `"source"`) || !strings.Contains(got, `"sender"`) {
		t.Fatalf("populated optional nested objects should be present: %s", got)
	}
}

// TestServerInputForwardsImages covers the images passthrough: attachments
// submitted over HTTP must reach StartRun intact (label + path).
func TestServerInputForwardsImages(t *testing.T) {
	rt := newFakeRuntime()
	handler := New(rt).Handler()

	body := `{"text":"看图 [Image #1]","images":[{"label":"[Image #1]","path":"/tmp/a.png"}]}`
	req := httptest.NewRequest(http.MethodPost, "/input", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("input status = %d body=%s", rec.Code, rec.Body.String())
	}

	rt.mu.Lock()
	got := rt.lastInput
	rt.mu.Unlock()
	if got.Text != "看图 [Image #1]" || len(got.Images) != 1 || got.Images[0].Label != "[Image #1]" || got.Images[0].Path != "/tmp/a.png" {
		t.Fatalf("input forwarded = %#v", got)
	}
}

func TestServerConnectView(t *testing.T) {
	rt := newFakeRuntime()
	handler := New(rt).Handler()

	req := httptest.NewRequest(http.MethodGet, "/connect", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("connect status = %d", rec.Code)
	}
	var view controlruntime.ConnectView
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("decode connect: %v", err)
	}
	if len(view.Connectors) != 1 || view.Connectors[0].Name != "telegram" || len(view.Connectors[0].Devices) != 1 {
		t.Fatalf("connect view = %#v", view)
	}
}

func TestServerExtraQueriesAndConnectCommand(t *testing.T) {
	rt := newFakeRuntime()
	handler := New(rt).Handler()

	req := httptest.NewRequest(http.MethodPost, "/input", bytes.NewBufferString(`{"text":"hello"}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("input status = %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/runs/current", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("current run status = %d body=%s", rec.Code, rec.Body.String())
	}

	for _, path := range []string{"/status", "/capabilities", "/metrics", "/context", "/memory", "/sessions", "/sessions/current/messages", "/model", "/commands", "/commands/menu?input=%2F"} {
		req = httptest.NewRequest(http.MethodGet, path, nil)
		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d body=%s", path, rec.Code, rec.Body.String())
		}
	}

	req = httptest.NewRequest(http.MethodGet, "/memory", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	var memory controlruntime.MemoryView
	if err := json.Unmarshal(rec.Body.Bytes(), &memory); err != nil {
		t.Fatalf("decode memory: %v", err)
	}
	if memory.Scope != controlruntime.MemoryScopeProject || len(memory.Sections) != 5 || memory.Sections[1].Content != "- converge runtime" {
		t.Fatalf("memory view = %#v", memory)
	}

	req = httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	var metricsPayload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &metricsPayload); err != nil {
		t.Fatalf("decode metrics: %v", err)
	}
	if _, ok := metricsPayload["promptTokens"]; !ok {
		t.Fatalf("metrics should use JSON tags, got %#v", metricsPayload)
	}
	if _, ok := metricsPayload["PromptTokens"]; ok {
		t.Fatalf("metrics leaked Go field name: %#v", metricsPayload)
	}

	req = httptest.NewRequest(http.MethodGet, "/sessions/current/messages", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	var messagesPayload map[string][]map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &messagesPayload); err != nil {
		t.Fatalf("decode messages: %v", err)
	}
	if len(messagesPayload["messages"]) != 1 || messagesPayload["messages"][0]["role"] != "user" {
		t.Fatalf("messages should use JSON tags, got %#v", messagesPayload)
	}
	if _, ok := messagesPayload["messages"][0]["Role"]; ok {
		t.Fatalf("messages leaked Go field name: %#v", messagesPayload)
	}

	req = httptest.NewRequest(http.MethodGet, "/model", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	var model map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &model); err != nil {
		t.Fatalf("decode model: %v", err)
	}
	if model["provider"] != "openai" || model["model"] != "gpt-test" {
		t.Fatalf("model view = %#v", model)
	}

	req = httptest.NewRequest(http.MethodPost, "/connect/telegram", bytes.NewBufferString(`{"args":["status"]}`))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("connect command status = %d body=%s", rec.Code, rec.Body.String())
	}
	if rt.connectName != "telegram" || len(rt.connectArgs) != 1 || rt.connectArgs[0] != "status" {
		t.Fatalf("connect command not forwarded: name=%q args=%#v", rt.connectName, rt.connectArgs)
	}

	req = httptest.NewRequest(http.MethodPost, "/connect/slack", http.NoBody)
	req.ContentLength = -1
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("connect empty body status = %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/disconnect/telegram", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("disconnect command status = %d body=%s", rec.Code, rec.Body.String())
	}
	if rt.disconnectName != "telegram" {
		t.Fatalf("disconnect command not forwarded: %q", rt.disconnectName)
	}
}

func TestServerRejectsUnavailableOptionalCapabilities(t *testing.T) {
	rt := controlruntime.New(controlruntime.RunnerFunc(
		func(context.Context, string, controlruntime.RunHost) (string, error) {
			return "", nil
		},
	), controlruntime.Services{})
	t.Cleanup(func() {
		if err := rt.Close(); err != nil {
			t.Error(err)
		}
	})
	handler := New(rt).Handler()

	for _, path := range []string{"/metrics", "/model", "/context", "/sessions", "/connect"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotImplemented {
			t.Fatalf("%s status = %d, want %d", path, rec.Code, http.StatusNotImplemented)
		}
	}
}

func TestServerControls(t *testing.T) {
	rt := newFakeRuntime()
	handler := New(rt).Handler()
	req := httptest.NewRequest(http.MethodPost, "/approvals/apr_1", bytes.NewBufferString(`{"allowed":true,"allow_with_permission":true}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("approval status = %d", rec.Code)
	}
	if rt.approved != "apr_1" || !rt.approvalAllowed {
		t.Fatalf("approval not forwarded")
	}

	req = httptest.NewRequest(http.MethodPost, "/questions/q_1", bytes.NewBufferString(`{"rejected":true}`))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("question status = %d", rec.Code)
	}
	if rt.answered != "q_1" || !rt.answerRejected {
		t.Fatalf("question not forwarded")
	}

	req = httptest.NewRequest(http.MethodPost, "/runs/run_1/abort", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("abort status = %d", rec.Code)
	}
	if rt.cancelled != "run_1" {
		t.Fatalf("cancel not forwarded: %q", rt.cancelled)
	}
}

func TestServerRejectsInvalidJSONContracts(t *testing.T) {
	rt := newFakeRuntime()
	handler := New(rt).Handler()

	for _, tc := range []struct {
		name string
		path string
		body string
	}{
		{
			name: "unknown submit field",
			path: "/input",
			body: `{"text":"hello","unexpected":true}`,
		},
		{
			name: "trailing submit json",
			path: "/input",
			body: `{"text":"hello"} {"text":"again"}`,
		},
		{
			name: "trailing optional connect json",
			path: "/connect/telegram",
			body: `{"args":["status"]} {"args":["again"]}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tc.path, bytes.NewBufferString(tc.body))
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
			}
			var payload map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
				t.Fatalf("decode error payload: %v", err)
			}
			if !strings.Contains(payload["error"], "invalid json") {
				t.Fatalf("error payload = %#v", payload)
			}
		})
	}
}

func TestServerEventsSSE(t *testing.T) {
	rt := newFakeRuntime()
	server := httptest.NewServer(New(rt).Handler())
	defer server.Close()

	rt.bus.Publish(controlruntime.Event{
		RunID:   "run_1",
		Type:    controlruntime.EventSystemMessage,
		Payload: controlruntime.MessagePayload{Content: "historical"},
	})

	req, err := http.NewRequest(http.MethodGet, server.URL+"/events?run_id=run_1&replay=1", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("events request: %v", err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("content type = %q", ct)
	}

	rt.bus.Publish(controlruntime.Event{
		RunID:   "run_1",
		Type:    controlruntime.EventRunDone,
		Payload: controlruntime.RunResult{Output: "ok"},
	})

	buf := make([]byte, 512)
	n, err := resp.Body.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatalf("read sse: %v", err)
	}
	got := string(buf[:n])
	if !strings.Contains(got, "historical") && !strings.Contains(got, "event: run_done") {
		t.Fatalf("sse = %q", got)
	}
}

func TestRuntimeHTTPAPIDocCoversRegisteredRoutes(t *testing.T) {
	serverSource, err := os.ReadFile("httpapi.go")
	if err != nil {
		t.Fatalf("read httpapi.go: %v", err)
	}
	doc, err := os.ReadFile("../../docs/RUNTIME_HTTP_API.md")
	if err != nil {
		t.Fatalf("read api doc: %v", err)
	}

	registered := parseRegisteredRoutes(string(serverSource))
	documented := parseDocumentedRoutes(string(doc))
	for _, route := range registered {
		if !documented[route] {
			t.Fatalf("route %s is registered but missing from docs/RUNTIME_HTTP_API.md", route)
		}
	}
}

func postJSON(t *testing.T, url string, body any) *http.Response {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	resp, err := http.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	return resp
}

func parseRegisteredRoutes(source string) []string {
	re := regexp.MustCompile(`mux\.HandleFunc\("([A-Z]+) ([^"]+)"`)
	matches := re.FindAllStringSubmatch(source, -1)
	out := make([]string, 0, len(matches))
	for _, match := range matches {
		out = append(out, match[1]+" "+match[2])
	}
	return out
}

func parseDocumentedRoutes(doc string) map[string]bool {
	re := regexp.MustCompile(`\| (GET|POST) \| ` + "`" + `([^` + "`" + `]+)` + "`" + ` \|`)
	matches := re.FindAllStringSubmatch(doc, -1)
	out := make(map[string]bool, len(matches))
	for _, match := range matches {
		path := strings.SplitN(match[2], "?", 2)[0]
		out[match[1]+" "+path] = true
	}
	return out
}
