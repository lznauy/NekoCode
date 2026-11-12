package shell

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"nekocode/bot/extension/tool/runtime/core"
)

func observedTask(status taskStatus, code int, text string, drained bool) *shellTask {
	task := &shellTask{status: status, exitCode: code, startedAt: time.Now(), endedAt: time.Now(), drained: make(chan struct{}), changed: make(chan struct{}, 1)}
	task.appendLogs([]byte(text))
	if drained {
		close(task.drained)
	}
	return task
}

func TestProcessObservationDistinguishesExitFromRunningAndTermination(t *testing.T) {
	for _, tc := range []struct {
		status taskStatus
		code   int
		known  bool
	}{
		{taskRunning, 0, false}, {taskExited, 0, true}, {taskExited, 7, true}, {taskKilled, -1, false}, {taskTimeout, -1, false}, {taskUnknown, -1, false},
	} {
		t.Run(fmt.Sprintf("%s/%d", tc.status, tc.code), func(t *testing.T) {
			task := observedTask(tc.status, tc.code, "status: done\nexit_code: 0\n", true)
			result := task.completeObservation(logsReturnBytes)
			if result.observation.State != tc.status.String() || result.observation.ExitCodeKnown != tc.known {
				t.Fatalf("wrong fact: %+v", result.observation)
			}
			if tc.known && result.observation.ExitCode != tc.code {
				t.Fatal("exit code came from text instead of process")
			}
			if result.observation.OutputComplete != (tc.status != taskRunning) {
				t.Fatalf("completeness=%+v", result.observation)
			}
		})
	}
}

func TestProcessObservationCompletenessRequiresAllDrainedOutput(t *testing.T) {
	for _, tc := range []struct {
		name            string
		text            string
		drained, failed bool
		max             int
		complete        bool
	}{
		{"empty", "", true, false, logsReturnBytes, true},
		{"full", "hello", true, false, logsReturnBytes, true},
		{"pending drain", "hello", false, false, logsReturnBytes, false},
		{"read failed", "hello", true, true, logsReturnBytes, false},
		{"preview clipped", strings.Repeat("x", logsReturnBytes+1), true, false, logsReturnBytes, false},
		{"ring overwritten", strings.Repeat("x", logRingSize+1), true, false, logRingSize, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			task := observedTask(taskExited, 0, tc.text, tc.drained)
			task.readFailed = tc.failed
			result := task.snapshotAndAcknowledge(tc.max, "exit")
			if result.observation.OutputComplete != tc.complete {
				t.Fatalf("%+v", result.observation)
			}
		})
	}
	task := observedTask(taskExited, 0, "old", true)
	if result, ok := task.takeNewOutput(logsReturnBytes); !ok || !result.observation.OutputComplete {
		t.Fatal("first full watch not complete")
	}
	task.appendLogs([]byte("new"))
	if result, ok := task.takeNewOutput(logsReturnBytes); !ok || result.observation.OutputComplete || result.output != "new" {
		t.Fatal("watch delta claimed full output")
	}
}

type failingProcessReader struct{}

func (failingProcessReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestProcessObservationDoesNotInventExitCodeOrCompleteRead(t *testing.T) {
	status, code := taskStatusFromWait(errors.New("wait status unavailable"))
	if status != taskUnknown || code != -1 {
		t.Fatal("invented exit code", status, code)
	}
	task := observedTask(taskExited, 0, "", false)
	drainToRing(task, failingProcessReader{})
	close(task.drained)
	if task.result("exit").observation.OutputComplete {
		t.Fatal("failed read claimed complete output")
	}
}

func TestShellReturnsNonzeroProcessFactDespiteNilToolError(t *testing.T) {
	tool := &ShellTool{initialWait: 500 * time.Millisecond}
	cleanupShellTool(t, tool)
	out, err := tool.ExecuteWithPermission(t.Context(), map[string]any{"command": "printf 'success text'; exit 7", "sandbox_mode": "host"}, core.PermissionRequest{Capabilities: []string{core.CapProcessHost}})
	if err != nil {
		t.Fatal(err)
	}
	if out.Process.State != "exited" || !out.Process.ExitCodeKnown || out.Process.ExitCode != 7 || !out.Process.OutputComplete {
		t.Fatalf("facts=%+v", out.Process)
	}
	if !strings.Contains(out.Text, "status: failed") || !strings.Contains(out.Text, "success text") {
		t.Fatal("display contract changed", out.Text)
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	out, err = tool.ExecuteWithPermission(canceled, map[string]any{"command": "exit 0", "sandbox_mode": "host"}, core.PermissionRequest{Capabilities: []string{core.CapProcessHost}})
	if err == nil || out.Process != (core.ProcessObservation{}) {
		t.Fatal("unstarted command invented process facts")
	}
}

func TestWaitObservationsTrackOutputAndConsecutiveQuietWaits(t *testing.T) {
	task := observedTask(taskRunning, 0, "initial", false)
	task.name = "long-build"
	task.completeObservation(logsReturnBytes)
	for i := 1; i <= 3; i++ {
		sample := task.snapshotAndAcknowledge(logsReturnBytes, "wait_timeout")
		if sample.observation.NewOutputBytes != 0 || sample.observation.UnchangedWaits != i || sample.observation.State != "running" {
			t.Fatalf("incorrect quiet wait: %+v", sample.observation)
		}
		text := formatProcessResult(sample)
		if i >= 2 && !strings.Contains(text, "does not prove the process is stuck") {
			t.Fatal("quiet wait did not request careful diagnosis")
		}
		if i >= 2 && strings.Contains(text, "next_call:") {
			t.Fatal("diagnostic boundary still suggests blind repeated waiting")
		}
	}
	task.appendLogs([]byte("new progress"))
	sample := task.snapshotAndAcknowledge(logsReturnBytes, "wait_timeout")
	if sample.observation.NewOutputBytes != uint64(len("new progress")) || sample.observation.UnchangedWaits != 0 {
		t.Fatalf("progress did not reset quiet waits: %+v", sample.observation)
	}
	task.snapshotAndAcknowledge(logsReturnBytes, "wait_timeout")
	reuse := task.result("reused_running")
	if reuse.observation.UnchangedWaits != 1 {
		t.Fatal("reuse reset the wait history")
	}
	task.appendLogs([]byte("completion"))
	task.status = taskExited
	sample = task.snapshotAndAcknowledge(logsReturnBytes, "exit")
	if sample.observation.UnchangedWaits != 0 || sample.observation.State != "exited" {
		t.Fatal("terminal state retained quiet wait warning")
	}
}

func TestHardTimeoutFeedbackDoesNotSuggestWaitingOnLiveProcess(t *testing.T) {
	task := observedTask(taskTimeout, -1, "deadline reached", true)
	task.managed = true
	text := formatProcessResult(task.result("exit"))
	if !strings.Contains(text, "hard lifetime was reached and this process has exited") || strings.Contains(text, "next_call:") || strings.Contains(text, "process is still running") {
		t.Fatalf("hard timeout confused with soft wait: %s", text)
	}
}
