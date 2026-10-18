package shell

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"nekocode/bot/extension/tool/runtime/sandbox"
	"nekocode/bot/extension/tool/runtime/toolutil"
)

const (
	logRingSize     = 256 * 1024
	logsReturnBytes = 8 * 1024
	maxTasks        = 64

	taskRetentionTTL = 10 * time.Minute
)

// taskRegistry owns process lifetimes independently from an individual agent
// run. A returned managed process therefore survives an interrupted turn and
// is stopped only explicitly, by a hard timeout, or when the registry closes.
type taskRegistry struct {
	mu        sync.Mutex
	tasks     map[string]*shellTask
	reserved  map[string]struct{}
	nextID    int
	lifecycle context.Context
	cancel    context.CancelFunc
}

func newTaskRegistry() *taskRegistry {
	ctx, cancel := context.WithCancel(context.Background())
	return &taskRegistry{
		tasks: make(map[string]*shellTask), reserved: make(map[string]struct{}),
		lifecycle: ctx, cancel: cancel,
	}
}

type startRequest struct {
	name       string
	owner      string
	command    string
	directory  string
	profile    sandbox.Profile
	host       bool
	timeout    time.Duration
	sampleWait time.Duration
}

// start launches a command and observes it for a short runtime-controlled
// window. The caller context only governs this startup observation; after a
// managed task handle is returned, the registry owns the process lifetime.
func (r *taskRegistry) start(ctx context.Context, req startRequest) (*shellTask, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	name, err := r.reserveIdentity(req.owner, req.name)
	if err != nil {
		return nil, "", err
	}

	procCtx, cancel := context.WithCancel(r.lifecycle)
	var proc processHandle
	if req.host {
		proc, err = sandbox.StartHostIn(procCtx, req.command, req.directory)
	} else {
		proc, err = sandbox.Start(procCtx, req.command, req.profile)
	}
	if err != nil {
		cancel()
		r.releaseReservation(req.owner, name)
		return nil, "", err
	}

	t := &shellTask{
		name: name, owner: req.owner, cmd: req.command,
		startedAt: time.Now(), status: taskRunning, proc: proc,
		done: make(chan struct{}), changed: make(chan struct{}, 1),
		drained: make(chan struct{}),
	}
	r.register(t)
	drainProcessOutput(t, proc)
	go func() {
		defer cancel()
		waitErr := proc.Wait()
		status, code := taskStatusFromWait(waitErr)
		t.markEnded(status, code)
		t.scheduleRemoval()
	}()

	if req.timeout > 0 {
		go r.enforceHardTimeout(t, req.timeout)
	}

	if err := waitStartup(ctx, t, req.sampleWait); err != nil {
		cancel()
		_ = proc.Terminate(2 * time.Second)
		if waitForDone(t, 3*time.Second) {
			r.remove(t)
		} else {
			t.completeObservation(logsReturnBytes)
			return nil, "", fmt.Errorf("%w; process %q could not be confirmed stopped", err, name)
		}
		return nil, "", err
	}
	if !t.isRunning() {
		waitTaskDrained(t, 500*time.Millisecond)
	}
	observed, managed := t.completeObservation(logsReturnBytes)
	if !managed {
		r.remove(t)
	}
	return t, toolutil.StripAnsi(string(observed)), nil
}

func (r *taskRegistry) reserveIdentity(owner, requested string) (string, error) {
	r.cleanupExpired()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextID++
	id := r.nextID
	name := strings.TrimSpace(requested)
	if name == "" {
		name = "process_" + strconv.Itoa(id)
	} else if !validProcessName(name) {
		return "", fmt.Errorf("name must use only letters, numbers, '.', '_' or '-' (max 64 characters)")
	}
	key := reservationKey(owner, name)
	if task := r.tasks[key]; task != nil {
		if task.isRunning() {
			return "", fmt.Errorf("process %q is already running", name)
		}
		delete(r.tasks, key)
	}
	if _, exists := r.reserved[key]; exists {
		return "", fmt.Errorf("process %q is already starting", name)
	}
	for len(r.tasks)+len(r.reserved) >= maxTasks {
		if !r.evictOldestEndedLocked() {
			return "", fmt.Errorf("managed process limit reached (%d)", maxTasks)
		}
	}
	r.reserved[key] = struct{}{}
	return name, nil
}

func reservationKey(owner, name string) string {
	return owner + "\x00" + name
}

func (r *taskRegistry) releaseReservation(owner, name string) {
	r.mu.Lock()
	delete(r.reserved, reservationKey(owner, name))
	r.mu.Unlock()
}

func (r *taskRegistry) register(t *shellTask) {
	key := reservationKey(t.owner, t.name)
	r.mu.Lock()
	delete(r.reserved, key)
	r.tasks[key] = t
	r.mu.Unlock()
}

func (r *taskRegistry) remove(t *shellTask) {
	key := reservationKey(t.owner, t.name)
	r.mu.Lock()
	if r.tasks[key] == t {
		delete(r.tasks, key)
	}
	r.mu.Unlock()
}

func validProcessName(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func waitStartup(ctx context.Context, t *shellTask, max time.Duration) error {
	if max <= 0 {
		max = time.Second
	}
	timer := time.NewTimer(max)
	defer timer.Stop()
	select {
	case <-t.done:
		return nil
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *taskRegistry) enforceHardTimeout(t *shellTask, timeout time.Duration) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-t.done:
		return
	case <-r.lifecycle.Done():
		return
	case <-timer.C:
		if !t.requestTimeout() {
			return
		}
		t.appendLogs([]byte(fmt.Sprintf("\n[command timed out after %s]\n", timeout.Truncate(time.Millisecond))))
		if err := t.proc.Terminate(2 * time.Second); err != nil {
			t.appendLogs([]byte(fmt.Sprintf("[failed to terminate timed-out command: %v]\n", err)))
			t.clearStopping()
		}
	}
}

func taskStatusFromWait(err error) (taskStatus, int) {
	if err == nil {
		return taskExited, 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if ws, ok := exitErr.Sys().(syscall.WaitStatus); ok {
			if ws.Signaled() {
				return taskKilled, -1
			}
			return taskExited, ws.ExitStatus()
		}
		return taskExited, exitErr.ExitCode()
	}
	return taskExited, 1
}

func (r *taskRegistry) wait(ctx context.Context, owner, ref string, max time.Duration) (processResult, error) {
	t, err := r.task(owner, ref)
	if err != nil {
		return processResult{}, err
	}
	if !t.beginMonitor() {
		return processResult{}, fmt.Errorf("process %q already has an active wait or watch", t.name)
	}
	defer t.endMonitor()
	reason, err := waitForEvent(ctx, t.done, max)
	if err != nil {
		return processResult{}, err
	}
	if reason == "exit" {
		waitTaskDrained(t, 500*time.Millisecond)
	}
	t.scheduleRemoval()
	return newProcessResult(t, reason, t.snapshotAndAcknowledge(logsReturnBytes)), nil
}

func (r *taskRegistry) watch(ctx context.Context, owner, ref, event string, max time.Duration) (processResult, error) {
	t, err := r.task(owner, ref)
	if err != nil {
		return processResult{}, err
	}
	if event == "exit" {
		return r.wait(ctx, owner, ref, max)
	}
	if event != "output" {
		return processResult{}, fmt.Errorf("event must be \"exit\" or \"output\"")
	}
	if !t.beginMonitor() {
		return processResult{}, fmt.Errorf("process %q already has an active wait or watch", t.name)
	}
	defer t.endMonitor()

	timer := time.NewTimer(max)
	defer timer.Stop()
	for {
		if output, ok := t.takeNewOutput(logsReturnBytes); ok {
			return newProcessResult(t, "output", output), nil
		}
		if !t.isRunning() {
			waitTaskDrained(t, 500*time.Millisecond)
			if output, ok := t.takeNewOutput(logsReturnBytes); ok {
				return newProcessResult(t, "output", output), nil
			}
			return newProcessResult(t, "exit", nil), nil
		}
		select {
		case <-t.changed:
		case <-t.done:
		case <-timer.C:
			return newProcessResult(t, "wait_timeout", nil), nil
		case <-ctx.Done():
			return processResult{}, ctx.Err()
		}
	}
}

func waitForEvent(ctx context.Context, done <-chan struct{}, max time.Duration) (string, error) {
	timer := time.NewTimer(max)
	defer timer.Stop()
	select {
	case <-done:
		return "exit", nil
	case <-timer.C:
		return "wait_timeout", nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func newProcessResult(t *shellTask, reason string, output []byte) processResult {
	return processResult{
		info:   t.summary(),
		output: toolutil.StripAnsi(string(output)),
		reason: reason,
	}
}

func (r *taskRegistry) list(owner string) []processInfo {
	r.cleanupExpired()
	r.mu.Lock()
	tasks := make([]*shellTask, 0, len(r.tasks))
	for _, t := range r.tasks {
		if t.owner == owner {
			tasks = append(tasks, t)
		}
	}
	r.mu.Unlock()
	sort.Slice(tasks, func(i, j int) bool {
		if tasks[i].startedAt.Equal(tasks[j].startedAt) {
			return tasks[i].name < tasks[j].name
		}
		return tasks[i].startedAt.Before(tasks[j].startedAt)
	})
	out := make([]processInfo, 0, len(tasks))
	for _, task := range tasks {
		if info := task.summary(); info.managed {
			out = append(out, info)
		}
	}
	return out
}

func (r *taskRegistry) summary(owner string) string {
	tasks := r.list(owner)
	if len(tasks) == 0 {
		return ""
	}
	parts := make([]string, 0, len(tasks))
	for _, info := range tasks {
		part := fmt.Sprintf("%s(%s", info.name, info.status)
		if info.status != "running" {
			part += fmt.Sprintf(",exit=%d", info.exitCode)
		}
		part += ")"
		parts = append(parts, part)
	}
	return strings.Join(parts, "; ")
}

func compactProcessText(value string, max int) string {
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max-3]) + "..."
}

func (r *taskRegistry) stop(owner, ref string) (string, error) {
	t, err := r.task(owner, ref)
	if err != nil {
		return "", err
	}
	return r.stopTask(t)
}

func (r *taskRegistry) stopTask(t *shellTask) (string, error) {
	proc, ok := t.beginStop()
	if !ok {
		if status := t.summary().status; status != "running" {
			return "", fmt.Errorf("process %q is already %s", t.name, status)
		}
		if waitForDone(t, 3*time.Second) {
			return fmt.Sprintf("process %s stopped", t.name), nil
		}
		return "", fmt.Errorf("process %q is already stopping", t.name)
	}
	if err := proc.Terminate(2 * time.Second); err != nil {
		t.clearStopping()
		return "", fmt.Errorf("failed to stop process %q: %w", t.name, err)
	}
	if !waitForDone(t, 500*time.Millisecond) {
		t.clearStopping()
		return "", fmt.Errorf("process %q did not reach a terminal state after termination", t.name)
	}
	return fmt.Sprintf("process %s stopped", t.name), nil
}

func (r *taskRegistry) stopAll() error {
	return r.stopTasks(r.runningTasks("", false))
}

func (r *taskRegistry) stopOwner(owner string) error {
	return r.stopTasks(r.runningTasks(owner, true))
}

func (r *taskRegistry) runningTasks(owner string, filterOwner bool) []*shellTask {
	r.mu.Lock()
	defer r.mu.Unlock()
	tasks := make([]*shellTask, 0, len(r.tasks))
	for _, t := range r.tasks {
		if (!filterOwner || t.owner == owner) && t.isRunning() {
			tasks = append(tasks, t)
		}
	}
	return tasks
}

func (r *taskRegistry) stopTasks(tasks []*shellTask) error {
	var wg sync.WaitGroup
	var errMu sync.Mutex
	var errs []error
	for _, task := range tasks {
		wg.Go(func() {
			if _, err := r.stopTask(task); err != nil {
				errMu.Lock()
				errs = append(errs, err)
				errMu.Unlock()
			}
		})
	}
	wg.Wait()
	return errors.Join(errs...)
}

func (r *taskRegistry) shutdown() error {
	if err := r.stopAll(); err != nil {
		return err
	}
	r.cancel()
	return nil
}

func (r *taskRegistry) task(owner, ref string) (*shellTask, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, fmt.Errorf("task is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if task := r.tasks[reservationKey(owner, ref)]; task != nil {
		return task, nil
	}
	return nil, fmt.Errorf("process %q not found", ref)
}

func (r *taskRegistry) cleanupExpired() {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	for id, t := range r.tasks {
		if t.shouldRemove(now) {
			delete(r.tasks, id)
		}
	}
}

func (r *taskRegistry) evictOldestEndedLocked() bool {
	var oldestKey string
	var oldestEnd time.Time
	for key, t := range r.tasks {
		t.mu.Lock()
		running := t.status == taskRunning
		endedAt := t.endedAt
		t.mu.Unlock()
		if running {
			continue
		}
		if oldestKey == "" || endedAt.Before(oldestEnd) {
			oldestKey, oldestEnd = key, endedAt
		}
	}
	if oldestKey != "" {
		delete(r.tasks, oldestKey)
		return true
	}
	return false
}

func waitForDone(t *shellTask, max time.Duration) bool {
	timer := time.NewTimer(max)
	defer timer.Stop()
	select {
	case <-t.done:
		return true
	case <-timer.C:
		return false
	}
}

func waitTaskDrained(t *shellTask, max time.Duration) {
	if t.isRunning() {
		return
	}
	timer := time.NewTimer(max)
	defer timer.Stop()
	select {
	case <-t.drained:
	case <-timer.C:
	}
}

func drainToRing(t *shellTask, reader io.Reader) {
	if reader == nil {
		return
	}
	buf := make([]byte, 32*1024)
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			t.appendLogs(buf[:n])
		}
		if err != nil {
			if err != io.EOF && !isPipeClosedError(err) {
				t.appendLogs([]byte(fmt.Sprintf("[read error: %v]\n", err)))
			}
			return
		}
	}
}

func drainProcessOutput(t *shellTask, proc processHandle) {
	var readers sync.WaitGroup
	for _, reader := range []io.Reader{proc.Stdout(), proc.Stderr()} {
		readers.Go(func() {
			drainToRing(t, reader)
		})
	}
	go func() {
		readers.Wait()
		close(t.drained)
	}()
}

func isPipeClosedError(err error) bool {
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		return errors.Is(pathErr.Err, os.ErrClosed)
	}
	return errors.Is(err, os.ErrClosed)
}

func formatDuration(d time.Duration) string {
	if d < time.Second {
		return d.Truncate(time.Millisecond).String()
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
}
