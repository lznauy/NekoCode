package decision

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEnvironmentKeyIsSent(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "env-test-key")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer env-test-key" {
			t.Errorf("authorization = %q", got)
		}
		w.Write([]byte(`{"answers":{"q0":{"type":"noul","noul":0.9}}}`))
	}))
	defer server.Close()
	NewRiskJudge(JevSettings{BaseURL: server.URL}).Dangerous(context.Background(), "example")
}

func TestMalformedScoresNeverApproveOrPrune(t *testing.T) {
	for _, score := range []string{"", `,"noul":null`, `,"noul":-1`, `,"noul":1.1`} {
		t.Run(score, func(t *testing.T) {
			server := jevTestServer(t, http.StatusOK, `{"answers":{"q0":{"type":"noul"`+score+`}}}`, nil)
			defer server.Close()
			settings := JevSettings{APIKey: "test", BaseURL: server.URL}
			if _, ok := NewRiskJudge(settings).Dangerous(context.Background(), "example"); ok {
				t.Error("malformed score accepted for approval")
			}
			if _, ok := NewToolPruner(settings).PruneTools(context.Background(), "goal", []Candidate{{Tool: "shell", Head: "output"}}); ok {
				t.Error("malformed score accepted for pruning")
			}
		})
	}
}

func TestLongCommandCannotHideDangerousSuffix(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request jevRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		score := "0"
		if strings.Contains(request.Questions["q0"].Instructions, "-delete") {
			score = "1"
		}
		w.Write([]byte(`{"answers":{"q0":{"type":"noul","noul":` + score + `}}}`))
	}))
	defer server.Close()
	judge := NewRiskJudge(JevSettings{APIKey: "test", BaseURL: server.URL})
	if dangerous, ok := judge.Dangerous(context.Background(), "find . "+strings.Repeat(" ", 1000)+"-delete"); ok && !dangerous {
		t.Fatal("approved a command whose destructive suffix was hidden")
	}
}
