package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"nekocode/mode"
	"nekocode/protocol"
)

const backgroundLimit = 8
const backgroundQueueLimit = 128
const backgroundHistoryLimit = 256
const backgroundResultLimit = 32 << 10
const backgroundDependencyRetryMin = 250 * time.Millisecond
const backgroundDependencyRetryMax = 5 * time.Second

type BackgroundUsage struct {
	TotalTokens     int   `json:"total_tokens"`
	CacheHitTokens  int   `json:"cache_hit_tokens"`
	CacheMissTokens int   `json:"cache_miss_tokens"`
	ToolUseCount    int   `json:"tool_use_count"`
	DurationMs      int64 `json:"duration_ms"`
}

type BackgroundTask struct {
	Isolation        string          `json:"isolation,omitempty"`
	SchedulingNote   string          `json:"scheduling_note,omitempty"`
	QueueReason      string          `json:"queue_reason,omitempty"`
	ResultTruncated  bool            `json:"result_truncated,omitempty"`
	After            []string        `json:"after,omitempty"`
	Usage            BackgroundUsage `json:"usage"`
	AttemptUsage     BackgroundUsage `json:"attempt_usage"`
	DeliveryError    string          `json:"delivery_error,omitempty"`
	DeliveryAttempts int             `json:"delivery_attempts,omitempty"`
	AttemptID        string          `json:"attempt_id,omitempty"`
	AttemptSeq       int             `json:"attempt_seq,omitempty"`
	Prompt           string          `json:"prompt,omitempty"`
	UpdateSequence   uint64          `json:"update_sequence,omitempty"`
	PendingUpdates   []string        `json:"pending_updates,omitempty"`
	ID               string          `json:"id"`
	SessionID        string          `json:"session_id"`
	GroupID          string          `json:"group_id"`
	Title            string          `json:"title"`
	Profile          string          `json:"profile"`
	Status           string          `json:"status"`
	Result           string          `json:"result,omitempty"`
	Error            string          `json:"error,omitempty"`
	Notified         bool            `json:"notified"`
	Delivery         string          `json:"delivery,omitempty"`
}

type backgroundEntry struct {
	recording     bool
	interactionID RunID
	workerStatus  string
	workerSummary protocol.RunSummary
	BackgroundTask
	work   protocol.BackgroundWork
	cancel context.CancelFunc
}

type backgroundInput struct {
	input     Input
	sessionID string
	done      chan struct{}
	id        RunID
	ctx       context.Context
	cancel    context.CancelFunc
	doneOnce  sync.Once
}

type backgroundTasks struct {
	retiredByTask      map[string]map[string]BackgroundUsage
	retired            map[string]BackgroundUsage
	runtime            *Runtime
	mu                 sync.Mutex
	ctx                context.Context
	cancel             context.CancelFunc
	wake               chan struct{}
	wg                 sync.WaitGroup
	closed             bool
	entries            map[string]*backgroundEntry
	order              []string
	loaded             map[string]bool
	active             int
	workspaces         map[string]bool
	inputs             []*backgroundInput
	pending            map[RunID]*backgroundInput
	lastGroup          string
	coordinating       bool
	coordinatorCancel  context.CancelFunc
	coordinatorSession string
	demands            map[RunID]*BackgroundDemand
	demandOrder        []RunID
}

func newBackgroundTasks(rt *Runtime) *backgroundTasks {
	ctx, cancel := context.WithCancel(context.Background())
	b := &backgroundTasks{retiredByTask: map[string]map[string]BackgroundUsage{}, retired: map[string]BackgroundUsage{}, runtime: rt, ctx: ctx, cancel: cancel, wake: make(chan struct{}, 1), entries: map[string]*backgroundEntry{}, loaded: map[string]bool{}, workspaces: map[string]bool{}, pending: map[RunID]*backgroundInput{}, demands: map[RunID]*BackgroundDemand{}}
	b.wg.Add(1)
	go func() { defer b.wg.Done(); b.loop() }()
	return b
}
func (b *backgroundTasks) signal() {
	select {
	case b.wake <- struct{}{}:
	default:
	}
}
func (b *backgroundTasks) busy() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.active > 0 || b.hasQueuedLocked() || len(b.pending) > 0 || b.coordinating
}
func (b *backgroundTasks) hasQueuedLocked() bool {
	for _, e := range b.entries {
		if e.Status == "queued" || e.Status == "waiting_dependencies" {
			return true
		}
	}
	return false
}

func (b *backgroundTasks) path(sessionID string) string {
	if fn := b.runtime.services.Sessions.BackgroundStatePath; fn != nil {
		return fn(sessionID)
	}
	return ""
}

// Load recovers receipts and notifications, never executable closures. In-flight
// work is marked interrupted and is not replayed after a process restart.
func (b *backgroundTasks) load(sessionID string) error {
	if sessionID == "" {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return fmt.Errorf("task service closed")
	}
	if b.loaded[sessionID] {
		return nil
	}
	path := b.path(sessionID)
	if path != "" {
		file, err := os.Open(path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err == nil {
			defer file.Close()
			info, err := file.Stat()
			if err != nil {
				return err
			}
			if info.Size() > 32<<20 {
				return fmt.Errorf("background state exceeds size limit")
			}
			var state backgroundState
			if err := json.NewDecoder(file).Decode(&state); err != nil {
				return fmt.Errorf("read background state: %w", err)
			}
			if (state.Version != 1 && state.Version != 2) || len(state.Tasks) > backgroundHistoryLimit || len(state.Demands) > backgroundHistoryLimit {
				return fmt.Errorf("unsupported background state")
			}
			b.retired[sessionID] = state.RetiredUsage
			b.retiredByTask[sessionID] = state.RetiredByTask
			seen := map[RunID]bool{}
			for _, d := range state.Demands {
				if !validBackgroundDemand(d, sessionID) || seen[d.ID] || b.demands[d.ID] != nil && b.demands[d.ID].SessionID != sessionID {
					return fmt.Errorf("invalid background demand receipt")
				}
				seen[d.ID] = true
			}
			seenTasks := map[string]bool{}
			for _, t := range state.Tasks {
				if t.ID == "" || t.SessionID != sessionID || seenTasks[t.ID] || b.entries[t.ID] != nil && b.entries[t.ID].SessionID != sessionID {
					return fmt.Errorf("invalid background task ownership")
				}
				seenTasks[t.ID] = true
			}
			for _, d := range state.Demands {
				if old := b.demands[d.ID]; old != nil {
					if old.SessionID != sessionID {
						return fmt.Errorf("demand identity collision")
					}
					continue
				}
				if d.Status == "queued" {
					d.Status = "pending"
				}
				if d.Status == "claimed" {
					d.Status = "interrupted"
					d.Error = "process stopped after admission; inspect the conversation before issuing a new demand"
				}
				recovered := d
				b.demands[d.ID] = &recovered
				b.demandOrder = append(b.demandOrder, d.ID)
			}
			for _, t := range state.Tasks {
				if t.SessionID != sessionID || t.ID == "" {
					return fmt.Errorf("invalid background task ownership")
				}
				if t.UpdateSequence < uint64(len(t.PendingUpdates)) {
					t.UpdateSequence = uint64(len(t.PendingUpdates))
				}
				if t.Delivery == "claimed" {
					t.Delivery = "failed"
				}
				if t.Status == "queued" || t.Status == "running" || strings.HasPrefix(t.Status, "waiting_") {
					t.Status = "interrupted"
					t.Error = "process stopped; resume explicitly after reviewing effects"
				}
				if _, exists := b.entries[t.ID]; exists {
					continue
				}
				if read := b.runtime.services.Sessions.SubagentUsage; read != nil {
					if usage, err := read(sessionID, t.ID); err == nil {
						// Canonical history includes attempts already owned by retired totals.
						usage = subtractRetiredUsage(usage, b.retiredByTask[sessionID][t.ID])
						if usage.TotalTokens >= t.Usage.TotalTokens {
							t.Usage = usage
						}
					}
				}
				b.entries[t.ID] = &backgroundEntry{BackgroundTask: t}
				b.order = append(b.order, t.ID)
			}
		}
	}
	if err := b.saveLocked(sessionID); err != nil {
		return err
	}
	b.loaded[sessionID] = true
	return nil
}

func (b *backgroundTasks) saveLocked(sessionID string) error {
	path := b.path(sessionID)
	if path == "" {
		return nil
	}
	tasks := make([]BackgroundTask, 0)
	for _, id := range b.order {
		if e := b.entries[id]; e != nil && e.SessionID == sessionID {
			tasks = append(tasks, e.BackgroundTask)
		}
	}
	demands := []BackgroundDemand{}
	for _, id := range b.demandOrder {
		if d := b.demands[id]; d.SessionID == sessionID {
			demands = append(demands, *d)
		}
	}
	data, err := json.Marshal(backgroundState{Version: 2, Tasks: tasks, Demands: demands, RetiredUsage: b.retired[sessionID], RetiredByTask: b.retiredByTask[sessionID]})
	if err != nil {
		return err
	}
	if len(data) > 32<<20 {
		return fmt.Errorf("background state exceeds size limit")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".background-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	err = errors.Join(err, f.Close())
	if err != nil {
		return err
	}
	return os.Rename(name, path)
}

func (h runHost) SubmitBackground(ctx context.Context, work protocol.BackgroundWork) (protocol.BackgroundReceipt, error) {
	snapshot, ok := mode.FromContext(ctx)
	if !ok || snapshot.ID != mode.SuperAgent {
		return protocol.BackgroundReceipt{}, fmt.Errorf("background tasks require Super Agent mode")
	}
	if _, err := mode.Validate(snapshot); err != nil {
		return protocol.BackgroundReceipt{}, err
	}
	if work.SessionID != h.runtime.CurrentSessionID() || work.ID == "" || work.Run == nil {
		return protocol.BackgroundReceipt{}, fmt.Errorf("invalid background execution identity")
	}
	var receipt protocol.BackgroundReceipt
	var err error
	if !h.lease.guard(func() { receipt, err = h.runtime.background.submit(ctx, work) }) {
		return receipt, fmt.Errorf("coordinator run is no longer active")
	}
	return receipt, err
}
func (h runHost) PendingBackgroundPlan(groupID string) int {
	sessionID := h.runtime.CurrentSessionID()
	b := h.runtime.background
	b.mu.Lock()
	defer b.mu.Unlock()
	count := 0
	for _, entry := range b.entries {
		if entry.SessionID != sessionID || entry.GroupID != groupID {
			continue
		}
		switch entry.Status {
		case "queued", "running", "waiting_dependencies", "waiting_approval", "waiting_question":
			count++
		}
	}
	return count
}

func (h runHost) CancelBackground(ctx context.Context, id string) error {
	return h.runtime.CancelBackgroundTask(ctx, h.runtime.CurrentSessionID(), id)
}

func (b *backgroundTasks) submit(ctx context.Context, work protocol.BackgroundWork) (protocol.BackgroundReceipt, error) {
	if err := ctx.Err(); err != nil {
		return protocol.BackgroundReceipt{}, err
	}
	if err := b.load(work.SessionID); err != nil {
		return protocol.BackgroundReceipt{}, err
	}
	b.mu.Lock()
	if b.closed || ctx.Err() != nil {
		b.mu.Unlock()
		return protocol.BackgroundReceipt{}, fmt.Errorf("task service closed or coordinator cancelled")
	}
	if len(work.After) > 8 {
		b.mu.Unlock()
		return protocol.BackgroundReceipt{}, fmt.Errorf("at most 8 dependencies")
	}
	seen := map[string]bool{}
	for _, id := range work.After {
		upstream := b.entries[id]
		if id == work.ID || seen[id] || upstream == nil || upstream.SessionID != work.SessionID {
			b.mu.Unlock()
			return protocol.BackgroundReceipt{}, fmt.Errorf("dependency must be a distinct accepted task in this session")
		}
		seen[id] = true
	}
	if b.dependedOnLocked(work.ID) {
		b.mu.Unlock()
		return protocol.BackgroundReceipt{}, fmt.Errorf("task result is leased by a dependent task")
	}
	old := b.entries[work.ID]
	if old != nil && old.SessionID != work.SessionID {
		b.mu.Unlock()
		return protocol.BackgroundReceipt{}, fmt.Errorf("task belongs to another session")
	}
	// A durable task belongs to its original demand, across execution attempts.
	// Legacy tasks without an owner adopt the supplied group once.
	if old != nil && old.GroupID != "" {
		coordination := protocol.CoordinationFromContext(ctx)
		if coordination.Notification && coordination.GroupID != old.GroupID {
			b.mu.Unlock()
			return protocol.BackgroundReceipt{}, fmt.Errorf("completion notification cannot resume another demand")
		}
		work.GroupID = old.GroupID
	}
	if old != nil && (old.Status == "queued" || old.Status == "waiting_dependencies" || old.cancel != nil || (!old.Notified && old.Delivery != "failed" && !(old.Delivery == "claimed" && old.GroupID == work.GroupID))) {
		b.mu.Unlock()
		return protocol.BackgroundReceipt{}, fmt.Errorf("task is active or its result has not been consumed")
	}
	queued := 0
	for _, e := range b.entries {
		if e.Status == "queued" || e.Status == "waiting_dependencies" {
			queued++
		}
	}
	if queued >= backgroundQueueLimit {
		b.mu.Unlock()
		return protocol.BackgroundReceipt{}, fmt.Errorf("background queue is full")
	}
	// Retain only consumed terminal history when making space. Unconsumed
	// results and executable jobs cannot be silently dropped.
	for old == nil && len(b.entries) >= backgroundHistoryLimit {
		evicted := false
		for i, id := range b.order {
			if e := b.entries[id]; e != nil && !b.dependedOnLocked(e.ID) && e.Notified && len(e.PendingUpdates) == 0 && e.cancel == nil && (e.Status != "queued" && e.Status != "waiting_dependencies") {
				b.retireUsageLocked(e)
				delete(b.entries, id)
				b.order = append(b.order[:i], b.order[i+1:]...)
				evicted = true
				break
			}
		}
		if !evicted {
			break
		}
	}
	if old == nil && len(b.entries) >= backgroundHistoryLimit {
		b.mu.Unlock()
		return protocol.BackgroundReceipt{}, fmt.Errorf("background history full; consume pending results first")
	}
	entry := &backgroundEntry{interactionID: RunID("task_scope_" + uuid.NewString()), BackgroundTask: BackgroundTask{Prompt: work.Prompt, UpdateSequence: work.UpdateSequenceFloor, ID: work.ID, SessionID: work.SessionID, GroupID: work.GroupID, Title: work.Title, Profile: work.Profile, Status: "queued"}, work: work}
	if old != nil {
		entry.PendingUpdates = append([]string(nil), old.PendingUpdates...)
		// Retained sidecar history may lag behind the child's committed receipt.
		// Never let it lower the floor used to assign newly appended updates.
		entry.UpdateSequence = max(entry.UpdateSequence, old.UpdateSequence, uint64(len(old.PendingUpdates)))
	}
	entry.After = append([]string(nil), work.After...)
	entry.Isolation, entry.SchedulingNote = work.Isolation, work.SchedulingNote
	if len(entry.After) > 0 {
		entry.Status = "waiting_dependencies"
	}
	if old != nil {
		entry.Usage = old.Usage
	}
	b.entries[work.ID] = entry
	if old == nil {
		b.order = append(b.order, work.ID)
	}
	if err := b.saveLocked(work.SessionID); err != nil {
		if old == nil {
			delete(b.entries, work.ID)
			b.order = b.order[:len(b.order)-1]
		} else {
			b.entries[work.ID] = old
		}
		b.mu.Unlock()
		return protocol.BackgroundReceipt{}, err
	}
	b.mu.Unlock()
	b.runtime.publishStepForSession(RunID("task_"+work.ID), protocol.StepEvent{Action: protocol.StepActionSubAgentStart, SubAgentID: work.ID, SubAgentProfile: work.Profile, SubAgentType: work.Profile, SubAgentStatus: "queued", SubAgentPrompt: protocol.SubagentTextPreview(work.Prompt, 1024)}, work.SessionID)
	b.signal()
	return protocol.BackgroundReceipt{ID: work.ID, GroupID: work.GroupID, Status: "queued"}, nil
}

func (b *backgroundTasks) loop() {
	// The service loop owns the timer so retries are bounded, deduplicated and
	// cancelled with the runtime, without detached timer callbacks.
	var timer *time.Timer
	var retry <-chan time.Time
	delay := backgroundDependencyRetryMin
	retrying := false
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()
	for {
		select {
		case <-b.ctx.Done():
			return
		case <-b.wake:
		case <-retry:
			retry = nil
		}
		if err := b.dispatch(); err != nil {
			if !retrying {
				b.runtime.Notify("Cannot persist task dependencies; retrying: " + err.Error())
			}
			retrying = true
			if retry == nil {
				if timer == nil {
					timer = time.NewTimer(delay)
				} else {
					timer.Reset(delay)
				}
				retry = timer.C
				delay = min(2*delay, backgroundDependencyRetryMax)
			}
		} else {
			if timer != nil {
				timer.Stop()
			}
			retry = nil
			delay = backgroundDependencyRetryMin
			retrying = false
		}
		b.coordinate()
	}
}

func (b *backgroundTasks) dispatch() error {
	b.runtime.mutationMu.Lock()
	defer b.runtime.mutationMu.Unlock()
	limit, foregroundWriter := b.runtime.backgroundScheduling()
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil
	}
	dependencyErr := b.resolveDependenciesLocked()
	for b.active < limit {
		var next *backgroundEntry
		// Prefer a different demand group to avoid one fan-out monopolizing slots.
		for pass := 0; pass < 2 && next == nil; pass++ {
			for _, id := range b.order {
				e := b.entries[id]
				if e.Status != "queued" || e.work.Run == nil {
					continue
				}
				if !e.work.ReadOnly && (foregroundWriter || b.workspaces[e.work.Workspace]) {
					continue
				}
				if pass == 0 && e.GroupID == b.lastGroup {
					continue
				}
				next = e
				break
			}
		}
		if next == nil {
			return dependencyErr
		}
		e := next
		ctx, cancel := context.WithCancel(b.ctx)
		e.cancel = cancel
		e.recording = true
		e.Status = "running"
		b.active++
		b.lastGroup = e.GroupID
		if !e.work.ReadOnly {
			b.workspaces[e.work.Workspace] = true
		}
		b.wg.Add(1)
		go func() { defer b.wg.Done(); b.execute(ctx, e) }()
	}
	return dependencyErr
}

func (b *backgroundTasks) execute(ctx context.Context, e *backgroundEntry) {
	b.runtime.mu.Lock()
	recorder := b.runtime.recorder
	b.runtime.mu.Unlock()
	if recorder != nil {
		if err := recorder.BeginRun(e.interactionID, e.SessionID); err != nil {
			b.runtime.Notify("Cannot archive background execution: " + err.Error())
		}
		defer b.runtime.releaseRecordedRun(e.interactionID)
	}
	ctx = protocol.WithCoordination(ctx, protocol.Coordination{GroupID: e.GroupID})
	host := backgroundHost{runtime: b.runtime, ctx: ctx, id: e.ID, sessionID: e.SessionID, interactionID: e.interactionID}
	var result string
	var err error
	func() {
		defer func() {
			if p := recover(); p != nil {
				err = fmt.Errorf("background execution panicked: %v", p)
			}
		}()
		result, err = e.work.Run(ctx, host)
	}()
	status := "completed"
	if err != nil {
		status = "failed"
	}
	b.mu.Lock()
	// The worker reports its outcome without publishing a terminal event.
	// Runtime owns settlement and applies cancellation with final priority.
	if err == nil {
		switch e.workerStatus {
		case "partial", "failed", "interrupted":
			status = e.workerStatus
		}
	}
	// Cancellation has final priority, including while waiting for the queue lock.
	if ctx.Err() != nil {
		status = "interrupted"
	}
	e.Status = status
	e.ResultTruncated = len(result) > backgroundResultLimit
	e.Result = boundedBackgroundText(result)
	if err != nil {
		e.Error = boundedBackgroundText(err.Error())
	}
	cancel := e.cancel
	e.cancel = nil
	releaseBackgroundWork(e)
	e.work.Run = nil
	b.active--
	if !e.work.ReadOnly {
		delete(b.workspaces, e.work.Workspace)
	}
	attemptID, attemptSeq := e.AttemptID, e.AttemptSeq
	summary := e.workerSummary
	saveErr := b.saveLocked(e.SessionID)
	b.mu.Unlock()
	cancel()
	b.runtime.approvals.RejectRun(e.interactionID)
	b.runtime.questions.RejectRun(e.interactionID)
	b.runtime.publishStepForSession(RunID("task_"+e.ID), protocol.StepEvent{Action: protocol.StepActionSubAgentEnd, SubAgentID: e.ID, SubAgentStatus: status, SubAgentAttemptID: attemptID, SubAgentAttemptSeq: attemptSeq, Summary: &summary}, e.SessionID)
	if saveErr != nil {
		b.runtime.Notify("Background result persistence failed: " + saveErr.Error())
	}
	b.signal()
}
func boundedBackgroundText(text string) string {
	if len(text) > backgroundResultLimit {
		return backgroundTextPrefix(text, backgroundResultLimit) + "\n[truncated; inspect saved task output]"
	}
	return text
}

// BackgroundTasks exposes receipts independently of foreground RunStatus.
func (r *Runtime) BackgroundTasks(sessionID string) ([]BackgroundTask, error) {
	if err := r.ensureOpen(); err != nil {
		return nil, err
	}
	if sessionID == "" {
		sessionID = r.CurrentSessionID()
	}
	if err := r.background.load(sessionID); err != nil {
		return nil, err
	}
	limit, foregroundWriter := r.backgroundScheduling()
	r.background.mu.Lock()
	defer r.background.mu.Unlock()
	out := make([]BackgroundTask, 0)
	for _, id := range r.background.order {
		if e := r.background.entries[id]; e.SessionID == sessionID {
			task := e.BackgroundTask
			task.QueueReason = r.background.queueReasonLocked(e, limit, foregroundWriter)
			task.Prompt = protocol.SubagentTextPreview(task.Prompt, 1024)
			task.PendingUpdates = append([]string(nil), task.PendingUpdates...)
			task.After = append([]string(nil), task.After...)
			out = append(out, task)
		}
	}
	return out, nil
}
func (r *Runtime) CancelBackgroundTask(ctx context.Context, sessionID, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := r.ensureOpen(); err != nil {
		return err
	}
	b := r.background
	if err := b.load(sessionID); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	b.mu.Lock()
	e := b.entries[id]
	if e == nil || e.SessionID != sessionID {
		b.mu.Unlock()
		return fmt.Errorf("background task not found in session")
	}
	cancel := e.cancel
	interactionID := e.interactionID
	queued := (e.Status == "queued" || e.Status == "waiting_dependencies")
	if queued {
		e.Status = "interrupted"
		e.Error = "cancelled before execution"
		releaseBackgroundWork(e)
		e.work.Run = nil
	}
	err := b.saveLocked(sessionID)
	b.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	r.approvals.RejectRun(interactionID)
	r.questions.RejectRun(interactionID)
	if queued {
		r.publishStepForSession(RunID("task_"+id), protocol.StepEvent{Action: protocol.StepActionSubAgentEnd, SubAgentID: id, SubAgentStatus: "interrupted"}, sessionID)
	}
	b.signal()
	return err
}

func (b *backgroundTasks) close() {
	b.mu.Lock()
	b.closed = true
	b.cancel()
	for _, e := range b.entries {
		if e.cancel != nil {
			e.cancel()
		}
		if e.Status == "queued" || e.Status == "waiting_dependencies" {
			e.Status = "interrupted"
			e.Error = "runtime closed before execution"
			releaseBackgroundWork(e)
			e.work.Run = nil
		}
	}
	for id, in := range b.pending {
		if d := b.demands[id]; d != nil && d.Status == "claimed" {
			d.Status = "interrupted"
			d.Error = "runtime stopped after admission"
		}
		in.cancel()
		in.finish()
	}
	b.pending = map[RunID]*backgroundInput{}
	b.inputs = nil
	for sessionID := range b.loaded {
		if err := b.saveLocked(sessionID); err != nil {
			b.runtime.Notify("Background shutdown persistence failed: " + err.Error())
		}
	}
	b.mu.Unlock()
	// Reject waits before joining executors, including when the foreground is idle.
	b.runtime.approvals.Close()
	b.runtime.questions.Close()
	b.wg.Wait()
}

func (in *backgroundInput) finish() { in.doneOnce.Do(func() { close(in.done) }) }

// BackgroundTaskPrompt returns the accepted task instruction before a child
// transcript exists. Empty text allows legacy non-background tasks to fall back.
func (r *Runtime) BackgroundTaskPrompt(sessionID, id string) (string, error) {
	if err := r.ensureOpen(); err != nil {
		return "", err
	}
	if sessionID == "" {
		sessionID = r.CurrentSessionID()
	}
	b := r.background
	if err := b.load(sessionID); err != nil {
		return "", err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	e := b.entries[id]
	if e == nil || e.SessionID != sessionID {
		return "", nil
	}
	return e.Prompt, nil
}
