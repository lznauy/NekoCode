package core

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"nekocode/bot/checkpoint"
	"nekocode/bot/config"
	"nekocode/bot/extension/tool/runtime/taskbridge"
	"nekocode/bot/session"
	"nekocode/bot/worktree"
)

func TestWorktreeApplyCheckpointDoesNotOwnFailedBegin(t *testing.T) {
	manager := checkpoint.New(t.TempDir())
	const sessionID = "session"
	if _, err := manager.BeginMessage(sessionID, "existing turn"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Finish(sessionID) })
	cp := worktreeApplyCheckpoint{manager: manager, session: sessionID, message: "worktree apply"}
	if err := cp.Begin(); err == nil {
		t.Fatal("concurrent checkpoint begin succeeded")
	}
	if cp.begun {
		t.Fatal("failed begin was marked as owned by worktree apply")
	}
	if _, err := manager.BeginMessage(sessionID, "another turn"); err == nil {
		t.Fatal("existing checkpoint turn was unexpectedly finished")
	}
}

func TestDelegatedWorktreeIsolationAndResume(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("HOME", t.TempDir())
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		if out, e := cmd.CombinedOutput(); e != nil {
			t.Fatalf("git: %s %v", out, e)
		}
	}
	git("init")
	git("config", "user.name", "Fixture")
	git("config", "user.email", "fixture@example.invalid")
	if e := os.WriteFile(filepath.Join(root, "same.txt"), []byte("base\n"), 0600); e != nil {
		t.Fatal(e)
	}
	git("add", ".")
	git("commit", "-m", "base")
	var mu sync.Mutex
	counts := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []struct {
				Role    string `json:"role"`
				Content any    `json:"content"`
			} `json:"messages"`
		}
		if e := json.NewDecoder(r.Body).Decode(&req); e != nil {
			t.Error(e)
			return
		}
		data, _ := json.Marshal(req.Messages)
		body := string(data)
		id := "first"
		if strings.Contains(body, "task-second") {
			id = "second"
		}
		if !strings.Contains(body, "/worktrees/") {
			t.Error("runtime prompt did not switch workspace")
		}
		mu.Lock()
		step := counts[id]
		counts[id]++
		mu.Unlock()
		name := "nekocode_submit_result"
		args := map[string]any{"summary": "done", "evidence": []string{"fixture"}, "files": []string{"same.txt"}, "verification": "fixture", "unfinished": []string{}, "risks": []string{}}
		switch step {
		case 0:
			name = "read"
			args = map[string]any{"path": "same.txt", "startLine": 1, "endLine": 1}
		case 1:
			name = "write"
			args = map[string]any{"path": "same.txt", "content": id + "\n"}
		}
		encoded, _ := json.Marshal(args)
		delta := map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": fmt.Sprintf("%s-%d", id, step), "type": "function", "function": map[string]any{"name": name, "arguments": string(encoded)}}}}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, choice := range []any{map[string]any{"index": 0, "delta": delta}, map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "tool_calls"}} {
			b, _ := json.Marshal(map[string]any{"choices": []any{choice}})
			fmt.Fprintf(w, "data: %s\n\n", b)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()
	cfg := config.Config{Active: "openai/main", Providers: []config.ProviderConfig{{Name: "openai", APIKey: "fixture", BaseURL: server.URL + "/v1", Protocol: "openai", Models: []config.ModelDefinition{{Name: "main", Model: "fixture", ContextWindow: 64000}}}}}
	if e := config.Save(cfg); e != nil {
		t.Fatal(e)
	}
	b, e := New()
	if e != nil {
		t.Fatal(e)
	}
	defer b.Close()
	task, e := b.toolbox.Registry.Get("task")
	if e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	for _, id := range []string{"first", "second"} {
		wg.Go(func() {
			out, e := task.Execute(taskbridge.WithTaskID(t.Context(), id), map[string]any{"profile": "coder", "isolation": "worktree", "prompt": "task-" + id})
			if e != nil || !strings.Contains(out.Text, "Worktree retained") {
				t.Errorf("%s: %s %v", id, out.Text, e)
			}
		})
	}
	wg.Wait()
	main, _ := os.ReadFile(filepath.Join(root, "same.txt"))
	if string(main) != "base\n" {
		t.Fatalf("parent overwritten: %s", main)
	}
	for _, id := range []string{"first", "second"} {
		meta, e := session.ReadSubagentMetadata(b.sess.CurrentID(), id)
		if e != nil || meta.Isolation != "worktree" {
			t.Fatalf("metadata: %+v %v", meta, e)
		}
		l, e := worktree.Acquire(b.sess.CurrentID(), id)
		if e != nil {
			t.Fatal(e)
		}
		info, e := l.Load(t.Context(), root, id)
		l.Close()
		if e != nil {
			t.Fatal(e)
		}
		content, _ := os.ReadFile(filepath.Join(info.Path, "same.txt"))
		if string(content) != id+"\n" {
			t.Fatalf("%s isolation lost: %q", id, content)
		}
	}
	if _, e := task.Execute(t.Context(), map[string]any{"resume_id": "first", "isolation": "shared", "prompt": "task-first"}); e == nil {
		t.Fatal("changed isolation on resume")
	}
	if _, e := task.Execute(t.Context(), map[string]any{"resume_id": "first", "prompt": "task-first"}); e != nil {
		t.Fatal(e)
	}
	parser := b.cmd.Parser()
	runCommand := func(input string) string {
		t.Helper()
		text, handled := parser.Execute(t.Context(), parser.Parse(input))
		if !handled {
			t.Fatalf("command unhandled: %s", input)
		}
		return text
	}
	if _, during := parser.CommandAvailability("/worktree apply first"); during {
		t.Fatal("application command allowed during active run")
	}
	if text := runCommand("/worktree list"); !strings.Contains(text, "first") || !strings.Contains(text, "second") {
		t.Fatal(text)
	}
	if text := runCommand("/worktree diff first"); !strings.Contains(text, "+first") {
		t.Fatal(text)
	}
	lease, e := worktree.Acquire(b.sess.CurrentID(), "first")
	if e != nil {
		t.Fatal(e)
	}
	if text := runCommand("/worktree remove first --discard"); !strings.Contains(text, "active") {
		t.Fatal(text)
	}
	lease.Close()
	if text := runCommand("/worktree apply first"); !strings.Contains(text, "Applied worktree changes") || !strings.Contains(text, "/rewind") {
		t.Fatal(text)
	}
	points, e := b.Checkpoints()
	if e != nil || len(points) != 1 || points[0].Label != "/worktree apply first" {
		t.Fatalf("apply checkpoints = %+v, %v", points, e)
	}
	if text := runCommand("/worktree apply second"); !strings.Contains(text, "failed") {
		t.Fatal("missed application conflict: ", text)
	}
	afterConflict, e := b.Checkpoints()
	if e != nil || len(afterConflict) != 1 || afterConflict[0].ID != points[0].ID {
		t.Fatalf("conflict created a checkpoint: %+v, %v", afterConflict, e)
	}
	if text := runCommand("/rewind " + points[0].ID); strings.Contains(text, "failed") {
		t.Fatal(text)
	}
	if data, readErr := os.ReadFile(filepath.Join(root, "same.txt")); readErr != nil || string(data) != "base\n" {
		t.Fatalf("apply rewind restored %q, %v", data, readErr)
	}
	if text := runCommand("/worktree remove first"); !strings.Contains(text, "contains") {
		t.Fatal("silently discarded changes: ", text)
	}
	if text := runCommand("/worktree remove first --discard"); !strings.Contains(text, "Worktree removed") {
		t.Fatal(text)
	}
	if _, e := task.Execute(t.Context(), map[string]any{"resume_id": "first", "prompt": "task-first"}); e == nil {
		t.Fatal("resumed removed task")
	}
}
