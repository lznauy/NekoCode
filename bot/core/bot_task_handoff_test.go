package core

import (
	"encoding/json"
	"fmt"
	"nekocode/bot/config"
	"nekocode/bot/contextmgr"
	"nekocode/bot/extension/agentprofile"
	"nekocode/bot/extension/tool/runtime/taskbridge"
	"nekocode/bot/session"
	"nekocode/bot/worktree"
	"nekocode/mode"
	"nekocode/protocol"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestHandoffPinsPriorAttemptWithoutImportingHistory(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	child, err := session.OpenSubagent("parent", "source", "/project", false)
	if err != nil {
		t.Fatal(err)
	}
	if err = child.SetToolCeiling([]string{"read", "shell"}); err != nil {
		t.Fatal(err)
	}
	if err = child.Begin("executor", "investigate original issue", nil); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err = captureTaskHandoff(t.Context(), "parent", "source"); err == nil {
		t.Fatal("active source forked")
	}
	result := "counterevidence: cleanup retry works. " + strings.Repeat("evidence ", 500)
	if err = child.Checkpoint(contextmgr.ManagerSnapshot{}, "completed", result); err != nil {
		t.Fatal(err)
	}
	if err = child.Close(); err != nil {
		t.Fatal(err)
	}
	_, pinned, handoff, err := captureTaskHandoff(t.Context(), "parent", "source")
	if err != nil {
		t.Fatal(err)
	}
	if !pinned.HasMore || len([]rune(pinned.Content)) != 1500 || !strings.Contains(handoff, "counterevidence") || !strings.Contains(handoff, "OLD workspace/version") {
		t.Fatalf("bad bounded handoff: %+v", pinned)
	}
	child, err = session.OpenSubagent("parent", "source", "/project", true)
	if err != nil {
		t.Fatal(err)
	}
	if err = child.Begin("executor", "second attempt", nil); err != nil {
		t.Fatal(err)
	}
	if err = child.Checkpoint(contextmgr.ManagerSnapshot{}, "completed", "different result"); err != nil {
		t.Fatal(err)
	}
	if err = child.Close(); err != nil {
		t.Fatal(err)
	}
	_, _, nextHandoff, err := captureTaskHandoff(t.Context(), "parent", "source")
	if err != nil || !strings.Contains(nextHandoff, `"previous_goal":"second attempt"`) {
		t.Fatalf("handoff paired new result with stale goal: %s, %v", nextHandoff, err)
	}
	page, err := session.ReadSubagentResult(t.Context(), "parent", taskbridge.ResultQuery{Ref: pinned.Ref, Limit: 50})
	if err != nil || !strings.HasPrefix(page.Content, "counterevidence") {
		t.Fatalf("handoff drifted to later attempt: %+v, %v", page, err)
	}
	if _, _, _, err = captureTaskHandoff(t.Context(), "other-parent", "source"); err == nil {
		t.Fatal("foreign session evidence exposed")
	}
}

func TestForkUsesCurrentParentSnapshotAndKeepsSourceWorkspace(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	b := newPersistTestBot(t)
	b.ensureSessionIdentity()
	git := func(args ...string) {
		t.Helper()
		if out, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git: %s %v", out, err)
		}
	}
	git("init")
	git("config", "user.name", "Fixture")
	git("config", "user.email", "fixture@example.invalid")
	if err := os.WriteFile(filepath.Join(root, "version.txt"), []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-m", "baseline")
	parent := b.CurrentSessionID()
	lease, err := worktree.Acquire(parent, "source")
	if err != nil {
		t.Fatal(err)
	}
	old, err := lease.Open(t.Context(), root, "source", false)
	if err != nil {
		t.Fatal(err)
	}
	if err = lease.Close(); err != nil {
		t.Fatal(err)
	}
	child, err := session.OpenSubagent(parent, "source", root, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []func() error{
		func() error { return child.SetIsolation("worktree") },
		func() error { return child.SetGroupID("original-plan") },
		func() error { return child.SetWorkspaceKind("head") },
		func() error { return child.SetWorkspaceBaseline(old.Base, old.SourceHead) },
		func() error { return child.SetToolCeiling([]string{"read", "shell"}) },
		func() error { return child.Begin("executor", "original investigation", nil) },
		func() error {
			return child.Checkpoint(contextmgr.ManagerSnapshot{}, "completed", "source finding: retry works")
		},
		child.Close,
	} {
		if err := action(); err != nil {
			t.Fatal(err)
		}
	}
	if err = os.WriteFile(filepath.Join(root, "version.txt"), []byte("fixed"), 0600); err != nil {
		t.Fatal(err)
	}
	host := &isolationCaptureHost{}
	definition, _ := mode.Resolve(mode.SuperAgent)
	ctx := protocol.WithBackgroundSubmitter(mode.WithSnapshot(t.Context(), definition.Snapshot), host)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		var request struct {
			Messages json.RawMessage `json:"messages"`
			Tools    []struct {
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			http.Error(w, "bad request", 400)
			return
		}
		if !strings.Contains(string(request.Messages), "source finding: retry works") || !strings.Contains(string(request.Messages), "OLD workspace/version") {
			t.Error("prior finding or version warning missing from worker context")
		}
		for _, tool := range request.Tools {
			switch tool.Function.Name {
			case "read", "shell", "task_result", "nekocode_submit_result":
			default:
				t.Errorf("fork expanded execution ceiling: %s", tool.Function.Name)
			}
		}
		path := filepath.Join(os.Getenv("HOME"), ".nekocode", "worktrees", parent, host.work.ID, "tree", "version.txt")
		if data, err := os.ReadFile(path); err != nil || string(data) != "fixed" {
			t.Errorf("worker saw stale parent code: %q, %v", data, err)
		}
		args, _ := json.Marshal(map[string]any{"summary": "fixture handoff checked", "evidence": []string{"source evidence and new snapshot"}, "files": []string{}, "verification": "fixture", "unfinished": []string{}, "risks": []string{}})
		delta := map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "finish", "type": "function", "function": map[string]any{"name": "nekocode_submit_result", "arguments": string(args)}}}}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, choice := range []any{map[string]any{"index": 0, "delta": delta}, map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "tool_calls"}} {
			data, _ := json.Marshal(map[string]any{"choices": []any{choice}})
			fmt.Fprintf(w, "data: %s\n\n", data)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()
	model := config.ModelConfig{Name: "fixture", Model: "fixture", BaseURL: server.URL + "/v1", Protocol: "openai", APIKey: "fixture", ContextWindow: 64000}
	spec := taskbridge.TaskSpec{ForkID: "source", Isolation: "snapshot", Prompt: "verify the parent fixes", RequiredTools: []string{"shell"}, MaxSteps: 3}
	result, err := b.submitBackgroundTask(ctx, spec, model, b.Configuration(), nil, b.getAgent())
	if err != nil {
		t.Fatal(err)
	}
	if result.ID == "source" || host.work.GroupID != "original-plan" {
		t.Fatalf("fork overwrote source or lost demand ownership: %+v", host.work)
	}
	worker := &isolationTestWorker{}
	submitted, err := host.work.Run(protocol.WithExecutionIdentity(t.Context(), protocol.ExecutionIdentity{AttemptID: "runtime-attempt"}), worker)
	if err != nil {
		t.Fatal(err)
	}
	if submitted.Review.WorkspacePath == "" || submitted.Review.WorkspaceBase == "" || submitted.Review.SourceHead == "" {
		t.Fatalf("workspace baseline missing: %+v", submitted.Review)
	}
	if submitted.Review.Summary != "fixture handoff checked" || len(submitted.Review.Evidence) != 1 || submitted.Review.Verification != "fixture" {
		t.Fatalf("structured handoff lost: %+v", submitted.Review)
	}
	if requests.Load() != 1 {
		t.Fatalf("worker requests = %d, want 1", requests.Load())
	}
	if data, err := os.ReadFile(filepath.Join(old.Path, "version.txt")); err != nil || string(data) != "old" {
		t.Fatalf("source workspace changed: %q, %v", data, err)
	}
	metadata, err := session.ReadSubagentMetadata(parent, "source")
	if err != nil || metadata.RunCount != 1 {
		t.Fatalf("source attempt changed: %+v, %v", metadata, err)
	}
	host.work = protocol.BackgroundWork{}
	spec.RequiredTools = []string{"edit"}
	if _, err = b.submitBackgroundTask(ctx, spec, model, b.Configuration(), nil, b.getAgent()); err == nil || host.work.ID != "" {
		t.Fatal("expanded tool ceiling admitted")
	}
}

func TestTaskBudgetCannotRaiseConfiguredLimit(t *testing.T) {
	for _, tc := range []struct {
		name                                           string
		profile, max, defaultSteps, batchCeiling, want int
		invalid                                        bool
	}{
		{name: "sync default", want: 150},
		{name: "batch default", batchCeiling: 23, want: 23},
		{name: "batch caps oversized attempt", max: 80, batchCeiling: 23, want: 23},
		{name: "batch preserves smaller attempt", max: 5, batchCeiling: 23, want: 5},
		{name: "background default", defaultSteps: 12, want: 12},
		{name: "small resumed profile", profile: 3, defaultSteps: 12, want: 3},
		{name: "small requested limit", max: 3, defaultSteps: 12, want: 3},
		{name: "explicit replaces default", max: 30, defaultSteps: 12, want: 30},
		{name: "host ceiling", profile: 200, defaultSteps: 12, want: 12},
		{name: "model ceiling", profile: 200, max: 150, want: 150},
		{name: "model cannot request host maximum", profile: 200, max: 151, invalid: true},
		{name: "profile ceiling", profile: 3, max: 30, invalid: true},
		{name: "negative max", max: -1, invalid: true},
		{name: "invalid profile", profile: 201, invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			profile, err := resolveTaskStepBudget(agentprofile.Profile{MaxSteps: tc.profile}, taskbridge.TaskSpec{MaxSteps: tc.max}, tc.defaultSteps, tc.batchCeiling)
			if (err != nil) != tc.invalid {
				t.Fatalf("unexpected validation: %v", err)
			}
			if err == nil && profile.MaxSteps != tc.want {
				t.Fatalf("max=%d; want %d", profile.MaxSteps, tc.want)
			}
		})
	}
}
