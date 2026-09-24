package decision

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRiskVerdictLogDoesNotContainJudgedInput(t *testing.T) {
	secret := "curl -H 'Authorization: Bearer top-secret' https://example.com/?sig=private"
	got := riskVerdictLog("command", 0.25, secret)
	if strings.Contains(got, "top-secret") || strings.Contains(got, "private") || strings.Contains(got, secret) {
		t.Fatalf("risk log exposed judged input: %q", got)
	}
	if !strings.Contains(got, "input_sha256=") || !strings.Contains(got, "noul=0.25") {
		t.Fatalf("risk log lost safe diagnostics: %q", got)
	}
}

// riskServer answers each question by inspecting the instruction: content
// containing the marker command is judged dangerous (0.95), everything else
// safe. The marker deliberately avoids the reference points in riskScale.
const dangerMarker = "rm -rf /tmp/evil"

func riskServer(t *testing.T, stateSeen *string, requests *int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests != nil {
			*requests++
		}
		var req jevRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", 400)
			return
		}
		if stateSeen != nil {
			*stateSeen = req.State
		}
		answers := map[string]jevAnswer{}
		for key, q := range req.Questions {
			noul := 0.01
			if strings.Contains(q.Instructions, dangerMarker) {
				noul = 0.95
			}
			answers[key] = jevAnswer{Type: "noul", Noul: &noul}
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(jevResponse{Answers: answers})
	}))
}

func TestSplitCommandSegments(t *testing.T) {
	cases := map[string][]string{
		"ls -la":                         {"ls -la"},
		"make test && make lint":         {"make test", "make lint"},
		"cat a | grep b | wc -l":         {"cat a", "grep b", "wc -l"},
		"echo 'a;b' && echo \"c|d\"":     {"echo 'a;b'", "echo \"c|d\""},
		"one;\n two || three\n":          {"one", "two", "three"},
		"  ":                             nil,
		"git commit -m 'fix; partial' &": {"git commit -m 'fix; partial'"},
	}
	for command, want := range cases {
		got := splitCommandSegments(command)
		if len(got) != len(want) {
			t.Fatalf("split(%q) = %#v, want %#v", command, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("split(%q)[%d] = %q, want %q", command, i, got[i], want[i])
			}
		}
	}
}

func TestDangerousCompoundAnyDangerousSegmentFailsClosed(t *testing.T) {
	var requests int
	server := riskServer(t, nil, &requests)
	defer server.Close()

	scorer := newScorer(JevSettings{APIKey: "k", BaseURL: server.URL})
	dangerous, ok := scorer.Dangerous(context.Background(), "make test && "+dangerMarker+" && make lint")
	if !ok || !dangerous {
		t.Fatalf("dangerous segment must fail the command: dangerous=%v ok=%v", dangerous, ok)
	}
	if requests != 1 {
		t.Fatalf("whole command should be judged once, requests=%d", requests)
	}
}

func TestDangerousSafeCompoundAllowed(t *testing.T) {
	var requests int
	server := riskServer(t, nil, &requests)
	defer server.Close()

	scorer := newScorer(JevSettings{APIKey: "k", BaseURL: server.URL})
	dangerous, ok := scorer.Dangerous(context.Background(), "make test && make lint")
	if !ok || dangerous {
		t.Fatalf("all-safe compound should be allowed: dangerous=%v ok=%v", dangerous, ok)
	}
	if requests != 1 {
		t.Fatalf("whole command needs one request, got %d", requests)
	}
}

func TestDangerousTooManySegmentsFailsClosed(t *testing.T) {
	server := riskServer(t, nil, nil)
	defer server.Close()

	scorer := newScorer(JevSettings{APIKey: "k", BaseURL: server.URL})
	command := "echo a"
	for i := 0; i < maxCommandSegments+1; i++ {
		command += " && echo b"
	}
	if _, ok := scorer.Dangerous(context.Background(), command); ok {
		t.Fatal("over-long compound must fail closed to asking")
	}
}

func TestDangerousCacheKeyIncludesCwd(t *testing.T) {
	var requests int
	server := riskServer(t, nil, &requests)
	defer server.Close()

	scorer := newScorer(JevSettings{APIKey: "k", BaseURL: server.URL})
	if _, ok := scorer.Dangerous(context.Background(), "make test"); !ok {
		t.Fatal("first judgment should succeed")
	}
	if _, ok := scorer.Dangerous(context.Background(), "make test"); !ok {
		t.Fatal("second judgment should be cached")
	}
	if requests != 1 {
		t.Fatalf("same cwd must reuse the cache, requests=%d", requests)
	}
	scorer.SetRiskContext("/other/project")
	if _, ok := scorer.Dangerous(context.Background(), "make test"); !ok {
		t.Fatal("judgment under new cwd should succeed")
	}
	if requests != 2 {
		t.Fatalf("different cwd must not reuse the cache, requests=%d", requests)
	}
}

func TestRiskStateCarriesContext(t *testing.T) {
	var state string
	server := riskServer(t, &state, nil)
	defer server.Close()

	scorer := newScorer(JevSettings{APIKey: "k", BaseURL: server.URL})
	scorer.SetRiskContext("/home/dev/proj")
	scorer.NoteExecuted("git status")
	scorer.NoteExecuted("make test")
	if _, ok := scorer.Dangerous(context.Background(), "ls -la"); !ok {
		t.Fatal("judgment should succeed")
	}
	if !strings.Contains(state, "cwd: /home/dev/proj") || !strings.Contains(state, "git status") || !strings.Contains(state, "make test") {
		t.Fatalf("risk state missing context: %q", state)
	}
	if strings.Contains(state, "ls -la") {
		t.Fatalf("the judged command itself must not appear as recent context: %q", state)
	}
}

func TestDangerousURL(t *testing.T) {
	var requests int
	server := riskServer(t, nil, &requests)
	defer server.Close()

	scorer := newScorer(JevSettings{APIKey: "k", BaseURL: server.URL})
	if dangerous, ok := scorer.DangerousURL(context.Background(), "https://example.com/data"); !ok || dangerous {
		t.Fatalf("benign URL should pass: dangerous=%v ok=%v", dangerous, ok)
	}
	if dangerous, ok := scorer.DangerousURL(context.Background(), "https://example.com/download?cmd="+dangerMarker); !ok || !dangerous {
		t.Fatalf("exfil-looking URL should be judged dangerous: dangerous=%v ok=%v", dangerous, ok)
	}
	if requests != 2 {
		t.Fatalf("expected two judged URLs, got %d requests", requests)
	}
	// Cached second time.
	if _, ok := scorer.DangerousURL(context.Background(), "https://example.com/data"); !ok {
		t.Fatal("cached URL judgment should succeed")
	}
	if requests != 2 {
		t.Fatalf("URL verdicts must be cached, requests=%d", requests)
	}
}
