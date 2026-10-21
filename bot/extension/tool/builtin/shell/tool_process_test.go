package shell

import (
	"context"
	"strings"
	"testing"
	"time"
)

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
