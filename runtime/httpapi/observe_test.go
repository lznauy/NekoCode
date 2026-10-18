package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"nekocode/protocol"
	controlruntime "nekocode/runtime"
)

func observeHandler(t *testing.T) http.Handler {
	t.Helper()
	rt := controlruntime.New(controlruntime.RunnerFunc(func(_ context.Context, input string, host controlruntime.RunHost) (string, error) {
		if input == "fail" {
			host.Step(protocol.StepEvent{Action: protocol.StepActionToolBlocked, ToolName: "shell", CallID: "c2", IsError: true})
			return "", errors.New("boom")
		}
		host.Step(protocol.StepEvent{Action: protocol.StepActionToolStart, ToolName: "read", CallID: "c1"})
		host.Step(protocol.StepEvent{Action: protocol.StepActionExecuteTool, ToolName: "read", CallID: "c1"})
		return "ok", nil
	}), controlruntime.Services{
		Metrics: func() controlruntime.MetricsSnapshot { return controlruntime.MetricsSnapshot{PromptTokens: 10} },
		ContextSnapshot: func() controlruntime.ContextSnapshot {
			return controlruntime.ContextSnapshot{Budget: 100, Used: 25, Free: 75}
		},
		MemoryView: func(controlruntime.MemoryScope) controlruntime.MemoryView { return controlruntime.MemoryView{} },
		CurrentModel: func() controlruntime.ModelSelection {
			return controlruntime.ModelSelection{Provider: "openai", Model: "gpt-test"}
		},
		ModelOptions: func() ([]controlruntime.ModelOption, string) {
			return []controlruntime.ModelOption{{Name: "default", Model: "gpt-test"}}, "default"
		},
		ToolNames: func() []string { return []string{"read", "shell"} },
	})
	t.Cleanup(func() { _ = rt.Close() })
	// An older failure followed by success exercises filtering before limit.
	for _, input := range []controlruntime.Input{{Text: "fail", Source: controlruntime.SourceRef{Kind: "headless"}}, {Text: "ok", Source: controlruntime.SourceRef{Kind: "telegram"}}} {
		id, err := rt.StartRun(context.Background(), input)
		if err != nil {
			t.Fatal(err)
		}
		if err := rt.WaitRun(context.Background(), id); err != nil {
			t.Fatal(err)
		}
	}
	return New(rt).Handler()
}

func doGet(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func getJSON(t *testing.T, h http.Handler, path string) map[string]any {
	t.Helper()
	rec := doGet(t, h, path)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s status = %d body=%s", path, rec.Code, rec.Body)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body
}

func TestObserveOverviewAggregatesRuns(t *testing.T) {
	h := observeHandler(t)
	body := getJSON(t, h, "/observe/overview")
	runs := body["runs"].(map[string]any)
	if runs["total"].(float64) != 2 || runs["done"].(float64) != 1 || runs["failed"].(float64) != 1 {
		t.Fatalf("runs = %v", runs)
	}
	if runs["tools"].(float64) != 2 || runs["tool_errors"].(float64) != 1 {
		t.Fatalf("tool stats = %v", runs)
	}
	if body["window"] != "retained" {
		t.Fatalf("window = %v", body["window"])
	}
	if body["capabilities"].(map[string]any)["metrics"] != true {
		t.Fatalf("capabilities = %v", body["capabilities"])
	}
	if body["metrics"].(map[string]any)["promptTokens"].(float64) != 10 {
		t.Fatalf("metrics = %v", body["metrics"])
	}
	if body["context"].(map[string]any)["budget"].(float64) != 100 {
		t.Fatalf("context = %v", body["context"])
	}
}

// The JSON overview and the Prometheus exposition fold the same run window, so
// the counters they both publish must agree. A divergence would mean two
// integrations reporting different numbers for the same runtime.
//
// The comparison covers only the metrics this surface actually emits; the
// exposition is a subset of the overview, not a full mirror.
func TestObserveOverviewMatchesPrometheus(t *testing.T) {
	h := observeHandler(t)

	overview := getJSON(t, h, "/observe/overview")
	runs := overview["runs"].(map[string]any)
	metrics, _ := overview["metrics"].(map[string]any)

	resp := doGet(t, h, "/observe/metrics")
	body := resp.Body.String()

	want := map[string]string{
		"nekocode_runs_done_window":    num(runs["done"]),
		"nekocode_runs_failed_window":  num(runs["failed"]),
		"nekocode_tool_errors_window":  num(runs["tool_errors"]),
		"nekocode_turn_input_tokens":   num(metrics["turnInput"]),
		"nekocode_compactions_session": num(metrics["compactCount"]),
	}
	for name, value := range want {
		if !strings.Contains(body, name+" "+value+"\n") {
			t.Errorf("%s does not match overview value %s\n%s", name, value, body)
		}
	}
}

// num renders a JSON number the way the Prometheus encoder does.
func num(v any) string {
	f, _ := v.(float64)
	return strconv.FormatInt(int64(f), 10)
}

func TestObserveRunsFilters(t *testing.T) {
	h := observeHandler(t)
	body := getJSON(t, h, "/observe/runs?state=failed")
	runs := body["runs"].([]any)
	if len(runs) != 1 || runs[0].(map[string]any)["state"] != "failed" {
		t.Fatalf("failed runs = %v", runs)
	}
	body = getJSON(t, h, "/observe/runs?source=telegram")
	runs = body["runs"].([]any)
	if len(runs) != 1 || runs[0].(map[string]any)["state"] != "done" {
		t.Fatalf("telegram runs = %v", runs)
	}
	run := runs[0].(map[string]any)
	if run["tools"].(float64) != 1 || run["state"] != "done" {
		t.Fatalf("run summary = %v", run)
	}
	if _, ok := run["input"]; ok {
		t.Fatalf("summary must not expose run input: %v", run)
	}
}

func TestObserveRunDetailOmitsToolIO(t *testing.T) {
	h := observeHandler(t)
	listed := getJSON(t, h, "/observe/runs?source=telegram")["runs"].([]any)
	id := listed[0].(map[string]any)["id"].(string)
	body := getJSON(t, h, "/observe/runs/"+id)
	run := body["run"].(map[string]any)
	if run["state"] != "done" {
		t.Fatalf("run = %v", run)
	}
	tools := body["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("tools = %v", tools)
	}
	tool := tools[0].(map[string]any)
	if tool["name"] != "read" || tool["status"] != "done" {
		t.Fatalf("tool = %v", tool)
	}
	for _, leak := range []string{"args", "output", "input"} {
		if _, ok := tool[leak]; ok {
			t.Fatalf("tool detail leaks %q: %v", leak, tool)
		}
	}

	if rec := doGet(t, h, "/observe/runs/missing"); rec.Code != http.StatusNotFound {
		t.Fatalf("missing run status = %d", rec.Code)
	}
}

func TestObservePrometheus(t *testing.T) {
	h := observeHandler(t)
	rec := doGet(t, h, "/observe/metrics")
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("content type = %q", ct)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"nekocode_runtime_state 0",
		"nekocode_runs_done_window 1",
		"nekocode_runs_failed_window 1",
		"nekocode_tool_errors_window 1",
		"nekocode_turn_input_tokens 0",
		"# TYPE nekocode_compactions_session gauge",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in:\n%s", want, body)
		}
	}
}

func TestObserveRunsFiltersBeforeLimiting(t *testing.T) {
	// Older failures must survive a small limit even when newer runs succeed.
	h := observeHandler(t)
	body := getJSON(t, h, "/observe/runs?state=failed&limit=1")
	runs := body["runs"].([]any)
	if len(runs) != 1 || runs[0].(map[string]any)["state"] != "failed" {
		t.Fatalf("filtered runs = %v", runs)
	}
	body = getJSON(t, h, "/observe/runs?limit=1")
	if runs := body["runs"].([]any); len(runs) != 1 {
		t.Fatalf("limit not applied: %v", runs)
	}
}

func TestObserveModelsAndTools(t *testing.T) {
	h := observeHandler(t)
	models := getJSON(t, h, "/observe/models")
	if len(models["models"].([]any)) != 1 || models["active"] != "default" {
		t.Fatalf("models = %v", models)
	}
	tools := getJSON(t, h, "/observe/tools")
	if got := tools["tools"].([]any); len(got) != 2 || got[0] != "read" {
		t.Fatalf("tools = %v", got)
	}
}

func TestCORSPreflightAndHeaders(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := WithOptions(inner, Options{Token: "secret", AllowOrigin: "https://dash.example"})

	req := httptest.NewRequest(http.MethodOptions, "/observe/overview", nil)
	req.Header.Set("Origin", "https://dash.example")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d (preflight must bypass auth)", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "https://dash.example" {
		t.Fatalf("preflight headers = %v", rec.Header())
	}

	req = httptest.NewRequest(http.MethodGet, "/observe/overview?token=secret", nil)
	req.Header.Set("Origin", "https://dash.example")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("query-token status = %d", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "https://dash.example" {
		t.Fatalf("response missing CORS header: %v", rec.Header())
	}

	req = httptest.NewRequest(http.MethodGet, "/observe/overview?token=secret", nil)
	req.Header.Set("Origin", "https://evil.example")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("disallowed origin must not receive CORS headers: %v", rec.Header())
	}

	req = httptest.NewRequest(http.MethodGet, "/observe/overview", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing token status = %d", rec.Code)
	}

	// A query token must not authorize a state-changing request, and a
	// mutating response must not be CORS-readable.
	req = httptest.NewRequest(http.MethodPost, "/input?token=secret", nil)
	req.Header.Set("Origin", "https://dash.example")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("query token authorized POST: status = %d", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("mutating response carried CORS headers: %v", rec.Header())
	}
}
