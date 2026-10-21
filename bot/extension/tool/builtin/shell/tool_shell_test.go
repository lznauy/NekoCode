package shell

import (
	"context"
	"strings"
	"testing"
	"time"

	"nekocode/bot/extension/tool/runtime/core"
)

func permReq() core.PermissionRequest {
	return core.PermissionRequest{Capabilities: []string{core.CapNetOutbound}}
}

func testShellTool() *ShellTool {
	return &ShellTool{initialWait: 100 * time.Millisecond}
}

func cleanupShellTool(t *testing.T, tool *ShellTool) {
	t.Helper()
	t.Cleanup(func() {
		if err := tool.Shutdown(); err != nil {
			t.Errorf("cleanup: %v", err)
		}
	})
}

func TestShellDescriptionExposesContractNotBackend(t *testing.T) {
	description := (&ShellTool{}).Description()
	for _, want := range []string{"permission-managed", "process tool", "Network", "writable roots", "host execution"} {
		if !strings.Contains(description, want) {
			t.Errorf("description missing capability contract %q: %s", want, description)
		}
	}
	for _, hidden := range []string{"native backend", "fallback", "/nix/store", "/home subdirs", "session_id", "poll"} {
		if strings.Contains(description, hidden) {
			t.Errorf("description leaked backend detail %q: %s", hidden, description)
		}
	}
}

func TestWritableRootsDescriptionSetsLeastPrivilege(t *testing.T) {
	var description string
	for _, parameter := range (&ShellTool{}).Parameters() {
		if parameter.Name == "writable_roots" {
			description = parameter.Description
			break
		}
	}
	for _, want := range []string{"outside the workspace", "exists", "smallest directory", "all descendants", "Omit workspace paths"} {
		if !strings.Contains(description, want) {
			t.Errorf("writable_roots description missing %q: %s", want, description)
		}
	}
}

func TestShellRunCompletesWithinObservation(t *testing.T) {
	tool := &ShellTool{initialWait: 500 * time.Millisecond}
	out, err := tool.ExecuteWithPermission(context.Background(), map[string]any{
		"command": "echo shell-done",
	}, permReq())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if strings.Contains(out.Render(), "task:") || strings.Contains(out.Render(), "status:") || strings.Contains(out.Render(), "exit_code:") {
		t.Fatalf("completed run should not include process metadata:\n%s", out.Render())
	}
	if !strings.Contains(out.Render(), "shell-done") {
		t.Fatalf("unexpected output:\n%s", out.Render())
	}
}

func TestShellRunReturnsManagedTaskThenProcessWaits(t *testing.T) {
	tool := testShellTool()
	processTool := NewProcessTool(tool)
	out, err := tool.ExecuteWithPermission(context.Background(), map[string]any{
		"command":    "sleep 0.3 && echo shell-later",
		"name":       "download",
		"timeout_ms": 2000,
	}, permReq())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out.Render(), "task: download") || strings.Contains(out.Render(), "status:") || strings.Contains(out.Render(), "elapsed:") {
		t.Fatalf("expected managed task, got:\n%s", out.Render())
	}

	wait, err := processTool.Execute(context.Background(), map[string]any{
		"action": "wait",
		"task":   "download",
	})
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if !strings.Contains(wait.Render(), "status: done") || !strings.Contains(wait.Render(), "reason: exit") || !strings.Contains(wait.Render(), "shell-later") {
		t.Fatalf("unexpected wait output:\n%s", wait.Render())
	}
	if strings.Contains(wait.Render(), "elapsed:") {
		t.Fatalf("wait output should omit elapsed metadata:\n%s", wait.Render())
	}
}

func TestShellHardTimeoutStopsTask(t *testing.T) {
	tool := testShellTool()
	processTool := NewProcessTool(tool)
	out, err := tool.ExecuteWithPermission(context.Background(), map[string]any{
		"command":    "sleep 5",
		"name":       "bounded",
		"timeout_ms": 250,
	}, permReq())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out.Render(), "task: bounded") || strings.Contains(out.Render(), "status:") || strings.Contains(out.Render(), "elapsed:") {
		t.Fatalf("expected managed task, got:\n%s", out.Render())
	}

	wait, err := processTool.Execute(context.Background(), map[string]any{"action": "wait", "task": "bounded"})
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if !strings.Contains(wait.Render(), "status: timeout") || !strings.Contains(wait.Render(), "command timed out") {
		t.Fatalf("expected hard timeout status, got:\n%s", wait.Render())
	}
}

func TestShellRejectsInvalidHardTimeout(t *testing.T) {
	tool := testShellTool()
	_, err := tool.ExecuteWithPermission(context.Background(), map[string]any{
		"command":    "echo should-not-run",
		"timeout_ms": "not-a-duration",
	}, permReq())
	if err == nil || !strings.Contains(err.Error(), "timeout_ms") {
		t.Fatalf("invalid hard timeout should fail explicitly, got %v", err)
	}
}

func TestManagedTaskSurvivesCallerCancellation(t *testing.T) {
	tool := testShellTool()
	processTool := NewProcessTool(tool)
	ctx, cancel := context.WithCancel(context.Background())
	out, err := tool.ExecuteWithPermission(ctx, map[string]any{
		"command": "sleep 0.3 && echo survived",
		"name":    "survivor",
	}, permReq())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out.Render(), "task: survivor") {
		t.Fatalf("expected task handle: %s", out.Render())
	}
	cancel()

	wait, err := processTool.Execute(context.Background(), map[string]any{"action": "wait", "task": "survivor"})
	if err != nil {
		t.Fatalf("wait after caller cancellation: %v", err)
	}
	if !strings.Contains(wait.Render(), "status: done") || !strings.Contains(wait.Render(), "survived") {
		t.Fatalf("managed process was tied to caller context:\n%s", wait.Render())
	}
}

func TestCancelledProcessWaitDoesNotStopTask(t *testing.T) {
	tool := testShellTool()
	processTool := NewProcessTool(tool)
	_, err := tool.ExecuteWithPermission(context.Background(), map[string]any{
		"command": "sleep 2",
		"name":    "waiting",
	}, permReq())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := processTool.Execute(ctx, map[string]any{"action": "wait", "task": "waiting"}); err == nil {
		t.Fatal("cancelled wait should return its context error")
	}
	if summary := tool.ProcessSummary(); !strings.Contains(summary, "waiting(running") {
		t.Fatalf("cancelled wait stopped the managed task: %q", summary)
	}
	if _, err := processTool.Execute(context.Background(), map[string]any{"action": "stop", "task": "waiting"}); err != nil {
		t.Fatalf("stop waiting task: %v", err)
	}
}

func TestCancellationDuringStartupObservationStopsCommand(t *testing.T) {
	tool := &ShellTool{initialWait: 500 * time.Millisecond}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err := tool.ExecuteWithPermission(ctx, map[string]any{
		"command": "sleep 2",
		"name":    "cancelled-start",
	}, permReq())
	if err == nil {
		t.Fatal("startup cancellation should abort the shell call")
	}
	if summary := tool.ProcessSummary(); summary != "" {
		t.Fatalf("startup-cancelled command leaked into managed tasks: %q", summary)
	}
}

func TestShellShutdownStopsManagedTasks(t *testing.T) {
	tool := testShellTool()
	for _, name := range []string{"service-a", "service-b"} {
		if _, err := tool.ExecuteWithPermission(context.Background(), map[string]any{
			"command": "sleep 5",
			"name":    name,
		}, permReq()); err != nil {
			t.Fatalf("start %s: %v", name, err)
		}
	}
	if err := tool.Shutdown(); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if summary := tool.ProcessSummary(); strings.Contains(summary, "(running") {
		t.Fatalf("shutdown left a managed task running: %q", summary)
	}
}

func TestShellRejectsDuplicateRunningName(t *testing.T) {
	tool := testShellTool()
	_, err := tool.ExecuteWithPermission(context.Background(), map[string]any{
		"command": "sleep 2",
		"name":    "same-name",
	}, permReq())
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	cleanupShellTool(t, tool)
	_, err = tool.ExecuteWithPermission(context.Background(), map[string]any{
		"command": "sleep 2",
		"name":    "same-name",
	}, permReq())
	if err == nil || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("duplicate running name should fail, got %v", err)
	}
}

func TestManagedProcessesAreScopedToSession(t *testing.T) {
	tool := testShellTool()
	processTool := NewProcessTool(tool)
	tool.SetSessionID("session-a")
	if _, err := tool.ExecuteWithPermission(context.Background(), map[string]any{
		"command": "sleep 2",
		"name":    "private-service",
	}, permReq()); err != nil {
		t.Fatalf("start session-a service: %v", err)
	}
	cleanupShellTool(t, tool)

	tool.SetSessionID("session-b")
	list, err := processTool.Execute(context.Background(), map[string]any{"action": "list"})
	if err != nil {
		t.Fatalf("list session-b: %v", err)
	}
	if list.Render() != "(no managed processes)" || tool.ProcessSummary() != "" {
		t.Fatalf("session-b can see session-a process: list=%q summary=%q", list.Render(), tool.ProcessSummary())
	}
	if _, err := processTool.Execute(context.Background(), map[string]any{
		"action": "wait", "task": "private-service",
	}); err == nil {
		t.Fatal("session-b should not access session-a process")
	}

	tool.SetSessionID("session-a")
	if summary := tool.ProcessSummary(); !strings.Contains(summary, "private-service(running") {
		t.Fatalf("session-a lost its managed process: %q", summary)
	}
	if err := tool.StopSession("session-a"); err != nil {
		t.Fatalf("stop session-a: %v", err)
	}
	if summary := tool.ProcessSummary(); strings.Contains(summary, "(running") {
		t.Fatalf("session stop left process running: %q", summary)
	}
}
