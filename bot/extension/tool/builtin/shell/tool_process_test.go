package shell

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestSandboxFailureRecoveryForShellAndManagedProcesses(t *testing.T) {
	for _, managed := range []bool{false, true} {
		for _, output := range []string{
			"mkdir: Read-only file system", "npm ERR! EACCES: permission denied",
			"curl: (6) Could not resolve host: example.com", "connect: Network is unreachable",
		} {
			result := processResult{info: processInfo{status: "failed", exitCode: 1, managed: managed, name: "build"}, output: output}
			text := processToolOutput(result).Text
			for _, want := range []string{"status: failed", "exit_code: 1", output, "recovery_hint:", "retry shell", "runtime obtain approval", "partial side effects", "explicit denials", "domain restrictions"} {
				if !strings.Contains(text, want) {
					t.Errorf("managed=%v output=%q: missing %q in %s", managed, output, want, text)
				}
			}
		}
	}
	for _, result := range []processResult{
		{info: processInfo{status: "done"}, output: "test checks permission denied"},
		{info: processInfo{status: "running"}, output: "permission denied"},
		{info: processInfo{status: "failed", exitCode: 1}, output: "syntax error near unexpected token"},
		{info: processInfo{status: "failed", exitCode: 1}, output: "assertion failed"},
	} {
		if text := processToolOutput(result).Text; strings.Contains(text, "recovery_hint:") {
			t.Errorf("unexpected escalation hint: %s", text)
		}
	}
}

func TestProcessWaitTimeoutLeavesTaskRunning(t *testing.T) {
	tool := testShellTool()
	processTool := NewProcessTool(tool)
	processTool.waitTimeout = 30 * time.Millisecond
	_, err := tool.ExecuteWithPermission(context.Background(), map[string]any{
		"command": "sleep 2",
		"name":    "slow",
	}, permReq())
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	wait, err := processTool.Execute(context.Background(), map[string]any{"action": "wait", "task": "slow"})
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if !strings.Contains(wait.Render(), "status: running") || !strings.Contains(wait.Render(), "reason: wait_timeout") {
		t.Fatalf("soft wait boundary must leave task running:\n%s", wait.Render())
	}
	if _, err := processTool.Execute(context.Background(), map[string]any{"action": "stop", "task": "slow"}); err != nil {
		t.Fatalf("stop after wait timeout: %v", err)
	}
}

func TestProcessWatchReturnsOnNewOutput(t *testing.T) {
	tool := testShellTool()
	processTool := NewProcessTool(tool)
	_, err := tool.ExecuteWithPermission(context.Background(), map[string]any{
		"command": "sleep 0.25; printf event-ready; sleep 0.2",
		"name":    "listener",
	}, permReq())
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	watch, err := processTool.Execute(context.Background(), map[string]any{
		"action": "watch", "task": "listener", "event": "output",
	})
	if err != nil {
		t.Fatalf("watch: %v", err)
	}
	if !strings.Contains(watch.Render(), "reason: output") || !strings.Contains(watch.Render(), "event-ready") {
		t.Fatalf("watch did not resume on output:\n%s", watch.Render())
	}
	if _, err := processTool.Execute(context.Background(), map[string]any{"action": "stop", "task": "listener"}); err != nil {
		t.Fatalf("stop listener: %v", err)
	}
}

func TestProcessListAndRuntimeSummary(t *testing.T) {
	tool := testShellTool()
	processTool := NewProcessTool(tool)
	_, err := tool.ExecuteWithPermission(context.Background(), map[string]any{
		"command": "sleep 2",
		"name":    "web",
	}, permReq())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	cleanupShellTool(t, tool)

	list, err := processTool.Execute(context.Background(), map[string]any{"action": "list"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(list.Render(), "web") || !strings.Contains(list.Render(), "running") {
		t.Fatalf("unexpected process list:\n%s", list.Render())
	}
	summary := tool.ProcessSummary()
	if !strings.Contains(summary, "web(running") || strings.Contains(summary, "sleep 2") || strings.Contains(summary, "elapsed=") {
		t.Fatalf("unexpected runtime summary: %q", summary)
	}
}

func TestDefaultProcessWaitAndWatchHaveNoPeriodicWakeup(t *testing.T) {
	for _, event := range []string{"exit", "output"} {
		t.Run(event, func(t *testing.T) {
			tool := testShellTool()
			cleanupShellTool(t, tool)
			process := NewProcessTool(tool)
			if process.waitDuration() != 0 {
				t.Fatal("default process monitoring still samples on a periodic deadline")
			}
			_, err := tool.ExecuteWithPermission(t.Context(), map[string]any{"command": "sleep 5", "name": "quiet"}, permReq())
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 40*time.Millisecond)
			defer cancel()
			_, err = process.Execute(ctx, map[string]any{"action": "watch", "task": "quiet", "event": event})
			if err != context.DeadlineExceeded {
				t.Fatalf("wait returned a synthetic result instead of awaiting cancellation: %v", err)
			}
			task, err := tool.registryOnce().task(tool.CurrentSessionID(), "quiet")
			if err != nil || !task.isRunning() {
				t.Fatal("cancelled observation killed the process")
			}
		})
	}
}

func TestUnboundedProcessWaitReturnsOnlyAtRealExit(t *testing.T) {
	tool := testShellTool()
	cleanupShellTool(t, tool)
	process := NewProcessTool(tool)
	_, err := tool.ExecuteWithPermission(t.Context(), map[string]any{"command": "sleep 0.25; printf finished", "name": "finishes"}, permReq())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	result, err := process.Execute(ctx, map[string]any{"action": "wait", "task": "finishes"})
	if err != nil || result.Process.State != "exited" || !strings.Contains(result.Text, "finished") {
		t.Fatalf("wait did not return final outcome: %+v %v", result, err)
	}
}
