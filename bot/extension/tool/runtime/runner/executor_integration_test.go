package runner_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nekocode/bot/checkpoint"
	"nekocode/bot/contextmgr"
	"nekocode/bot/extension/tool"
	"nekocode/bot/extension/tool/builtin/filesystem/search"
	writebuiltin "nekocode/bot/extension/tool/builtin/filesystem/write"
	"nekocode/bot/extension/tool/builtin/shell"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/permission"
	"nekocode/bot/extension/tool/runtime/runner"
	"nekocode/bot/provider/types"
	"nekocode/bot/session"
	"nekocode/protocol"
)

type testTool struct{ name string }

func (t *testTool) Name() string                                    { return t.name }
func (t *testTool) Description() string                             { return "test" }
func (t *testTool) Parameters() []core.Parameter                    { return nil }
func (t *testTool) ExecutionMode(map[string]any) core.ExecutionMode { return core.ModeParallel }
func (t *testTool) Execute(context.Context, map[string]any) (core.ToolOutput, error) {
	return core.ToolOutput{Text: "ok"}, nil
}

func TestGrepArtifactSurvivesContextBudgetAndSessionReload(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cwd := t.TempDir()
	source := filepath.Join(cwd, "evidence.txt")
	if err := os.WriteFile(source, []byte(strings.Repeat("needle evidence\n", 4000)), 0600); err != nil {
		t.Fatal(err)
	}
	sessions := session.New(cwd)
	dir, err := session.ToolOutputDir(sessions.CurrentID())
	if err != nil {
		t.Fatal(err)
	}
	store := core.NewOutputStore(dir)
	registry := tools.New(&search.GrepTool{})
	registry.Workspace().Configure(cwd, nil)
	executor := runner.NewExecutor(registry)
	executor.ExecutionState().OutputStore = store
	result := executor.ExecuteBatch(context.Background(), []core.ToolCallItem{{ID: "grep", Name: "grep", Args: map[string]any{"pattern": "needle", "path": source}}})[0]
	if result.Error != "" || result.Artifact.Path == "" || result.Artifact.Incomplete {
		t.Fatalf("grep failed: %+v", result)
	}
	manager := contextmgr.New(contextmgr.Config{ContextWindow: 10000, SystemPrompt: strings.Repeat("x", 28000)})
	manager.SetOutputStore(store)
	manager.AddToolResultsBatch([]contextmgr.ToolResultMsg{{ToolName: "grep", AllowOutputFile: executor.AllowOutputFile, Message: types.Message{
		Content: result.TextOutput().Text, ToolOutput: result.Artifact, ToolCallID: result.ID,
	}}})
	snapshot := sessions.Current()
	snapshot.CaptureContext(manager.Snapshot(), 0, 0, nil)
	if err := sessions.Save(snapshot); err != nil {
		t.Fatal(err)
	}
	loaded, err := sessions.Load(snapshot.ID)
	if err != nil {
		t.Fatal(err)
	}
	restored := contextmgr.New(contextmgr.Config{ContextWindow: 10000})
	restored.SetOutputStore(core.NewOutputStore(dir))
	restored.Restore(loaded.ContextSnapshot())
	msg := restored.Snapshot().Messages[0]
	if msg.ToolOutput != result.Artifact || len(msg.Content) >= len(result.EffectiveOutput()) {
		t.Fatal("context rebudgeting lost the original artifact")
	}
	files, err := filepath.Glob(filepath.Join(dir, "result-*.txt"))
	if err != nil || len(files) != 1 {
		t.Fatalf("preview was saved again: %v %v", files, err)
	}
	raw, err := os.ReadFile(msg.ToolOutput.Path)
	if err != nil || !strings.Contains(string(raw), "2000:needle evidence") || !strings.Contains(string(raw), "4000:needle evidence") || strings.Contains(string(raw), "Tool output truncated") {
		t.Fatalf("saved result lost middle/tail evidence: %v", err)
	}
}

type fileMutationTool struct{ testTool }

func (t *fileMutationTool) ExecutionMode(map[string]any) core.ExecutionMode {
	return core.ModeSequential
}

func (t *fileMutationTool) Execute(_ context.Context, args map[string]any) (core.ToolOutput, error) {
	path, _ := args["path"].(string)
	content, _ := args["content"].(string)
	return core.ToolOutput{Text: "changed"}, os.WriteFile(path, []byte(content), 0o644)
}

func TestExecutorCheckpointsWriteBeforeMutation(t *testing.T) {
	work := t.TempDir()
	path := filepath.Join(work, "main.go")
	if err := os.WriteFile(path, []byte("before"), 0o644); err != nil {
		t.Fatal(err)
	}
	cp := checkpoint.New(t.TempDir())
	cp.Activate("session", nil, 0)
	if _, err := cp.BeginMessage("session", "Write a file"); err != nil {
		t.Fatal(err)
	}

	r := tools.New()
	r.Workspace().Configure(work, nil)
	r.Register(&writebuiltin.WriteTool{})
	e := runner.NewExecutor(r)
	e.ExecutionState().Checkpoints = cp
	result := e.ExecuteBatch(context.Background(), []core.ToolCallItem{{
		ID: "1", Name: "write", Args: map[string]any{"path": path, "content": "after"},
	}})[0]
	if result.Error != "" {
		t.Fatalf("write failed: %+v", result)
	}
	if _, err := cp.Rewind("session", ""); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "before" {
		t.Fatalf("rewound content = %q err=%v", data, err)
	}
}

func TestExecutorDoesNotCheckpointShellSideEffects(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shell.txt")
	if err := os.WriteFile(path, []byte("before"), 0o644); err != nil {
		t.Fatal(err)
	}
	cp := checkpoint.New(t.TempDir())
	cp.Activate("session", nil, 0)
	if _, err := cp.BeginMessage("session", "Run a shell mutation"); err != nil {
		t.Fatal(err)
	}
	r := tools.New(&fileMutationTool{testTool{name: "shell"}})
	e := runner.NewExecutor(r)
	e.ExecutionState().Checkpoints = cp
	e.SetConfirmFn(func(protocol.ConfirmRequest) protocol.ConfirmReply { return protocol.AllowOnce() })
	result := e.ExecuteBatch(context.Background(), []core.ToolCallItem{{
		ID: "1", Name: "shell", Args: map[string]any{
			"command": "mutate", "path": path, "content": "after",
		},
	}})[0]
	if result.Error != "" {
		t.Fatalf("shell mutation failed: %+v", result)
	}
	rewound, err := cp.Rewind("session", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(rewound.Changes) != 0 {
		t.Fatalf("shell side effect was checkpointed: %+v", rewound)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "after" {
		t.Fatalf("shell side effect changed during rewind: %q err=%v", data, err)
	}
}

type writeTool struct{ testTool }

func (t *writeTool) ExecutionMode(map[string]any) core.ExecutionMode { return core.ModeSequential }

func TestExecutorBatch(t *testing.T) {
	r := tools.New()
	r.Register(&testTool{name: "read"})
	r.Register(&testTool{name: "safe"})
	r.Register(&writeTool{testTool{name: "writer"}})
	e := runner.NewExecutor(r)

	// Empty batch.
	results := e.ExecuteBatch(context.Background(), nil)
	if len(results) != 0 {
		t.Error("expected empty results")
	}

	// Safe tool runs.
	results = e.ExecuteBatch(context.Background(), []core.ToolCallItem{
		{ID: "2", Name: "safe"},
	})
	if results[0].Error != "" || results[0].Output != "ok" {
		t.Errorf("unexpected result: %+v", results[0])
	}
}

func TestExecutorBatchPreservesCallOrderAcrossModes(t *testing.T) {
	r := tools.New()
	r.Register(&testTool{name: "read"})
	r.Register(&writeTool{testTool{name: "write"}})
	e := runner.NewExecutor(r)

	results := e.ExecuteBatch(context.Background(), []core.ToolCallItem{
		{ID: "1", Name: "write", Args: map[string]any{"path": "a.go"}},
		{ID: "2", Name: "read", Args: map[string]any{"path": "a.go"}},
		{ID: "3", Name: "write", Args: map[string]any{"path": "b.go"}},
	})

	for i, wantID := range []string{"1", "2", "3"} {
		if results[i].ID != wantID {
			t.Fatalf("result %d has ID %q, want %q; results=%+v", i, results[i].ID, wantID, results)
		}
	}
}

func TestExecutorPlanMode(t *testing.T) {
	r := tools.New()
	r.Register(&writeTool{testTool{name: "writer"}})
	r.RegisterWithOptions(&testTool{name: "read"}, tools.RegistrationOptions{PlanAllowed: true})
	e := runner.NewExecutor(r)
	e.SetPlanMode(true)

	results := e.ExecuteBatch(context.Background(), []core.ToolCallItem{
		{ID: "1", Name: "writer"},
	})
	if results[0].Error == "" {
		t.Error("expected plan mode block")
	}
	results = e.ExecuteBatch(context.Background(), []core.ToolCallItem{{ID: "2", Name: "read"}})
	if results[0].Error != "" || results[0].Output != "ok" {
		t.Errorf("expected plan mode read to run: %+v", results[0])
	}
}

func TestExecutorConfirm(t *testing.T) {
	r := tools.New()
	r.Register(&writeTool{testTool{name: "writer"}})
	e := runner.NewExecutor(r)
	e.SetPermissionPolicy(permission.PermissionsDecl{Ask: []string{"writer"}}, "/repo", "/home/user")

	e.SetConfirmFn(func(req protocol.ConfirmRequest) protocol.ConfirmReply { return protocol.Deny() })

	results := e.ExecuteBatch(context.Background(), []core.ToolCallItem{
		{ID: "1", Name: "writer"},
	})
	if results[0].Error == "" {
		t.Error("expected confirm denial")
	}
}

func TestExecutorShellRunAndProcessWait(t *testing.T) {
	r := tools.New()
	shellTool := &shell.ShellTool{}
	r.Register(shellTool)
	r.Register(shell.NewProcessTool(shellTool))
	e := runner.NewExecutor(r)
	e.SetPermissionPolicy(permission.PermissionsDecl{}, "/repo", "/home/user")
	prompts := 0
	e.SetConfirmFn(func(req protocol.ConfirmRequest) protocol.ConfirmReply {
		prompts++
		if req.Kind != protocol.ConfirmKindPermission {
			t.Fatalf("unexpected confirm kind: %+v", req)
		}
		if req.ToolName != "shell" {
			t.Fatalf("shell command approval should display shell, got %+v", req)
		}
		if req.Args["command"] != "sleep 2.2 && echo executor-shell" {
			t.Fatalf("shell command approval should show actual command, got %+v", req.Args)
		}
		if prompts > 1 {
			t.Fatalf("plain shell command should only need command approval, got prompt %d: %+v", prompts, req)
		}
		if req.Approval != nil && req.Approval.Combined {
			t.Fatalf("plain shell command should not expose merged capability escalation: %+v", req)
		}
		return protocol.ConfirmReply{Allowed: true}
	})

	start := e.ExecuteBatch(context.Background(), []core.ToolCallItem{{
		ID:   "1",
		Name: "shell",
		Args: map[string]any{
			"command":    "sleep 2.2 && echo executor-shell",
			"name":       "executor-test",
			"timeout_ms": 5000,
		},
	}})[0]
	if start.Error != "" {
		t.Fatalf("shell run failed: %+v", start)
	}
	if !strings.Contains(start.Output, "task: executor-test") {
		t.Fatalf("run output missing task name: %q", start.Output)
	}

	wait := e.ExecuteBatch(context.Background(), []core.ToolCallItem{{
		ID:   "2",
		Name: "process",
		Args: map[string]any{"action": "wait", "task": "executor-test"},
	}})[0]
	if wait.Error != "" {
		t.Fatalf("process wait failed: %+v", wait)
	}
	if !strings.Contains(wait.Output, "executor-shell") || !strings.Contains(wait.Output, "status: done") {
		t.Fatalf("wait output missing command result: %q", wait.Output)
	}
}

func TestExecutorShellRunAppliesBashDenyRules(t *testing.T) {
	r := tools.New()
	r.Register(&shell.ShellTool{})
	e := runner.NewExecutor(r)
	e.SetPermissionPolicy(permission.PermissionsDecl{}, "/repo", "/home/user")
	e.SetConfirmFn(func(req protocol.ConfirmRequest) protocol.ConfirmReply {
		t.Fatalf("denied shell command should not prompt: %+v", req)
		return protocol.Deny()
	})

	got := e.ExecuteBatch(context.Background(), []core.ToolCallItem{{
		ID:   "1",
		Name: "shell",
		Args: map[string]any{
			"command": "sudo echo blocked",
		},
	}})[0]
	if got.Error == "" {
		t.Fatalf("expected shell run to be denied")
	}
	if !strings.Contains(got.Error, "denied by rule shell") {
		t.Fatalf("error = %q, want shell deny rule", got.Error)
	}
}

func TestExecutorDoesNotOwnReadBeforeWriteGovernance(t *testing.T) {
	r := tools.New()
	r.Register(&testTool{name: "read"})
	r.Register(&writeTool{testTool{name: "write"}})
	e := runner.NewExecutor(r)

	// Read-before-write is governed by agent ledger policy, not executor-local
	// state. The executor must not reject a call solely because it lacks local
	// read history, otherwise bash/read evidence recorded in the ledger is ignored.
	results := e.ExecuteBatch(context.Background(), []core.ToolCallItem{
		{ID: "1", Name: "write", Args: map[string]any{"path": "existing.go"}},
	})
	if results[0].Error != "" {
		t.Errorf("executor should not apply read-before-write policy: %s", results[0].Error)
	}
}
