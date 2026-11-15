package shell

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"
)

// A quiet server must return control at an observation boundary without being
// terminated or automatically suggesting another wait.
func TestQuietServiceObservationHasDeadline(t *testing.T) {
	for _, action := range []string{"wait", "watch"} {
		t.Run(action, func(t *testing.T) {
			tool := testShellTool()
			tool.initialWait = 10 * time.Millisecond
			cleanupShellTool(t, tool)
			if _, err := tool.ExecuteWithPermission(t.Context(), map[string]any{"command": "sleep 5", "name": "service"}, permReq()); err != nil {
				t.Fatal(err)
			}
			process := NewProcessTool(tool)
			ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
			defer cancel()
			args := map[string]any{"action": action, "task": "service", "wait_timeout_ms": 20}
			if action == "watch" {
				args["event"] = "output"
			}
			result, err := process.Execute(ctx, args)
			if err != nil {
				t.Fatalf("observation did not return before caller cancellation: %v", err)
			}
			if result.Process.State != "running" || result.Process.Reason != "wait_timeout" {
				t.Fatalf("lost running state: %+v", result)
			}
			if strings.Contains(result.Text, "next_call:") {
				t.Fatal("quiet observation instructs blind waiting", result.Text)
			}
			if !strings.Contains(result.Text, "service readiness") {
				t.Fatal("missing readiness diagnostic", result.Text)
			}
			if strings.Contains(result.Text, "loopback") || strings.Contains(result.Text, "same shell command") {
				t.Fatal("observation assumes an unobserved network topology", result.Text)
			}
			task, err := tool.registryOnce().task(tool.CurrentSessionID(), "service")
			if err != nil || !task.isRunning() {
				t.Fatal("observation terminated service")
			}
		})
	}
}

func TestObservationBudgetValidation(t *testing.T) {
	tool := testShellTool()
	cleanupShellTool(t, tool)
	process := NewProcessTool(tool)
	for _, value := range []any{nil, 0, -1, "30", math.NaN(), math.Inf(1)} {
		_, err := process.Execute(t.Context(), map[string]any{"action": "wait", "task": "missing", "wait_timeout_ms": value})
		if err == nil || !strings.Contains(err.Error(), "wait_timeout_ms") {
			t.Fatalf("invalid budget %v was not rejected before task lookup: %v", value, err)
		}
	}
}

func TestManagedLaunchDoesNotPrescribeWaitingForServerExit(t *testing.T) {
	text := formatManagedShellRun("server", "")
	if strings.Contains(text, "next_call:") {
		t.Fatal("launch prescribes waiting", text)
	}
	if !strings.Contains(text, "status: running") || !strings.Contains(text, "same shell command") {
		t.Fatal("missing process/network semantics", text)
	}
}
