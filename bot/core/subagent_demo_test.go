package core

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"nekocode/bot/config"
	"nekocode/protocol"
)

// Use the shipped plugin and standard Bot wiring, not a hand-wired Engine.
// Only the model is scripted; filesystem tools and completion handling are real.
func TestSubagentDemoEndToEnd(t *testing.T) {
	t.Run("default_skills", func(t *testing.T) {
		runSubagentDemo(t, "subagent-demo/reviewer", nil)
	})
	t.Run("alias_and_duplicate_skills", func(t *testing.T) {
		runSubagentDemo(t, "reviewer", []string{"check", "check"})
	})
}

func runSubagentDemo(t *testing.T, profile string, skills []string) {
	t.Helper()
	fixture, err := filepath.Abs("../../examples/subagent-demo")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("HOME", filepath.Join(root, "home"))
	pluginDir := filepath.Join(root, ".nekocode", "plugins", "subagent-demo")
	if err := os.CopyFS(pluginDir, os.DirFS(fixture)); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "sample.go")
	const source = "package sample\n\nconst Answer = 42\n"
	if err := os.WriteFile(path, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages []struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
			Tools []struct {
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			http.Error(w, "invalid request", 400)
			return
		}
		messages, _ := json.Marshal(request.Messages)
		contains := func(want string) {
			if !strings.Contains(string(messages), want) {
				t.Errorf("request %d missing %q", calls.Load(), want)
			}
		}
		tool := func(index int, id, name string, args any) map[string]any {
			data, _ := json.Marshal(args)
			return map[string]any{"index": index, "id": id, "type": "function", "function": map[string]any{"name": name, "arguments": string(data)}}
		}
		var delta map[string]any
		finish := "tool_calls"
		switch calls.Add(1) {
		case 1:
			delta = map[string]any{"tool_calls": []any{tool(0, "catalog", "agent_profiles", map[string]any{})}}
		case 2:
			contains("subagent-demo/reviewer")
			args := map[string]any{"profile": profile, "prompt": "Read and review " + path}
			if skills != nil {
				args["skills"] = skills
			}
			delta = map[string]any{"tool_calls": []any{tool(0, "delegate", "task", args)}}
		case 3:
			contains("只读代码审查")
			contains("Skill: check")
			for _, entry := range request.Tools {
				switch entry.Function.Name {
				case "read", "grep", "glob", "nekocode_submit_result":
				default:
					t.Errorf("unexpected subagent tool: %s", entry.Function.Name)
				}
			}
			if len(request.Tools) != 4 {
				t.Errorf("subagent tool count=%d, want 4", len(request.Tools))
			}
			delta = map[string]any{"tool_calls": []any{
				tool(0, "read-source", "read", map[string]any{"path": path, "startLine": 1, "endLine": 3}),
				tool(1, "denied-shell", "shell", map[string]any{"command": "echo unexpected-shell"}),
			}}
		case 4:
			contains("Answer = 42")
			contains("disabled by sub-agent profile")
			delta = map[string]any{"tool_calls": []any{tool(0, "handoff", "nekocode_submit_result", map[string]any{
				"summary":  "Read sample.go: Answer = 42; no code changes.",
				"evidence": []string{"sample.go:3 defines Answer = 42"}, "files": []string{path},
				"verification": "Read the file; shell was blocked; no tests executed by the subagent.", "unfinished": []string{}, "risks": []string{},
			})}}
		case 5:
			contains("Read sample.go: Answer = 42")
			contains("shell was blocked")
			finish = "stop"
			delta = map[string]any{"content": "Demo complete: Answer = 42; read-only delegation verified."}
		default:
			t.Error("unexpected extra model request")
			finish = "stop"
			delta = map[string]any{"content": "Unexpected model request."}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, choice := range []any{map[string]any{"index": 0, "delta": delta}, map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": finish}} {
			data, _ := json.Marshal(map[string]any{"id": "demo", "object": "chat.completion.chunk", "choices": []any{choice}})
			fmt.Fprintf(w, "data: %s\n\n", data)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()
	if err := config.Save(config.Config{Active: "demo", Models: []config.ModelConfig{{Name: "demo", Provider: "openai", Protocol: "openai", Model: "demo", APIKey: "local-test-only", BaseURL: server.URL + "/v1"}}}); err != nil {
		t.Fatal(err)
	}
	b, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := b.Close(); err != nil {
			t.Error(err)
		}
	})
	if _, err := b.ext.AgentProfile("subagent-demo/reviewer"); err != nil {
		t.Fatal(err)
	}
	host := &subagentDemoHost{}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	out, err := b.Run(ctx, "Use agent_profiles, then delegate a read-only review of "+path, host)
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 5 || !strings.Contains(out, "Demo complete") {
		t.Fatalf("calls=%d output=%q", calls.Load(), out)
	}
	host.mu.Lock()
	defer host.mu.Unlock()
	var id string
	var starts int
	var read, blocked, ended bool
	for _, event := range host.events {
		switch event.Action {
		case protocol.StepActionSubAgentStart:
			starts++
			if event.SubAgentProfile != "subagent-demo/reviewer" || event.SubAgentType != "subagent-demo/reviewer" {
				t.Errorf("unresolved profile in start event: %+v", event)
			}
			if len(event.SubAgentSkills) != 1 || event.SubAgentSkills[0] != "check" {
				t.Errorf("resolved skills = %v, want [check]", event.SubAgentSkills)
			}
			id = event.SubAgentID
		case protocol.StepActionExecuteTool:
			if event.ToolName == "read" && event.SubAgentID == id && id != "" && !event.IsError && strings.Contains(event.Output, "Answer = 42") {
				read = true
			}
		case protocol.StepActionToolBlocked:
			if event.ToolName == "shell" && event.SubAgentID == id && id != "" {
				blocked = true
			}
		case protocol.StepActionSubAgentEnd:
			ended = event.SubAgentID == id && id != ""
		}
	}
	if starts != 1 || id == "" || !read || !blocked || !ended {
		t.Fatalf("lifecycle: id=%q read=%v blocked=%v ended=%v", id, read, blocked, ended)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != source {
		t.Fatal("demo source changed", err)
	}
	t.Log("PASS: manifest → catalog → task → isolated subagent → real read + blocked shell → structured handoff → parent result")
}

type subagentDemoHost struct {
	mu     sync.Mutex
	events []protocol.StepEvent
}

func (*subagentDemoHost) Text(string)               {}
func (*subagentDemoHost) Reason(string)             {}
func (*subagentDemoHost) Phase(string)              {}
func (*subagentDemoHost) Todos([]protocol.TodoItem) {}
func (*subagentDemoHost) Confirm(protocol.ConfirmRequest) protocol.ConfirmReply {
	return protocol.Deny()
}
func (*subagentDemoHost) Ask(protocol.QuestionRequest) protocol.QuestionReply {
	return protocol.QuestionReply{}
}
func (h *subagentDemoHost) Step(ev protocol.StepEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.events = append(h.events, ev)
}
