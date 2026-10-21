package agent

import (
	"context"
	"encoding/json"
	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/extension/skill"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/policy"
	"nekocode/protocol"
)

func TestApplyPostToolHooksForwardsStopResult(t *testing.T) {
	a := newTestAgent()
	a.deps.gov.Register(policy.Hook{
		Name:  "stop",
		Point: policy.PostToolBatch,
		On: func(policy.State) *policy.Result {
			stop := policy.StopCompleted
			return &policy.Result{Stop: &stop}
		},
	})

	results := a.deps.gov.RecordTools([]policy.ToolResult{{Name: "write"}})
	if !a.toolRunner.applyPolicyResults(results) {
		t.Fatal("expected PostToolBatch stop")
	}
	if a.run.stopReason != policy.StopCompleted {
		t.Fatalf("stopReason = %q, want %q", a.run.stopReason, policy.StopCompleted)
	}
}

func TestFilterToolCallsAppliesPreToolPolicyBlock(t *testing.T) {
	a := newTestAgent()
	a.deps.gov.Register(policy.Hook{
		Name:  "block-read",
		Point: policy.PreToolUse,
		On: func(s policy.State) *policy.Result {
			return &policy.Result{BlockTool: &policy.BlockTool{
				Tool:   "read",
				Reason: "read blocked",
			}}
		},
	})

	filtered := a.toolRunner.filterToolCalls([]core.ToolCallItem{
		{Name: "read", Args: map[string]any{"path": "x.go"}},
	})

	if len(filtered.Allowed) != 0 {
		t.Fatalf("allowed = %d, want 0", len(filtered.Allowed))
	}
	if got := filtered.Blocked[0]; got != "read blocked" {
		t.Fatalf("blocked reason = %q, want read blocked", got)
	}
}

func TestFilterToolCallsUsesEffectiveDelegatedIdentity(t *testing.T) {
	a := newTestAgent()
	a.deps.gov.Register(policy.Hook{
		Name:  "block-remote-delete",
		Point: policy.PreToolUse,
		On: func(s policy.State) *policy.Result {
			if s.Facts().Tool.Name != "mcp__github__delete_repository" {
				return nil
			}
			return &policy.Result{BlockTool: &policy.BlockTool{Reason: "remote delete blocked"}}
		},
	})

	filtered := a.toolRunner.filterToolCalls([]core.ToolCallItem{{
		Name:          "capability",
		Args:          map[string]any{"action": "call"},
		EffectiveName: "mcp__github__delete_repository",
		EffectiveArgs: map[string]any{"repo": "demo"},
	}})
	if len(filtered.Allowed) != 0 || filtered.Blocked[0] != "remote delete blocked" {
		t.Fatalf("delegated policy identity was lost: %+v", filtered)
	}
}

func TestRecordToolResultsUsesEffectiveDelegatedIdentity(t *testing.T) {
	a := newTestAgent()
	var gotName string
	var gotRepo any
	a.deps.gov.Register(policy.Hook{
		Name:  "capture-remote",
		Point: policy.PostToolUse,
		On: func(s policy.State) *policy.Result {
			gotName = s.Facts().Tool.Name
			gotRepo = s.Facts().Tool.Args["repo"]
			return nil
		},
	})
	calls := []core.ToolCallItem{{
		ID:            "1",
		Name:          "capability",
		Args:          map[string]any{"action": "call"},
		EffectiveName: "mcp__github__create_issue",
		EffectiveArgs: map[string]any{"repo": "demo"},
	}}
	a.toolRunner.recordToolResults(calls, nil, []core.ToolCallResult{{ID: "1", Name: "mcp__github__create_issue", Output: "ok"}})
	if gotName != "mcp__github__create_issue" || gotRepo != "demo" {
		t.Fatalf("post-tool identity = %q %v", gotName, gotRepo)
	}
}

func TestFilterToolCallsReadBeforeWriteBlockComesFromHook(t *testing.T) {
	a := newTestAgent()
	path := filepath.Join(t.TempDir(), "main.go")
	if err := os.WriteFile(path, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	filtered := a.toolRunner.filterToolCalls([]core.ToolCallItem{
		{Name: "write", Args: map[string]any{"path": path}},
	})

	if len(filtered.Allowed) != 0 {
		t.Fatalf("allowed = %d, want 0", len(filtered.Allowed))
	}
	if got := filtered.Blocked[0]; !strings.Contains(got, "ledger 中没有该文件的读取记录") {
		t.Fatalf("blocked reason = %q, want read-before-write hook reason", got)
	}
}

func TestFilterToolCallsAllowsWriteAfterRead(t *testing.T) {
	a := newTestAgent()
	path := filepath.Join(t.TempDir(), "main.go")
	if err := os.WriteFile(path, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.deps.gov.RecordTool(policy.ToolResult{
		Name: "read",
		Args: map[string]any{"path": path},
	})

	filtered := a.toolRunner.filterToolCalls([]core.ToolCallItem{
		{Name: "write", Args: map[string]any{"path": path}},
	})

	if len(filtered.Allowed) != 1 {
		t.Fatalf("allowed = %d, want write allowed after read; blocked=%v", len(filtered.Allowed), filtered.Blocked)
	}
}

func TestFilterToolCallsBlocksUnreadEditEvenWithLongAnchor(t *testing.T) {
	a := newTestAgent()
	path := filepath.Join(t.TempDir(), "main.go")
	if err := os.WriteFile(path, []byte("package main\n\nfunc main() {\n\tmessage := \"hello\"\n\tprintln(message)\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	filtered := a.toolRunner.filterToolCalls([]core.ToolCallItem{
		{Name: "edit", Args: map[string]any{
			"path": path,
			"oldString": strings.Join([]string{
				"package main",
				"",
				"func main() {",
				"\tmessage := \"hello\"",
				"\tprintln(message)",
				"}",
			}, "\n"),
			"newString": "package main\n",
		}},
	})

	if len(filtered.Allowed) != 0 {
		t.Fatalf("allowed = %d, want unread edit blocked", len(filtered.Allowed))
	}
	if got := filtered.Blocked[0]; !strings.Contains(got, "ledger 中没有该文件的读取记录") {
		t.Fatalf("blocked reason = %q, want read-before-write hook reason", got)
	}
}

func TestFilterToolCallsBlocksEditWithShortAnchor(t *testing.T) {
	a := newTestAgent()
	path := filepath.Join(t.TempDir(), "main.go")
	if err := os.WriteFile(path, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	filtered := a.toolRunner.filterToolCalls([]core.ToolCallItem{
		{Name: "edit", Args: map[string]any{
			"path":      path,
			"oldString": "main",
			"newString": "app",
		}},
	})

	if len(filtered.Allowed) != 0 {
		t.Fatalf("allowed = %d, want short unread edit blocked", len(filtered.Allowed))
	}
	if got := filtered.Blocked[0]; !strings.Contains(got, "ledger 中没有该文件的读取记录") {
		t.Fatalf("blocked reason = %q, want read-before-write hook reason", got)
	}
}

func TestFilterToolCallsAllowsWriteToNewFile(t *testing.T) {
	a := newTestAgent()
	path := filepath.Join(t.TempDir(), "new.go")

	filtered := a.toolRunner.filterToolCalls([]core.ToolCallItem{
		{Name: "write", Args: map[string]any{"path": path}},
	})

	if len(filtered.Allowed) != 1 {
		t.Fatalf("allowed = %d, want new file write allowed; blocked=%v", len(filtered.Allowed), filtered.Blocked)
	}
}

func TestFilterToolCallsAllowsEditAfterSuccessfulWrite(t *testing.T) {
	a := newTestAgent()
	path := filepath.Join(t.TempDir(), "main.go")
	a.deps.gov.RecordTool(policy.ToolResult{
		Name: "write",
		Args: map[string]any{"path": path},
	})

	filtered := a.toolRunner.filterToolCalls([]core.ToolCallItem{
		{Name: "edit", Args: map[string]any{"path": path, "oldString": "package main\n", "newString": "package main\n\nfunc main() {}\n"}},
	})

	if len(filtered.Allowed) != 1 {
		t.Fatalf("allowed = %d, want edit allowed after write; blocked=%v", len(filtered.Allowed), filtered.Blocked)
	}
}

func TestMergeResultsPreservesOriginalCallOrder(t *testing.T) {
	calls := []core.ToolCallItem{
		{ID: "1", Name: "read", Args: map[string]any{"path": "a.go"}},
		{ID: "2", Name: "write", Args: map[string]any{"path": "b.go"}},
		{ID: "3", Name: "shell", Args: map[string]any{"command": "go test ./..."}},
	}
	execResults := []core.ToolCallResult{
		{ID: "1", Name: "read", Output: "read ok"},
		{ID: "3", Name: "shell", Output: "shell ok"},
	}

	results := mergeResults(calls, map[int]string{1: "blocked"}, execResults)
	if len(results) != 3 {
		t.Fatalf("results = %d, want 3", len(results))
	}
	if results[0].Output != "read ok" || results[1].Error != "blocked" || results[2].Output != "shell ok" {
		t.Fatalf("unexpected result order: %+v", results)
	}
}

func TestEmitResultCallbacksUsesEffectiveOutput(t *testing.T) {
	var gotOutput string
	msgs := emitResultCallbacks(
		[]core.ToolCallItem{{ID: "1", Name: "read", Args: map[string]any{"path": "a.go"}}},
		nil,
		[]core.ToolCallResult{{ID: "1", Name: "read", Output: "ok"}},
		func(ev protocol.StepEvent) {
			gotOutput = ev.Output
		},
	)

	if len(msgs) != 1 || msgs[0].ToolCallID != "1" || msgs[0].Content != "ok" {
		t.Fatalf("messages = %+v, want one tool result message", msgs)
	}
	if gotOutput != "ok" {
		t.Fatalf("callback output = %q, want ok", gotOutput)
	}
}

func TestEmitResultCallbacksMarksErrors(t *testing.T) {
	msgs := emitResultCallbacks(
		[]core.ToolCallItem{{ID: "1", Name: "shell", Args: map[string]any{"command": "false"}}},
		nil,
		[]core.ToolCallResult{{ID: "1", Name: "shell", Error: "command failed: exit status 1"}},
		nil,
	)

	if len(msgs) != 1 || msgs[0].Content != "command failed: exit status 1" || !msgs[0].IsError {
		t.Fatalf("messages = %+v, want failed tool result with IsError", msgs)
	}
}

func TestEmitResultCallbacksSkipsBlockedUIEvents(t *testing.T) {
	var callbacks int
	msgs := emitResultCallbacks(
		[]core.ToolCallItem{{ID: "1", Name: "edit", Args: map[string]any{"path": "x.go"}}},
		map[int]string{0: "blocked by policy"},
		[]core.ToolCallResult{{ID: "1", Name: "edit", Error: "blocked by policy"}},
		func(ev protocol.StepEvent) {
			callbacks++
		},
	)

	if callbacks != 0 {
		t.Fatalf("callbacks = %d, want no UI result callback for blocked tool", callbacks)
	}
	if len(msgs) != 1 || msgs[0].Content != "blocked by policy" || !msgs[0].IsError {
		t.Fatalf("messages = %+v, want blocked result preserved for context", msgs)
	}
}

func TestEmitStartCallbacksMarksBlockedCalls(t *testing.T) {
	var events []string
	var blockedOutput string
	emitStartCallbacks([]core.ToolCallItem{
		{Name: "read", Args: map[string]any{"path": "x.go"}},
		{Name: "write", Args: map[string]any{"path": "x.go", "_preview": "preview should not show"}},
	}, map[int]string{1: "blocked"}, func(ev protocol.StepEvent) {
		events = append(events, string(ev.Action)+":"+ev.ToolName)
		if ev.Action == protocol.StepActionToolBlocked {
			blockedOutput = ev.Output
		}
	})

	if got, want := strings.Join(events, ","), "tool_start:read,tool_blocked:write"; got != want {
		t.Fatalf("events = %q, want %q", got, want)
	}
	if blockedOutput != "blocked" {
		t.Fatalf("blocked output = %q, want reason", blockedOutput)
	}
}

func TestEmitStartCallbacksUsesEffectiveDelegatedIdentity(t *testing.T) {
	var got protocol.StepEvent
	emitStartCallbacks([]core.ToolCallItem{{
		ID:            "1",
		Name:          "capability",
		Args:          map[string]any{"action": "call"},
		EffectiveName: "mcp__github__create_issue",
		EffectiveArgs: map[string]any{"repo": "demo"},
	}}, nil, func(ev protocol.StepEvent) { got = ev })
	if got.ToolName != "mcp__github__create_issue" || !strings.Contains(got.ToolArgs, "demo") {
		t.Fatalf("start callback identity = %+v", got)
	}
}

func TestSkillLoadedStateFollowsContextDelivery(t *testing.T) {
	for _, deferred := range []bool{false, true} {
		name := "immediate"
		if deferred {
			name = "deferred_reload"
		}
		t.Run(name, func(t *testing.T) {
			a := newTestAgent()
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: delivery-test\ndescription: test\n---\nComplete instructions."), 0600); err != nil {
				t.Fatal(err)
			}
			mgr := skill.NewWithDirs(a.deps.ctxMgr, a.deps.toolRegistry, 128000, []string{dir})
			mgr.Load(nil)
			if deferred {
				a.deps.ctxMgr.SetSystemPrompt(strings.Repeat("x", 512000))
			}
			a.toolRunner.executeAndFeedback([]core.ToolCallItem{{ID: "skill-call", Name: "skill", Args: map[string]any{"name": "delivery-test"}}}, "", nil)
			snap := a.deps.ctxMgr.Snapshot()
			var delivered bool
			for _, msg := range snap.Messages {
				if msg.ToolCallID == "skill-call" {
					if msg.IsError {
						t.Fatalf("skill failed: %s", msg.Content)
					}
					delivered = msg.Source != core.DeferredOutputSource
				}
			}
			if delivered == deferred {
				t.Fatal("fixture delivery status is wrong")
			}
			if !deferred {
				if !mgr.LoadedSet()["delivery-test"] {
					t.Fatal("successfully delivered skill was not marked loaded")
				}
				return
			}
			if mgr.LoadedSet()["delivery-test"] {
				t.Fatal("deferred skill marked loaded before delivery")
			}
			data, err := json.Marshal(snap)
			if err != nil {
				t.Fatal(err)
			}
			var loaded ctxmgr.ManagerSnapshot
			if err := json.Unmarshal(data, &loaded); err != nil {
				t.Fatal(err)
			}
			restored := ctxmgr.New(ctxmgr.Config{ContextWindow: 128000})
			restored.SetOutputStore(a.deps.toolExecutor.ExecutionState().OutputStore)
			restoredMgr := skill.NewWithDirs(restored, a.deps.toolRegistry, 128000, []string{dir})
			restoredMgr.Load(nil)
			restored.Restore(loaded)
			restored.SetSystemPrompt("small")
			if _, err := restored.Summarize(context.Background()); err != nil {
				t.Fatal(err)
			}
			if !restoredMgr.LoadedSet()["delivery-test"] {
				t.Fatal("restored delivery was not marked loaded")
			}
		})
	}
}
