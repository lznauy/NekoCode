package standard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"nekocode/bot/config"
	"nekocode/bot/core"
	"nekocode/bot/extension/tool/runtime/taskbridge"
	"nekocode/mode"
	controlruntime "nekocode/runtime"
)

func TestCoordinatorReadsFullResultAndSharesPinnedInputWithWorker(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("HOME", filepath.Join(root, "home"))
	sourceBody := strings.Repeat("审查证据与分析。", 900) + "PINNED_REPORT_END"
	done := make(chan struct{}, 1)
	var mu sync.Mutex
	stage := 0
	sourceRef := ""
	sourceID := ""
	assembled := ""
	consumerStarted := false
	call := func(name string, args any) map[string]any {
		raw, _ := json.Marshal(args)
		return map[string]any{"index": 0, "id": fmt.Sprint(name, time.Now().UnixNano()), "type": "function", "function": map[string]any{"name": name, "arguments": string(raw)}}
	}
	handoff := func(summary string) map[string]any {
		return call("nekocode_submit_result", map[string]any{"summary": summary, "evidence": []string{"fixture report"}, "files": []string{}, "verification": "Read pinned task_result pages", "unfinished": []string{}, "risks": []string{}})
	}
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
			Tools []struct {
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		visible := map[string]bool{}
		for _, tool := range request.Tools {
			visible[tool.Function.Name] = true
		}
		raw, _ := json.Marshal(request.Messages)
		text := string(raw)
		mu.Lock()
		defer mu.Unlock()
		var delta map[string]any
		finish := "stop"
		if visible["nekocode_submit_result"] {
			if strings.Contains(text, "CONSUME_INPUT") {
				if !visible["task_result"] || visible["shell"] || visible["task_submit"] {
					t.Error("consumer result scope or read-only tools wrong")
				}
				if !strings.Contains(text, sourceRef) {
					t.Error("shared result reference missing from worker context")
				}
				var latest taskbridge.ResultPage
				for _, message := range request.Messages {
					if message.Role == "tool" {
						var page taskbridge.ResultPage
						if json.Unmarshal([]byte(message.Content), &page) == nil && page.Ref == sourceRef {
							latest = page
						}
					}
				}
				if latest.Ref == "" {
					delta = map[string]any{"tool_calls": []any{call("task_result", map[string]any{"result_ref": sourceRef, "limit": 2000})}}
				} else if latest.HasMore {
					delta = map[string]any{"tool_calls": []any{call("task_result", map[string]any{"result_ref": sourceRef, "offset": latest.NextOffset, "limit": 2000})}}
				} else {
					if !strings.Contains(text, "PINNED_REPORT_END") {
						t.Error("consumer never read the complete report")
					}
					delta = map[string]any{"tool_calls": []any{handoff("consumer verified full report")}}
				}
			} else {
				if visible["task_result"] {
					t.Error("worker without inputs received sibling result access")
				}
				delta = map[string]any{"tool_calls": []any{handoff(sourceBody)}}
			}
			finish = "tool_calls"
		} else {
			if !visible["task_result"] || visible["read"] || visible["shell"] {
				t.Error("coordinator result/execution policy wrong")
			}
			switch stage {
			case 0:
				delta = map[string]any{"tool_calls": []any{call("task_submit", map[string]any{"prompt": "PRODUCE_SOURCE", "tool_scope": "read_tools_only", "required_tools": []string{}})}}
				finish = "tool_calls"
				stage = 1
			case 1:
				_, tail, ok := strings.Cut(text, "[Subagent ID: ")
				if !ok {
					t.Error("source receipt missing")
				}
				sourceID, _, _ = strings.Cut(tail, "]")
				delta = map[string]any{"content": "Source dispatched"}
				stage = 2
			case 2:
				delta = map[string]any{"tool_calls": []any{call("task_result", map[string]any{"id": sourceID, "limit": 2000})}}
				finish = "tool_calls"
				stage = 3
			case 3:
				var page taskbridge.ResultPage
				for _, message := range request.Messages {
					if message.Role == "tool" {
						var candidate taskbridge.ResultPage
						if json.Unmarshal([]byte(message.Content), &candidate) == nil && candidate.ID == sourceID {
							page = candidate
						}
					}
				}
				if page.Ref == "" {
					t.Error("parent did not receive structured full result")
				}
				sourceRef = page.Ref
				assembled += page.Content
				if page.HasMore {
					delta = map[string]any{"tool_calls": []any{call("task_result", map[string]any{"result_ref": sourceRef, "offset": page.NextOffset, "limit": 2000})}}
				} else {
					if !strings.Contains(assembled, sourceBody) {
						t.Error("coordinator result pages omitted report content")
					}
					delta = map[string]any{"tool_calls": []any{call("task_submit", map[string]any{"prompt": "CONSUME_INPUT", "isolation": "shared", "tool_scope": "read_tools_only", "required_tools": []string{"task_result"}, "inputs": []string{sourceRef}})}}
					stage = 4
					consumerStarted = true
				}
				finish = "tool_calls"
			case 4:
				delta = map[string]any{"content": "Consumer dispatched"}
				stage = 5
			default:
				if !strings.Contains(text, "consumer verified full report") {
					t.Error("consumer completion not delivered")
				}
				delta = map[string]any{"content": "Both reports verified"}
				select {
				case done <- struct{}{}:
				default:
				}
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		first, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": delta}}})
		last, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": finish}}})
		fmt.Fprintf(w, "data: %s\n\ndata: %s\n\ndata: [DONE]\n\n", first, last)
	}))
	defer model.Close()
	if err := config.Save(config.Config{Active: "local/test", Providers: []config.ProviderConfig{{Name: "local", APIKey: "fixture", BaseURL: model.URL + "/v1", Protocol: "openai", Models: []config.ModelDefinition{{Name: "test", Model: "test"}}}}}); err != nil {
		t.Fatal(err)
	}
	bot, err := core.New()
	if err != nil {
		t.Fatal(err)
	}
	assertFixtureModel(t, bot, "local/test", model.URL+"/v1")
	if err := bot.SetInteractionMode(mode.SuperAgent); err != nil {
		t.Fatal(err)
	}
	rt := FromBot(bot)
	defer rt.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	id, err := rt.StartRun(ctx, controlruntime.Input{Text: "Coordinate report handoff"})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.WaitRun(ctx, id); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatalf("handoff did not finish: %s", runtimeTestDiagnostic(rt))
	}
	mu.Lock()
	defer mu.Unlock()
	if !consumerStarted || sourceRef == "" {
		t.Fatal("handoff not exercised")
	}
	tasks, err := rt.BackgroundTasks("")
	if err != nil || len(tasks) != 2 {
		t.Fatalf("unexpected extraction-only tasks: %d %v", len(tasks), err)
	}
}
