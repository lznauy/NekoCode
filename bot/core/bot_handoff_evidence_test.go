package core

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"nekocode/bot/config"
	"nekocode/bot/extension/tool/runtime/refbridge"
	"nekocode/bot/extension/tool/runtime/taskbridge"
)

func TestTaskRunnerResolvesSubmittedEvidenceAfterFinalCheckpoint(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cwd := t.TempDir()
	path := filepath.Join(cwd, "fixture.txt")
	if err := os.WriteFile(path, []byte("fixture contents"), 0600); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	step := 0
	submittedID := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages []struct {
				Role    string `json:"role"`
				CallID  string `json:"tool_call_id"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			http.Error(w, "invalid fixture request", 400)
			return
		}
		mu.Lock()
		current := step
		step++
		mu.Unlock()
		name, id, args := "read", "read-call", map[string]any{"path": path, "startLine": 1, "endLine": 1}
		if current == 1 {
			name, id, args = "evidence", "list-call", map[string]any{"action": "list"}
		}
		if current >= 2 {
			evidenceID := ""
			for _, msg := range request.Messages {
				if msg.CallID == "list-call" {
					var page refbridge.Page
					if err := json.Unmarshal([]byte(msg.Content), &page); err != nil {
						t.Error(err)
					}
					for _, item := range page.Evidence {
						if item.Tool == "read" {
							evidenceID = item.ID
						}
					}
				}
			}
			if evidenceID == "" {
				t.Error("child did not discover its executed read receipt")
				http.Error(w, "missing fixture receipt", 500)
				return
			}
			mu.Lock()
			submittedID = evidenceID
			mu.Unlock()
			name, id = "nekocode_submit_result", "submit-call"
			args = map[string]any{"summary": "inspection done", "evidence": []string{"read fixture"}, "evidence_ids": []string{evidenceID}, "files": []string{}, "verification": "fixture read only", "unfinished": []string{}, "risks": []string{}}
		}
		encoded, _ := json.Marshal(args)
		delta := map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": id, "type": "function", "function": map[string]any{"name": name, "arguments": string(encoded)}}}}}}}
		data, _ := json.Marshal(delta)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", data)
	}))
	defer server.Close()
	cfg := config.Config{Active: "test/worker", Providers: []config.ProviderConfig{{Name: "test", Protocol: "openai", APIKey: "fixture", BaseURL: server.URL + "/v1", Models: []config.ModelDefinition{{Name: "worker", Model: "gpt-4o-mini", ContextWindow: 128000}}}}}
	if err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	b, err := NewAt(cwd)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	tool, err := b.toolbox.Registry.Get("task")
	if err != nil {
		t.Fatal(err)
	}
	output, err := tool.Execute(taskbridge.WithTaskID(referenceActorContext(t, b), "worker"), map[string]any{"profile": "explore", "prompt": "Read the fixture and submit its execution evidence."})
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	id := submittedID
	calls := step
	mu.Unlock()
	if calls != 3 || id == "" || !strings.Contains(output.Text, id) || !strings.Contains(output.Text, `"status":"resolved"`) || !strings.Contains(output.Text, `"execution_status":"success"`) || !strings.Contains(output.Text, `"attempt_id":`) {
		t.Fatalf("handoff not checked after commit: %s (calls=%d)", output.Text, calls)
	}
}
