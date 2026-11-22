package subagent

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/provider/types"
	"nekocode/protocol"
)

// A scheduling interval covers one foreground attempt, including model/tool
// I/O. Resuming the saved task starts another interval in the same workspace.
const DefaultForegroundInterval = 10 * time.Minute

var errForegroundReview = errors.New("foreground attempt due for coordinator review")

type processWaitHistory struct {
	reviewTask   string
	launchArgs   string
	launchTask   string
	observations int
	elapsed      time.Duration
	last         protocol.ProcessObservation
}

func (w *processWaitHistory) observe(name string, args map[string]any, result core.ToolCallResult) {
	if w.reviewTask != "" {
		return // Keep the evidence for the handoff already promised to the coordinator.
	}
	if name == "write" || name == "edit" {
		for _, receipt := range result.Evidence.Executions {
			if receipt.Tool == name && receipt.Status == "success" && result.Error == "" {
				*w = processWaitHistory{}
				break
			}
		}
		return
	}
	if name == "shell" {
		// Each attempt has one workspace. Only link successive handles when
		// the shell receipt confirms the same launch arguments in this attempt.
		for _, receipt := range result.Evidence.Executions {
			if receipt.Tool != "shell" {
				continue
			}
			if receipt.ArgumentsHash == "" || receipt.ArgumentsHash != w.launchArgs {
				*w = processWaitHistory{}
			}
			w.launchArgs = receipt.ArgumentsHash
			w.launchTask = receipt.Process.Task
			return
		}
		*w = processWaitHistory{}
		return
	}

	if !isProcessWait(name, args) {
		return
	}
	for _, receipt := range result.Evidence.Executions {
		if receipt.Tool != "process" || receipt.Process.Task == "" || receipt.EndedAt <= receipt.StartedAt {
			continue
		}
		p := receipt.Process
		// A task may relaunch the same probe under a new process handle.
		// Compare its output before process IDs and status labels are added.
		silent := p.State == "running" && p.Reason == "wait_timeout" && p.NewOutputBytes == 0
		terminalSample := p.State == "exited" && p.SampleHash != "" && w.launchArgs != "" && p.Task == w.launchTask
		if !silent && !terminalSample {
			w.observations, w.elapsed, w.last = 0, 0, p
			continue
		}
		previous := w.last
		sameObservation := previous.State == p.State
		if silent {
			sameObservation = sameObservation && previous.Task == p.Task
		} else {
			sameObservation = sameObservation && previous.SampleHash == p.SampleHash && previous.ExitCodeKnown == p.ExitCodeKnown && previous.ExitCode == p.ExitCode
		}
		if !sameObservation {
			w.observations, w.elapsed = 0, 0
		}
		w.observations++
		w.elapsed += time.Duration(receipt.EndedAt - receipt.StartedAt)
		w.last = receipt.Process
	}
}

func isProcessWait(name string, args map[string]any) bool {
	action, _ := args["action"].(string)
	action = strings.ToLower(strings.TrimSpace(action))
	return name == "process" && (action == "wait" || action == "watch")
}

// Allow the model to use a finished test result or to complete the task.
// Yield only when it asks to spend another turn waiting.
func (w *processWaitHistory) deferWait(name string, args map[string]any, process core.Tool) bool {
	if !isProcessWait(name, args) || w.observations < 2 || w.elapsed < 2*time.Minute {
		return false
	}
	task, _ := args["task"].(string)
	task = strings.TrimSpace(task)
	expected := w.last.Task
	if w.last.State == "exited" {
		expected = w.launchTask
	}
	if task == "" || task != expected {
		return false
	}
	// The previous observation can be stale by the time the model asks again.
	// Let the tool deliver a ready result through its normal execution path.
	if probe, ok := process.(interface{ WaitReady(map[string]any) bool }); ok && probe.WaitReady(args) {
		return false
	}
	if w.reviewTask == "" {
		w.reviewTask = task
	}
	return true
}

func (w processWaitHistory) description() string {
	if w.last.Task == "" {
		return ""
	}
	detail := fmt.Sprintf("Observed %d process waits totaling %s; last process %q reported %q (%s).", w.observations, w.elapsed.Round(time.Second), w.last.Task, w.last.State, w.last.Reason)
	if w.reviewTask != "" {
		detail += fmt.Sprintf(" Further observation of %q was deferred.", w.reviewTask)
	}
	return detail
}

func (r *engineRun) foregroundPartial() *Result {
	content := "Foreground attempt yielded for coordinator review. Inspect existing changes and process state before resuming this task. Verify missing prerequisites and their owner; an interrupted operation may have partial side effects."
	if r.cfg.IsolatedWorkspace {
		content = "Foreground worktree attempt reached its time limit. Its private process registry closes at handoff and terminates remaining managed processes. Previous process handles cannot be used on resume. Inspect the worktree and saved output before deciding whether to rerun a command."
		if r.cfg.Environment != nil {
			if processes := r.cfg.Environment().ManagedProcesses; processes != "" {
				content += " Last observed before shutdown: " + processes
			}
		}
		r.ctxMgr.Add("user", content, types.MessageSourceRuntimeEvent)
	}
	if detail := r.state.processWait.description(); detail != "" {
		content += " " + detail
	}
	result := buildProtocolPartialResult(r.state.lastText, content, r.state.meta(r.ctxMgr.Status()))
	result.Summary = protocol.RunSummary{StepCount: r.step, StopReason: "foreground_interval"}
	return result
}
