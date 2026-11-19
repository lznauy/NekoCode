package shell

import (
	"context"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/execution"
	"os"
	"strings"
	"testing"
	"time"
)

func TestShellRejectsMalformedExecutionArguments(t *testing.T) {
	for _, extra := range []map[string]any{
		{"sandbox_mode": true}, {"sandbox_mode": ""}, {"sandbox_mode": "typo"}, {"network": "true"}, {"network": 42},
		{"writable_roots": 42}, {"writable_roots": []any{42}}, {"writable_roots": []any{" "}}, {"name": 42},
	} {
		tool := testShellTool()
		args := map[string]any{"command": "printf SHOULD_NOT_EXECUTE"}
		for k, v := range extra {
			args[k] = v
		}
		out, err := tool.ExecuteWithPermission(context.Background(), args, permReq())
		if err == nil || strings.Contains(out.Text, "SHOULD_NOT_EXECUTE") {
			t.Errorf("invalid args executed: %+v out=%q err=%v", extra, out.Text, err)
		}
		tool.Shutdown()
	}
}
func TestShellLongOutputRetainsBeginningInActorArtifact(t *testing.T) {
	tool := &ShellTool{initialWait: time.Second}
	cleanupShellTool(t, tool)
	state := execution.NewExecutionState()
	state.OutputStore = core.NewOutputStore(t.TempDir())
	ctx := execution.WithExecutionState(t.Context(), state)
	out, err := tool.ExecuteWithPermission(ctx, map[string]any{"command": "printf 'FIRST_LINE\\n'; head -c 300000 /dev/zero | tr '\\000' x; printf '\\nLAST_LINE\\n'"}, permReq())
	if err != nil {
		t.Fatal(err)
	}
	if out.Artifact.Path == "" {
		t.Fatalf("long output has no recoverable artifact: text bytes=%d", len(out.Text))
	}
	if err := state.OutputStore.CheckArtifact(out.Artifact); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(out.Artifact.Path)
	if err != nil || !strings.HasPrefix(string(data), "FIRST_LINE\n") || !strings.HasSuffix(string(data), "LAST_LINE\n") || out.Artifact.Incomplete {
		t.Fatalf("artifact lost output: bytes=%d artifact=%+v err=%v", len(data), out.Artifact, err)
	}
	if !strings.Contains(strings.ToLower(out.Text), "truncat") {
		t.Fatalf("missing truncation notice: %q", out.Text[:min(100, len(out.Text))])
	}
}

func TestManagedProcessReturnsSavedOutputAfterExit(t *testing.T) {
	tool := &ShellTool{initialWait: time.Millisecond}
	cleanupShellTool(t, tool)
	state := execution.NewExecutionState()
	state.OutputStore = core.NewOutputStore(t.TempDir())
	ctx := execution.WithExecutionState(t.Context(), state)
	initial, err := tool.ExecuteWithPermission(ctx, map[string]any{"command": "sleep 0.1; printf 'BEGIN\\n'; head -c 20000 /dev/zero | tr '\\000' x; printf '\\nEND\\n'", "name": "long-log"}, permReq())
	if err != nil || initial.Process.State != "running" {
		t.Fatalf("command was not managed: %+v %v", initial.Process, err)
	}
	output, err := NewProcessTool(tool).Execute(ctx, map[string]any{"action": "wait", "task": "long-log"})
	if err != nil || output.Artifact.Path == "" {
		t.Fatalf("managed output lost: %+v %v", output, err)
	}
	if err := state.OutputStore.CheckArtifact(output.Artifact); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(output.Artifact.Path)
	if err != nil || !strings.HasPrefix(string(data), "BEGIN\n") || !strings.HasSuffix(string(data), "END\n") {
		t.Fatalf("managed output missing: bytes=%d err=%v", len(data), err)
	}
}

func TestShellCaptureLimitIsExplicitAndDoesNotStopProcess(t *testing.T) {
	task := observedTask(taskRunning, 0, "", false)
	task.outputStore = core.NewOutputStore(t.TempDir())
	task.capture = core.NewOutputCapture(task.outputStore, 32*1024, nil)
	task.appendLogs([]byte(strings.Repeat("x", 40000)))
	task.finishOutput()
	close(task.drained)
	result := task.completeObservation(logsReturnBytes)
	if task.status != taskRunning || result.captured.Artifact.Path == "" || !result.captured.Artifact.Incomplete || !strings.Contains(result.output, "incomplete") {
		t.Fatalf("capture cap lost: status=%v artifact=%+v", task.status, result.captured.Artifact)
	}
	data, err := os.ReadFile(result.captured.Artifact.Path)
	if err != nil || len(data) != 32*1024 {
		t.Fatalf("capture limit failed: bytes=%d err=%v", len(data), err)
	}
}

func TestShellCaptureFailureDoesNotHideCommandExecution(t *testing.T) {
	dir := t.TempDir()
	blocked := dir + "/file"
	if err := os.WriteFile(blocked, []byte("occupied"), 0600); err != nil {
		t.Fatal(err)
	}
	task := observedTask(taskExited, 0, "", false)
	task.outputStore = core.NewOutputStore(blocked)
	task.capture = core.NewOutputCapture(task.outputStore, core.MaxCapturedOutputBytes, nil)
	task.appendLogs([]byte(strings.Repeat("x", 20000)))
	task.finishOutput()
	close(task.drained)
	result := task.completeObservation(logsReturnBytes)
	if !strings.Contains(result.output, "Command ran, but output preservation failed") || !result.observation.ExitCodeKnown || result.observation.ExitCode != 0 {
		t.Fatalf("capture failure hid execution facts: %+v", result.observation)
	}
}

func TestCompletedShellPreviewPreservesMiddleForLaterRead(t *testing.T) {
	log := "first\n" + strings.Repeat("setup\n", 320) + "middle failure\n" + strings.Repeat("detail\n", 320) + "last\n"
	task := observedTask(taskExited, 1, "", false)
	task.outputStore = core.NewOutputStore(t.TempDir())
	task.capture = core.NewOutputCapture(task.outputStore, core.MaxCapturedOutputBytes, nil)
	task.appendLogs([]byte(log))
	task.finishOutput()
	close(task.drained)
	result := task.completeObservation(logsReturnBytes)
	if !strings.HasPrefix(result.output, "first\n") || !strings.HasSuffix(result.output, "last\n") || strings.Contains(result.output, "middle failure") || len(result.output) > completedPreviewBytes || result.observation.OutputComplete || result.observation.ExitCode != 1 {
		t.Fatalf("incorrect preview or process result: %q %+v", result.output, result.observation)
	}
	t.Logf("transcript=%d preview=%d rendered=%d", len(log), len(result.output), len(processToolOutput(result).Render()))
	full, err := os.ReadFile(result.captured.Artifact.Path)
	if err != nil || string(full) != log || result.captured.Artifact.Incomplete {
		t.Fatalf("transcript mismatch: %v", err)
	}
}
