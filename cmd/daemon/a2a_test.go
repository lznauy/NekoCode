package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"

	controlruntime "nekocode/runtime"
	"nekocode/runtime/httpapi"
)

func TestA2AMigrationPreservesExistingDaemonAPI(t *testing.T) {
	rt := controlruntime.New(controlruntime.RunnerFunc(func(context.Context, string, controlruntime.RunHost) (string, error) { return "done", nil }), controlruntime.ServiceGroups{})
	t.Cleanup(func() { _ = rt.Close() })
	options := httpapi.Options{Token: "secret", AllowOrigin: "https://dashboard.example"}
	baseline := httpapi.WithOptions(httpapi.New(rt).Handler(), options)
	for _, enabled := range []bool{false, true} {
		handler, err := newDaemonHandler(rt, "0.0.0.0:8765", options, enabled, "")
		if err != nil {
			t.Fatal(err)
		}
		for _, test := range []struct{ method, path, token string }{
			{http.MethodGet, "/health", ""},
			{http.MethodGet, "/runs", ""},
			{http.MethodGet, "/runs", "secret"},
			{http.MethodGet, "/observe/overview", "secret"},
			{http.MethodGet, "/observe/metrics?token=secret", ""},
			{http.MethodOptions, "/observe/overview", ""},
			{http.MethodGet, "/unknown", "secret"},
		} {
			record := func(h http.Handler) *httptest.ResponseRecorder {
				req := httptest.NewRequest(test.method, test.path, nil)
				req.Header.Set("Origin", "https://dashboard.example")
				if test.token != "" {
					req.Header.Set("Authorization", "Bearer "+test.token)
				}
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, req)
				return rec
			}
			want, got := record(baseline), record(handler)
			if got.Code != want.Code {
				t.Fatalf("A2A=%v %s %s: status=%d want=%d", enabled, test.method, test.path, got.Code, want.Code)
			}
			for _, header := range []string{"Content-Type", "WWW-Authenticate", "Access-Control-Allow-Origin", "Access-Control-Allow-Methods", "Access-Control-Allow-Headers"} {
				if got.Header().Get(header) != want.Header().Get(header) {
					t.Fatalf("A2A=%v %s: header %s changed", enabled, test.path, header)
				}
			}
		}
	}
}

func TestA2AUsesMasterRuntimeAndRestoresMainSession(t *testing.T) {
	var mu sync.Mutex
	current := "main"
	selectSession := func(id string) error { mu.Lock(); defer mu.Unlock(); current = id; return nil }
	services := controlruntime.ServiceGroups{Sessions: controlruntime.SessionsServices{
		CurrentSessionID: func() string { mu.Lock(); defer mu.Unlock(); return current },
		ListSessions:     func() []controlruntime.SessionMeta { return nil },
		SessionMessages:  func() []controlruntime.DisplayMessage { return nil },
		NewSession: func() (controlruntime.SessionMeta, error) {
			_ = selectSession("a2a_session")
			return controlruntime.SessionMeta{ID: "a2a_session"}, nil
		},
		ResumeSession: selectSession,
	}}
	rt := controlruntime.New(controlruntime.RunnerFunc(func(_ context.Context, _ string, host controlruntime.RunHost) (string, error) {
		host.Text("answer")
		return "answer", nil
	}), services)
	t.Cleanup(func() { _ = rt.Close() })
	handler, err := newDaemonHandler(rt, "127.0.0.1:8765", httpapi.Options{Token: "secret"}, true, "")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/a2a/message:send", strings.NewReader(`{"message":{"messageId":"integration","role":"ROLE_USER","parts":[{"text":"hello"}]}}`))
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Content-Type", "application/a2a+json")
	req.Header.Set("A2A-Version", "1.0")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	var result struct {
		Task *a2a.Task `json:"task"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK || result.Task == nil || result.Task.Status.State != a2a.TaskStateCompleted {
		t.Fatalf("response: %d %s", rec.Code, rec.Body.String())
	}
	if rt.CurrentSessionID() != "main" {
		t.Fatalf("main session changed: %s", rt.CurrentSessionID())
	}
	run, ok := rt.CurrentRun()
	if !ok || run.SessionID != "a2a_session" || run.Source.Kind != "a2a" || run.Output != "answer" {
		t.Fatalf("master run projection lost: %#v", run)
	}
	if len(result.Task.Artifacts) != 1 || result.Task.Artifacts[0].Parts[0].Text() != "answer" {
		t.Fatalf("A2A result lost: %#v", result.Task.Artifacts)
	}
}

func TestA2ARequiresExplicitEnablement(t *testing.T) {
	rt := controlruntime.New(controlruntime.RunnerFunc(func(context.Context, string, controlruntime.RunHost) (string, error) { return "done", nil }), controlruntime.ServiceGroups{})
	t.Cleanup(func() { _ = rt.Close() })
	for _, enabled := range []bool{false, true} {
		handler, err := newDaemonHandler(rt, "127.0.0.1:8765", httpapi.Options{Token: "secret"}, enabled, "")
		if err != nil {
			t.Fatal(err)
		}
		for _, path := range []string{"/.well-known/agent-card.json", "/a2a/tasks"} {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.Header.Set("Authorization", "Bearer secret")
			req.Header.Set("A2A-Version", "1.0")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			want := http.StatusNotFound
			if enabled {
				want = http.StatusOK
			}
			if rec.Code != want {
				t.Fatalf("A2A=%v path=%s: status=%d body=%s", enabled, path, rec.Code, rec.Body.String())
			}
		}
	}
	if _, err := newDaemonHandler(rt, "0.0.0.0:8765", httpapi.Options{}, false, ""); err != nil {
		t.Fatalf("default listener behavior changed: %v", err)
	}
	if _, err := newDaemonHandler(rt, "0.0.0.0:8765", httpapi.Options{}, true, ""); err == nil {
		t.Fatal("unauthenticated network A2A listener allowed")
	}
}
