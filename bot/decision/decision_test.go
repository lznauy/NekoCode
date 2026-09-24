package decision

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func jevTestServer(t *testing.T, status int, body string, seen *int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if seen != nil {
			*seen++
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
}

func TestPruneToolsBatchAndVerdicts(t *testing.T) {
	var requests int
	thresh := 0.2
	server := jevTestServer(t, http.StatusOK,
		`{"model":"jev-1.13.0","answers":{"q0":{"type":"noul","noul":0.05},"q1":{"type":"noul","noul":0.9},"q2":{"type":"noul","noul":0.3}}}`,
		&requests)
	defer server.Close()

	scorer := NewToolPruner(JevSettings{APIKey: "k", BaseURL: server.URL, KeepThreshold: &thresh})
	prune, ok := scorer.PruneTools(context.Background(), "fix the login bug", []Candidate{
		{Tool: "shell", Head: "build failed: 3 errors"},
		{Tool: "read", Head: "package main"},
		{Tool: "grep", Head: "no matches"},
	})
	if !ok {
		t.Fatal("scorer reported unavailable")
	}
	if requests != 1 {
		t.Fatalf("expected one batched request, got %d", requests)
	}
	if len(prune) != 3 || prune[0] != true || prune[1] != false || prune[2] != false {
		t.Fatalf("verdicts = %v, want [true false false]", prune)
	}
}

func TestPruneToolsDedupesRepeatedInput(t *testing.T) {
	var requests int
	server := jevTestServer(t, http.StatusOK,
		`{"answers":{"q0":{"type":"noul","noul":0.05}}}`, &requests)
	defer server.Close()

	scorer := NewToolPruner(JevSettings{APIKey: "k", BaseURL: server.URL})
	cands := []Candidate{{Tool: "shell", Head: "same output"}}
	if _, ok := scorer.PruneTools(context.Background(), "goal", cands); !ok {
		t.Fatal("first call should succeed")
	}
	if _, ok := scorer.PruneTools(context.Background(), "goal", cands); !ok {
		t.Fatal("second call should be served from cache")
	}
	if requests != 1 {
		t.Fatalf("cache miss on identical input: %d requests", requests)
	}
}

func TestPruneToolsFailsOpen(t *testing.T) {
	cases := map[string]struct {
		status int
		body   string
	}{
		"http 500":          {http.StatusInternalServerError, `{"error":"boom"}`},
		"unauthorized":      {http.StatusUnauthorized, `{}`},
		"malformed json":    {http.StatusOK, `not-json`},
		"missing answer":    {http.StatusOK, `{"answers":{}}`},
		"wrong answer type": {http.StatusOK, `{"answers":{"q0":{"type":"choice","choice":"x"}}}`},
	}
	for name, tc := range cases {
		server := jevTestServer(t, tc.status, tc.body, nil)
		scorer := NewToolPruner(JevSettings{APIKey: "k", BaseURL: server.URL})
		prune, ok := scorer.PruneTools(context.Background(), "goal", []Candidate{{Tool: "shell", Head: "out"}})
		server.Close()
		if ok || prune != nil {
			t.Fatalf("%s: expected fail-open (nil, false), got prune=%v ok=%v", name, prune, ok)
		}
	}
}

func TestPruneToolsUnreachableServerFailsOpenFast(t *testing.T) {
	scorer := NewToolPruner(JevSettings{APIKey: "k", BaseURL: "http://127.0.0.1:1/v1/systemone", Timeout: 500 * time.Millisecond})
	start := time.Now()
	_, ok := scorer.PruneTools(context.Background(), "goal", []Candidate{{Tool: "shell", Head: "out"}})
	if ok {
		t.Fatal("expected fail-open")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("connection error should fail fast, took %s", elapsed)
	}
}

func TestNewToolPrunerNilWithoutKey(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "")
	if NewToolPruner(JevSettings{}) != nil {
		t.Fatal("no key anywhere: expected nil pruner")
	}
	t.Setenv("TYPESAFE_API_KEY", "from-env")
	if NewToolPruner(JevSettings{}) == nil {
		t.Fatal("env key should construct a pruner")
	}
}

func TestEnabledSwitchWinsOverKeyPresence(t *testing.T) {
	enabled := false
	if NewToolPruner(JevSettings{APIKey: "k", Enabled: &enabled}) != nil {
		t.Fatal("enabled=false must disable the pruner even with a key")
	}
	if NewRiskJudge(JevSettings{APIKey: "k", Enabled: &enabled}) != nil {
		t.Fatal("enabled=false must disable the risk judge even with a key")
	}
	on := true
	if NewToolPruner(JevSettings{APIKey: "k", Enabled: &on}) == nil {
		t.Fatal("enabled=true must keep the pruner")
	}
	// nil (unset) keeps the legacy default: enabled when a key is present.
	if NewToolPruner(JevSettings{APIKey: "k"}) == nil {
		t.Fatal("unset enabled must default to on")
	}
}
