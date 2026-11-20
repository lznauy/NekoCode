package shell

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/toolutil"
)

type taskStatus int

const (
	taskRunning taskStatus = iota
	taskExited
	taskKilled
	taskTimeout
	taskUnknown
)

func (s taskStatus) String() string {
	switch s {
	case taskRunning:
		return "running"
	case taskExited:
		return "exited"
	case taskKilled:
		return "killed"
	case taskTimeout:
		return "timeout"
	default:
		return "unknown"
	}
}

type shellTask struct {
	directory   string
	name        string
	owner       string
	cmd         string
	launchKey   string
	startedAt   time.Time
	proc        processHandle
	scratchFile *os.File // immutable after registration; used only for idle-handle eviction
	cancel      context.CancelFunc

	mu             sync.Mutex
	status         taskStatus
	timedOut       bool
	stopping       bool
	monitoring     bool
	managed        bool
	endedAt        time.Time
	exitCode       int
	readFailed     bool
	logs           []byte
	logStart       uint64
	logEnd         uint64
	watchOffset    uint64
	observedOffset uint64
	unchangedWaits int
	removeAt       time.Time
	capture        *core.OutputCapture
	outputStore    *core.OutputStore
	savedOutput    core.ToolOutput
	captureErr     error

	done    chan struct{}
	drained chan struct{}
	changed chan struct{}
}

type processHandle interface {
	Stdout() io.ReadCloser
	Stderr() io.ReadCloser
	Wait() error
	Terminate(time.Duration) error
}

func (t *shellTask) signalChanged() {
	select {
	case t.changed <- struct{}{}:
	default:
	}
}

func (t *shellTask) appendLogs(chunk []byte) {
	if len(chunk) == 0 {
		return
	}
	t.mu.Lock()
	if t.capture != nil {
		if _, err := t.capture.Write(chunk); err != nil && !errors.Is(err, core.ErrOutputLimit) && t.captureErr == nil {
			t.captureErr = err
		}
	}
	t.logEnd += uint64(len(chunk))
	if len(chunk) >= logRingSize {
		t.logs = append(t.logs[:0], chunk[len(chunk)-logRingSize:]...)
	} else {
		if trim := len(t.logs) + len(chunk) - logRingSize; trim > 0 {
			t.logs = append(t.logs[:0], t.logs[trim:]...)
		}
		t.logs = append(t.logs, chunk...)
	}
	t.logStart = t.logEnd - uint64(len(t.logs))
	t.mu.Unlock()
	t.signalChanged()
}

func (t *shellTask) markEnded(status taskStatus, code int) {
	t.mu.Lock()
	if t.timedOut {
		status = taskTimeout
		code = -1
	}
	t.status = status
	t.endedAt = time.Now()
	t.exitCode = code
	t.mu.Unlock()
	close(t.done)
	t.signalChanged()
}

func (t *shellTask) requestTimeout() bool {
	t.mu.Lock()
	if t.status != taskRunning || t.stopping {
		t.mu.Unlock()
		return false
	}
	t.timedOut = true
	t.stopping = true
	t.mu.Unlock()
	t.signalChanged()
	return true
}

func (t *shellTask) completeObservation(max int) processResult {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.status == taskRunning {
		t.managed = true
		t.watchOffset = t.logEnd
	}
	// Keep an exited command discoverable until its pipes and saved output
	// have finished draining; a timed observation must not discard recovery.
	select {
	case <-t.drained:
	default:
		t.managed = true
	}
	return t.resultLocked("", tailCopy(t.logs, max), true, false)
}

// Completed commands have a recoverable transcript, so their default preview
// can be smaller than the live watch window.
const completedPreviewBytes = 2048

func (t *shellTask) finishOutput() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.capture == nil {
		return
	}
	output, err := t.capture.Finish()
	if output.Artifact.Path == "" && len(output.Text) > completedPreviewBytes && err == nil {
		output.Artifact.Path, err = t.outputStore.PreserveOutput(output.Text)
	}
	if err != nil && t.captureErr == nil {
		t.captureErr = err
	}
	output.Artifact.Incomplete = output.Artifact.Incomplete || t.readFailed || t.captureErr != nil
	t.savedOutput = output
}

func (t *shellTask) isRunning() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.status == taskRunning
}

func (t *shellTask) beginStop() (processHandle, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.status != taskRunning || t.stopping {
		return nil, false
	}
	t.stopping = true
	return t.proc, true
}

func (t *shellTask) clearStopping() {
	t.mu.Lock()
	if t.status == taskRunning {
		t.stopping = false
	}
	t.mu.Unlock()
}

func (t *shellTask) beginMonitor() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.monitoring {
		return false
	}
	t.monitoring = true
	return true
}

func (t *shellTask) endMonitor() {
	t.mu.Lock()
	t.monitoring = false
	t.mu.Unlock()
}

func (t *shellTask) scheduleRemoval() {
	t.mu.Lock()
	t.removeAt = time.Now().Add(taskRetentionTTL)
	t.mu.Unlock()
}

func (t *shellTask) shouldRemove(now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.status != taskRunning && !t.removeAt.IsZero() && now.After(t.removeAt)
}

func (t *shellTask) snapshotAndAcknowledge(max int, reason string) processResult {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.watchOffset = t.logEnd
	return t.resultLocked(reason, tailCopy(t.logs, max), true, false)
}

func (t *shellTask) takeNewOutput(max int) (processResult, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	start := t.watchOffset
	if start >= t.logEnd {
		return processResult{}, false
	}
	if start < t.logStart {
		start = t.logStart
	}
	output := tailCopy(t.logs[int(start-t.logStart):], max)
	full := t.watchOffset == 0
	clipped := t.logEnd-t.watchOffset > uint64(len(output))
	t.watchOffset = t.logEnd
	return t.resultLocked("output", output, full, clipped), true
}

func (t *shellTask) result(reason string) processResult {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.resultLocked(reason, nil, true, false)
}

// Caller holds mu: output, state and completeness describe the same sample.
func (t *shellTask) resultLocked(reason string, output []byte, fromStart, clipped bool) processResult {
	complete := false
	select {
	case <-t.drained:
		complete = t.status != taskRunning && !t.readFailed && fromStart && uint64(len(output)) == t.logEnd
	default:
	}
	newBytes := t.logEnd - t.observedOffset
	if reason != "reused_running" {
		if reason == "wait_timeout" && t.status == taskRunning && newBytes == 0 {
			t.unchangedWaits++
		} else if newBytes > 0 || t.status != taskRunning {
			t.unchangedWaits = 0
		}
		t.observedOffset = t.logEnd
	}
	observation := core.ProcessObservation{State: t.status.String(), OutputComplete: complete, Task: t.name, Reason: reason, NewOutputBytes: newBytes, UnchangedWaits: t.unchangedWaits}
	if t.status == taskExited {
		observation.ExitCode, observation.ExitCodeKnown = t.exitCode, true
	}
	text := toolutil.StripAnsi(string(output))
	if t.captureErr != nil {
		text += fmt.Sprintf("\n[Command ran, but output preservation failed: %v. Do not rerun automatically.]", t.captureErr)
	} else if t.savedOutput.Artifact.Incomplete {
		text += "\n[Output preview truncated. The saved capture is incomplete (capture limit or read failure); do not rerun automatically.]"
	} else if fromStart && len(output) > 0 && reason != "output" && t.status != taskRunning && t.savedOutput.Artifact.Path != "" && len(t.savedOutput.Text) > completedPreviewBytes {
		text = core.LimitOutput(toolutil.StripAnsi(t.savedOutput.Text), completedPreviewBytes)
		observation.OutputComplete = false
	} else if clipped || fromStart && uint64(len(output)) < t.logEnd {
		if t.savedOutput.Artifact.Path != "" {
			text += "\n[Output preview truncated; use read on the result artifact to recover earlier output. Do not rerun the command.]"
		} else {
			text += "\n[Output preview truncated; the saved capture will be available after the process output drains (up to 8 MiB). Use process wait/watch, not a new shell launch.]"
		}
	}
	observation.SampleHash = core.EvidenceHash([]byte(text))
	return processResult{info: t.summaryLocked(), output: text, reason: reason, observation: observation, captured: t.savedOutput}
}

func tailCopy(data []byte, max int) []byte {
	if len(data) == 0 {
		return nil
	}
	if max <= 0 || max > len(data) {
		max = len(data)
	}
	return append([]byte(nil), data[len(data)-max:]...)
}

func (t *shellTask) summary() processInfo {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.summaryLocked()
}

func (t *shellTask) summaryLocked() processInfo {
	duration := time.Since(t.startedAt)
	if t.status != taskRunning {
		duration = t.endedAt.Sub(t.startedAt)
	}
	status := t.status.String()
	if t.status == taskExited && t.exitCode == 0 {
		status = "done"
	} else if t.status == taskExited {
		status = "failed"
	}
	return processInfo{
		directory:  t.directory,
		name:       t.name,
		command:    t.cmd,
		status:     status,
		duration:   formatDuration(duration),
		exitCode:   t.exitCode,
		managed:    t.managed,
		hasScratch: t.scratchFile != nil,
	}
}

type processInfo struct {
	hasScratch bool
	directory  string
	name       string
	command    string
	status     string
	duration   string
	exitCode   int
	managed    bool
}

type processResult struct {
	captured    core.ToolOutput
	observation core.ProcessObservation
	info        processInfo
	output      string
	reason      string
}
