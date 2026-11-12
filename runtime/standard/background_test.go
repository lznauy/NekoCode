package standard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"nekocode/bot/config"
	"nekocode/bot/core"
	"nekocode/bot/extension/tool/runtime/taskbridge"
	"nekocode/bot/session"
	"nekocode/bot/worktree"
	"nekocode/mode"
	controlruntime "nekocode/runtime"
)

func TestSuperAgentBackgroundRealBotAndAutomaticReview(t *testing.T) {
	for _, general := range []bool{false, true} {
		name := "read_tools"
		if general {
			name = "general_worktrees"
		}
		t.Run(name, func(t *testing.T) { testSuperAgentBackgroundRealBot(t, general, false) })
	}
	t.Run("general_dirty_snapshots", func(t *testing.T) { testSuperAgentBackgroundRealBot(t, true, true) })
}

func testSuperAgentBackgroundRealBot(t *testing.T, general, dirty bool) {
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(root, "evidence.txt")
	if err := os.WriteFile(path, []byte("real worker evidence"), 0600); err != nil {
		t.Fatal(err)
	}
	if general {
		for _, args := range [][]string{{"init"}, {"config", "user.name", "Fixture"}, {"config", "user.email", "fixture@example.invalid"}, {"add", "."}, {"commit", "-m", "baseline"}} {
			if out, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); err != nil {
				t.Fatalf("git: %s %v", out, err)
			}
		}
	}
	parentContent := "real worker evidence"
	if dirty {
		parentContent += " with uncommitted changes"
		if err := os.WriteFile(path, []byte(parentContent), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "new.txt"), []byte("parent untracked file"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	scope := "read_tools_only"
	if general {
		scope = "general"
	}
	releaseA, releaseB := make(chan struct{}), make(chan struct{})
	startedA, startedB := make(chan struct{}), make(chan struct{})
	reviews := make(chan string, 4)
	var mainCalls, workerA, workerB atomic.Int32
	toolCall := func(name string, args any) map[string]any {
		data, _ := json.Marshal(args)
		return map[string]any{"index": 0, "id": fmt.Sprint(name, time.Now().UnixNano()), "type": "function", "function": map[string]any{"name": name, "arguments": string(data)}}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages json.RawMessage `json:"messages"`
			Tools    []struct {
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			return
		}
		child := false
		for _, tool := range req.Tools {
			if tool.Function.Name == "nekocode_submit_result" {
				child = true
			}
		}
		var delta map[string]any
		finish := "stop"
		if child {
			for _, tool := range req.Tools {
				if !general && (tool.Function.Name == "shell" || tool.Function.Name == "write" || tool.Function.Name == "capability") {
					t.Errorf("read-only executor exposed %s", tool.Function.Name)
				}
			}
			if general && !strings.Contains(string(req.Messages), "/worktrees/") {
				t.Error("general executor did not receive isolated workspace")
			}
			marker := "A"
			counter, started, release := &workerA, startedA, releaseA
			if strings.Contains(string(req.Messages), "WORK_B") {
				marker = "B"
				counter, started, release = &workerB, startedB, releaseB
			}
			step := counter.Add(1)
			if step == 1 {
				close(started)
				select {
				case <-release:
				case <-r.Context().Done():
					return
				}
				delta = map[string]any{"tool_calls": []any{toolCall("read", map[string]any{"path": "evidence.txt", "startLine": 1, "endLine": 1})}}
			} else if general && step == 2 {
				delta = map[string]any{"tool_calls": []any{toolCall("write", map[string]any{"path": "evidence.txt", "content": "modified " + marker + "\n"})}}
			} else {
				if dirty && !strings.Contains(string(req.Messages), parentContent) {
					t.Error("worker read old committed code")
				}
				if !strings.Contains(string(req.Messages), "real worker evidence") {
					t.Error("worker did not execute the real read tool")
				}
				delta = map[string]any{"tool_calls": []any{toolCall("nekocode_submit_result", map[string]any{"summary": "VERIFIED_" + marker, "evidence": []string{"read evidence.txt"}, "files": []string{}, "verification": "Read actual fixture", "unfinished": []string{}, "risks": []string{}})}}
			}
			finish = "tool_calls"
		} else {
			for _, tool := range req.Tools {
				switch tool.Function.Name {
				case "task_result", "task_submit", "task_cancel", "task_list", "agent_profiles", "question", "todo_write":
				default:
					t.Errorf("coordinator exposed execution tool %q", tool.Function.Name)
				}
			}
			switch mainCalls.Add(1) {
			case 1:
				delta = map[string]any{"tool_calls": []any{toolCall("task_submit", map[string]any{"tool_scope": scope, "required_tools": []string{"read"}, "title": "A", "prompt": "WORK_A read " + path})}}
				finish = "tool_calls"
			case 2:
				if !strings.Contains(string(req.Messages), "acceptance receipt") {
					t.Error("task_submit did not return acceptance receipt")
				}
				delta = map[string]any{"content": "A dispatched"}
			case 3:
				delta = map[string]any{"tool_calls": []any{toolCall("task_submit", map[string]any{"tool_scope": scope, "required_tools": []string{"read"}, "title": "B", "prompt": "WORK_B read " + path})}}
				finish = "tool_calls"
			case 4:
				delta = map[string]any{"content": "B dispatched"}
			default:
				marker := "A"
				if strings.Contains(string(req.Messages), "VERIFIED_B") && workerA.Load() < 2 {
					marker = "B"
				}
				reviews <- marker
				if !strings.Contains(string(req.Messages), "[Background task results]") {
					t.Error("review was not activated by a completion notification")
				}
				delta = map[string]any{"content": "Reviewed " + marker}
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, choice := range []any{map[string]any{"index": 0, "delta": delta}, map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": finish}} {
			data, _ := json.Marshal(map[string]any{"choices": []any{choice}})
			fmt.Fprintf(w, "data: %s\n\n", data)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()
	if err := config.Save(config.Config{Active: "local/test", Providers: []config.ProviderConfig{{Name: "local", APIKey: "test-only", BaseURL: server.URL + "/v1", Protocol: "openai", Models: []config.ModelDefinition{{Name: "test", Model: "test"}}}}}); err != nil {
		t.Fatal(err)
	}
	bot, err := core.New()
	if err != nil {
		t.Fatal(err)
	}
	assertFixtureModel(t, bot, "local/test", server.URL+"/v1")
	if err := bot.SetInteractionMode(mode.SuperAgent); err != nil {
		t.Fatal(err)
	}
	if general {
		bot.SetFullAccess(true)
	}
	rt := FromBot(bot)
	defer rt.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	await := func(ch <-chan struct{}) {
		select {
		case <-ch:
		case <-ctx.Done():
			t.Fatalf("worker did not start: main=%d; %s", mainCalls.Load(), runtimeTestDiagnostic(rt))
		}
	}
	a, err := rt.StartRun(ctx, controlruntime.Input{Text: "Investigate A"})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.WaitRun(ctx, a); err != nil {
		t.Fatal(err)
	}
	await(startedA)
	if current, ok := rt.CurrentRun(); !ok || current.ID != a {
		t.Fatalf("background events replaced current foreground run: %+v", current)
	}
	b, err := rt.StartRun(ctx, controlruntime.Input{Text: "Investigate B independently"})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.WaitRun(ctx, b); err != nil {
		t.Fatal(err)
	}
	await(startedB)
	if general {
		tasks, err := rt.BackgroundTasks("")
		if err != nil || len(tasks) != 2 {
			t.Fatalf("missing tasks: %+v %v", tasks, err)
		}
		for _, task := range tasks {
			if task.Isolation != "worktree" || task.Status != "running" {
				t.Fatalf("general writers failed to overlap: %+v", tasks)
			}
		}
	}
	close(releaseB)
	select {
	case marker := <-reviews:
		if marker != "B" {
			t.Fatalf("wrong review %s", marker)
		}
	case <-ctx.Done():
		t.Fatal("B did not activate coordinator")
	}
	close(releaseA)
	select {
	case marker := <-reviews:
		if marker != "A" {
			t.Fatalf("wrong review %s", marker)
		}
	case <-ctx.Done():
		t.Fatal("A did not activate coordinator")
	}
	if mainCalls.Load() != 6 {
		t.Fatalf("unexpected parent turn count %d", mainCalls.Load())
	}
	if general {
		content, err := os.ReadFile(path)
		if err != nil || string(content) != parentContent {
			t.Fatalf("worker changed parent workspace: %q %v", content, err)
		}
		items, err := worktree.List(bot.CurrentSessionID())
		if err != nil || len(items) != 2 {
			t.Fatalf("worktrees not retained: %+v %v", items, err)
		}
		seen := map[string]bool{}
		for _, item := range items {
			page, err := session.ReadSubagentResult(ctx, bot.CurrentSessionID(), taskbridge.ResultQuery{ID: item.TaskID, Limit: 20})
			if err != nil || page.WorkspaceBase != item.Base || page.SourceHead != item.SourceHead {
				t.Fatalf("result lost host-supplied workspace baseline: %+v %v", page, err)
			}
			if item.Snapshot != dirty {
				t.Fatalf("wrong snapshot mode: %+v", item)
			}
			if dirty {
				if content, err := os.ReadFile(filepath.Join(item.Path, "new.txt")); err != nil || string(content) != "parent untracked file" {
					t.Fatalf("snapshot omitted new file: %q %v", content, err)
				}
			}
			content, err := os.ReadFile(filepath.Join(item.Path, "evidence.txt"))
			if err != nil {
				t.Fatal(err)
			}
			seen[string(content)] = true
		}
		if !seen["modified A\n"] || !seen["modified B\n"] {
			t.Fatalf("parallel edits were not isolated: %v", seen)
		}
	}
}
