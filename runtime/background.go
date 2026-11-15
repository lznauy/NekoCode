package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"os"
	"path/filepath"
	"sync"
	"time"

	"nekocode/mode"
	"nekocode/protocol"
	"nekocode/util/modelbudget"
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
	ResultVersion    int                  `json:"result_version,omitempty"`
	ResultRef        string               `json:"result_ref,omitempty"`
	ReviewPolicy     string               `json:"review_policy,omitempty"`
	Review           *protocol.TaskReview `json:"review,omitempty"`
	ReviewComplete   bool                 `json:"review_complete,omitempty"`
	Isolation        string               `json:"isolation,omitempty"`
	SchedulingNote   string               `json:"scheduling_note,omitempty"`
	QueueReason      string               `json:"queue_reason,omitempty"`
	ResultTruncated  bool                 `json:"result_truncated,omitempty"`
	After            []string             `json:"after,omitempty"`
	Usage            BackgroundUsage      `json:"usage"`
	AttemptUsage     BackgroundUsage      `json:"attempt_usage"`
	DeliveryError    string               `json:"delivery_error,omitempty"`
	DeliveryAttempts int                  `json:"delivery_attempts,omitempty"`
	AttemptID        string               `json:"attempt_id,omitempty"`
	AttemptSeq       int                  `json:"attempt_seq,omitempty"`
	Prompt           string               `json:"prompt,omitempty"`
	UpdateSequence   uint64               `json:"update_sequence,omitempty"`
	PendingUpdates   []string             `json:"pending_updates,omitempty"`
	ID               string               `json:"id"`
	SessionID        string               `json:"session_id"`
	GroupID          string               `json:"group_id"`
	Title            string               `json:"title"`
	Profile          string               `json:"profile"`
	Status           string               `json:"status"`
	Result           string               `json:"result,omitempty"`
	Error            string               `json:"error,omitempty"`
	Notified         bool                 `json:"notified"`
	Delivery         string               `json:"delivery,omitempty"`
}

type backgroundEntry struct {
	callBudget    *modelbudget.Budget
	recording     bool
	interactionID RunID
	workerStatus  string
	settling      bool // completion owns the attempt after this boundary
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
	results            map[string][]byte // Only used by hosts without persistent session storage.
	retiredByTask      map[string]map[string]BackgroundUsage
	retired            map[string]BackgroundUsage
	runtime            *Runtime
	mu                 sync.Mutex // state only; never held across storage I/O
	mutationMu         sync.Mutex // ordered persistent mutations, including rollback
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

// Persistent writers take mutationMu before mu. Storage drops only mu, so
// readers, worker progress and cancellation remain independent of disk latency.
func (b *backgroundTasks) lockMutation() {
	b.mutationMu.Lock()
	b.mu.Lock()
}
func (b *backgroundTasks) unlockMutation() {
	b.mu.Unlock()
	b.mutationMu.Unlock()
}

// load recovers receipts and notifications, never executable closures.
func (b *backgroundTasks) load(sessionID string) error {
	if sessionID == "" {
		return nil
	}
	// Most readers use an already loaded session and need no persistence lock.
	b.mu.Lock()
	loaded, closed := b.loaded[sessionID], b.closed
	b.mu.Unlock()
	if closed {
		return fmt.Errorf("task service closed")
	}
	if loaded {
		return nil
	}
	b.lockMutation()
	defer b.unlockMutation()
	if b.closed {
		return fmt.Errorf("task service closed")
	}
	if b.loaded[sessionID] {
		return nil
	}
	b.mu.Unlock()
	state, err := b.readState(sessionID)
	b.mu.Lock()
	if err != nil {
		return err
	}
	seen := map[RunID]bool{}
	for _, d := range state.Demands {
		if !validBackgroundDemand(d, sessionID) || seen[d.ID] || b.demands[d.ID] != nil && b.demands[d.ID].SessionID != sessionID {
			return fmt.Errorf("invalid background demand receipt")
		}
		seen[d.ID] = true
	}
	seenTasks := map[string]bool{}
	for _, task := range state.Tasks {
		if task.ID == "" || task.SessionID != sessionID || seenTasks[task.ID] || b.entries[task.ID] != nil && b.entries[task.ID].SessionID != sessionID {
			return fmt.Errorf("invalid background task ownership")
		}
		seenTasks[task.ID] = true
	}
	b.retired[sessionID] = state.RetiredUsage
	b.retiredByTask[sessionID] = state.RetiredByTask
	for _, d := range state.Demands {
		if b.demands[d.ID] != nil {
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
	for _, task := range state.Tasks {
		task.UpdateSequence = max(task.UpdateSequence, uint64(len(task.PendingUpdates)))
		if task.Delivery == "claimed" {
			task.Delivery = "failed"
		}
		if pendingBackgroundStatus(task.Status) {
			task.Status = "interrupted"
			task.Error = "process stopped; resume explicitly after reviewing effects"
		}
		if b.entries[task.ID] == nil {
			b.entries[task.ID] = &backgroundEntry{BackgroundTask: task}
			b.order = append(b.order, task.ID)
		}
	}
	if err := b.saveLocked(sessionID); err != nil {
		return err
	}
	b.loaded[sessionID] = true
	return nil
}

// readState owns only local recovery data. The caller serializes installation
// with persistent mutations; reading files and child usage needs no state lock.
func (b *backgroundTasks) readState(sessionID string) (backgroundState, error) {
	var state backgroundState
	path := b.path(sessionID)
	if path == "" {
		return state, nil
	}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return state, err
	}
	if info.Size() > 32<<20 {
		return state, fmt.Errorf("background state exceeds size limit")
	}
	if err := json.NewDecoder(file).Decode(&state); err != nil {
		return state, fmt.Errorf("read background state: %w", err)
	}
	if (state.Version != 1 && state.Version != 2) || len(state.Tasks) > backgroundHistoryLimit || len(state.Demands) > backgroundHistoryLimit {
		return state, fmt.Errorf("unsupported background state")
	}
	for i := range state.Tasks {
		task := &state.Tasks[i]
		if task.ID == "" || task.SessionID != sessionID {
			return state, fmt.Errorf("invalid background task ownership")
		}
		if err := b.recoverResult(task); err != nil {
			return state, fmt.Errorf("recover background result for task %q: %w", task.ID, err)
		}
		if read := b.runtime.services.Sessions.SubagentUsage; read != nil {
			if usage, err := read(sessionID, task.ID); err == nil {
				usage = subtractRetiredUsage(usage, state.RetiredByTask[task.ID])
				if usage.TotalTokens >= task.Usage.TotalTokens {
					task.Usage = usage
				}
			}
		}
	}
	return state, nil
}

// Caller holds mu. Mutable collections are owned by persistent writers;
// progress callbacks change only copied scalar status and attempt fields.
func (b *backgroundTasks) snapshotLocked(sessionID string) backgroundState {
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
	return backgroundState{Version: 2, Tasks: tasks, Demands: demands, RetiredUsage: b.retired[sessionID], RetiredByTask: b.retiredByTask[sessionID]}
}

// saveTaskLocked keeps a proposed receipt private until storage accepts it.
func (b *backgroundTasks) saveTaskLocked(e *backgroundEntry, next BackgroundTask) error {
	before := e.BackgroundTask
	state := b.snapshotLocked(next.SessionID)
	found := false
	for i := range state.Tasks {
		if state.Tasks[i].ID == next.ID {
			state.Tasks[i] = next
			found = true
			break
		}
	}
	if !found {
		state.Tasks = append(state.Tasks, next)
	}
	if err := b.saveStateLocked(next.SessionID, state); err != nil {
		return err
	}
	// Unrelated worker observations arriving during storage must survive an
	// update that did not itself change these fields.
	if next.Status == before.Status {
		next.Status = e.Status
	}
	if next.AttemptSeq == before.AttemptSeq {
		next.AttemptSeq = e.AttemptSeq
	}
	e.BackgroundTask = next
	return nil
}

// Caller holds mutationMu and mu. Retaining mutationMu prevents an older
// snapshot or rollback from overwriting a newer accepted mutation.
func (b *backgroundTasks) saveLocked(sessionID string) error {
	return b.saveStateLocked(sessionID, b.snapshotLocked(sessionID))
}

func (b *backgroundTasks) saveStateLocked(sessionID string, state backgroundState) error {
	b.mu.Unlock()
	defer b.mu.Lock()
	path := b.path(sessionID)
	if path == "" {
		return nil
	}
	data, err := json.Marshal(state)
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
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	if err = errors.Join(err, f.Close()); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
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
	budget := modelbudget.FromContext(ctx)
	if budget != nil && budget.Snapshot().WorkRemaining == 0 {
		return protocol.BackgroundReceipt{}, modelbudget.ErrExhausted
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
	if work.ReviewPolicy != "" && work.ReviewPolicy != "immediate" && work.ReviewPolicy != "batch" {
		return protocol.BackgroundReceipt{}, fmt.Errorf("invalid review_policy")
	}
	if err := b.load(work.SessionID); err != nil {
		return protocol.BackgroundReceipt{}, err
	}
	b.lockMutation()
	if b.closed || ctx.Err() != nil {
		b.unlockMutation()
		return protocol.BackgroundReceipt{}, fmt.Errorf("task service closed or coordinator cancelled")
	}
	if len(work.After) > 8 {
		b.unlockMutation()
		return protocol.BackgroundReceipt{}, fmt.Errorf("at most 8 dependencies")
	}
	seen := map[string]bool{}
	for _, id := range work.After {
		upstream := b.entries[id]
		if id == work.ID || seen[id] || upstream == nil || upstream.SessionID != work.SessionID {
			b.unlockMutation()
			return protocol.BackgroundReceipt{}, fmt.Errorf("dependency must be a distinct accepted task in this session")
		}
		seen[id] = true
	}
	if b.dependedOnLocked(work.ID) {
		b.unlockMutation()
		return protocol.BackgroundReceipt{}, fmt.Errorf("task result is leased by a dependent task")
	}
	old := b.entries[work.ID]
	if old != nil && old.SessionID != work.SessionID {
		b.unlockMutation()
		return protocol.BackgroundReceipt{}, fmt.Errorf("task belongs to another session")
	}
	if old != nil && work.ReviewPolicy == "" {
		work.ReviewPolicy = old.ReviewPolicy
	}
	// A durable task belongs to its original demand, across execution attempts.
	// Legacy tasks without an owner adopt the supplied group once.
	if old != nil && old.GroupID != "" {
		coordination := protocol.CoordinationFromContext(ctx)
		if coordination.Notification && coordination.GroupID != old.GroupID {
			b.unlockMutation()
			return protocol.BackgroundReceipt{}, fmt.Errorf("completion notification cannot resume another demand")
		}
		work.GroupID = old.GroupID
	}
	if old != nil && (old.Status == "queued" || old.Status == "waiting_dependencies" || old.cancel != nil || (!old.Notified && old.Delivery != "failed" && !(old.Delivery == "claimed" && old.GroupID == work.GroupID))) {
		var err error
		if old.Status == "queued" || old.Status == "waiting_dependencies" || old.cancel != nil {
			err = fmt.Errorf("task %q is still active (status=%s); use task_list to inspect its status, wait for the completion notification, then read task_result or resume", work.ID, old.Status)
		} else {
			err = fmt.Errorf("%w: task %q has a terminal result whose notification is not settled (delivery=%q, notified=%t); read task_result and use that report, then resume only after task_list shows delivery=delivered or notified=true", protocol.ErrBackgroundResumeDeliveryPending, work.ID, old.Delivery, old.Notified)
		}
		b.unlockMutation()
		return protocol.BackgroundReceipt{}, err
	}
	queued := 0
	for _, e := range b.entries {
		if e.Status == "queued" || e.Status == "waiting_dependencies" {
			queued++
		}
	}
	if queued >= backgroundQueueLimit {
		b.unlockMutation()
		return protocol.BackgroundReceipt{}, fmt.Errorf("background queue is full")
	}
	// Retain only consumed terminal history when making space. Unconsumed
	// results and executable jobs cannot be silently dropped.
	for old == nil && len(b.entries) >= backgroundHistoryLimit {
		evicted := false
		for _, id := range b.order {
			if e := b.entries[id]; e != nil && !b.dependedOnLocked(e.ID) && e.Notified && len(e.PendingUpdates) == 0 && e.cancel == nil && (e.Status != "queued" && e.Status != "waiting_dependencies") {
				if err := b.removeTaskLocked(e); err != nil {
					b.unlockMutation()
					return protocol.BackgroundReceipt{}, err
				}
				evicted = true
				break
			}
		}
		if !evicted {
			break
		}
	}
	if old == nil && len(b.entries) >= backgroundHistoryLimit {
		b.unlockMutation()
		return protocol.BackgroundReceipt{}, fmt.Errorf("background history full; consume pending results first")
	}
	entry := &backgroundEntry{callBudget: modelbudget.FromContext(ctx), interactionID: RunID("task_scope_" + uuid.NewString()), BackgroundTask: BackgroundTask{ResultVersion: 1, Prompt: work.Prompt, UpdateSequence: work.UpdateSequenceFloor, ID: work.ID, SessionID: work.SessionID, GroupID: work.GroupID, Title: work.Title, Profile: work.Profile, Status: "queued"}, work: work}
	if old != nil {
		entry.PendingUpdates = append([]string(nil), old.PendingUpdates...)
		// Retained sidecar history may lag behind the child's committed receipt.
		// Never let it lower the floor used to assign newly appended updates.
		entry.UpdateSequence = max(entry.UpdateSequence, old.UpdateSequence, uint64(len(old.PendingUpdates)))
	}
	entry.After = append([]string(nil), work.After...)
	entry.ReviewPolicy = work.ReviewPolicy
	entry.Isolation, entry.SchedulingNote = work.Isolation, work.SchedulingNote
	if len(entry.After) > 0 {
		entry.Status = "waiting_dependencies"
	}
	if old != nil {
		entry.Usage = old.Usage
		entry.AttemptSeq = old.AttemptSeq
	}
	if err := b.saveTaskLocked(entry, entry.BackgroundTask); err != nil {
		b.unlockMutation()
		return protocol.BackgroundReceipt{}, err
	}
	b.entries[work.ID] = entry
	if old == nil {
		b.order = append(b.order, work.ID)
	}
	b.unlockMutation()
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
				b.runtime.Notify("Cannot persist background scheduling; retrying: " + err.Error())
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
	b.lockMutation()
	defer b.unlockMutation()
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
		admitted := e.BackgroundTask
		admitted.AttemptID = uuid.NewString()
		admitted.AttemptSeq++
		admitted.Status = "running"
		if err := b.saveTaskLocked(e, admitted); err != nil {
			return errors.Join(dependencyErr, fmt.Errorf("persist background attempt admission: %w", err))
		}
		ctx, cancel := context.WithCancel(modelbudget.WithRole(modelbudget.WithBudget(b.ctx, e.callBudget), modelbudget.Work, e.ID))
		e.cancel = cancel
		e.recording = true
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
	// Receipt updates replace BackgroundTask under mu. Capture the execution
	// identity once rather than reading even unchanged fields concurrently.
	b.mu.Lock()
	taskID, sessionID, groupID, interactionID := e.ID, e.SessionID, e.GroupID, e.interactionID
	identity := protocol.ExecutionIdentity{RunID: "task_" + taskID, TaskID: taskID, AttemptID: e.AttemptID}
	run := e.work.Run
	b.mu.Unlock()
	b.runtime.mu.Lock()
	recorder := b.runtime.recorder
	b.runtime.mu.Unlock()
	if recorder != nil {
		if err := recorder.BeginRun(interactionID, sessionID); err != nil {
			b.runtime.Notify("Cannot archive background execution: " + err.Error())
		}
		defer b.runtime.releaseRecordedRun(interactionID)
	}
	ctx = protocol.WithCoordination(ctx, protocol.Coordination{GroupID: groupID})
	ctx = protocol.WithExecutionIdentity(ctx, identity)
	ctx = protocol.WithBackgroundResultReader(ctx, runHost{runtime: b.runtime})
	host := backgroundHost{runtime: b.runtime, ctx: ctx, id: taskID, sessionID: sessionID, interactionID: interactionID}
	var result protocol.BackgroundResult
	var err error
	func() {
		defer func() {
			if p := recover(); p != nil {
				err = fmt.Errorf("background execution panicked: %v", p)
			}
		}()
		result, err = run(ctx, host)
	}()
	status := "completed"
	if err != nil {
		status = "failed"
	}
	b.lockMutation()
	// The worker reports its outcome without publishing a terminal event.
	// Runtime owns settlement and applies cancellation with final priority.
	if err == nil {
		switch e.workerStatus {
		case "partial", "failed", "interrupted":
			status = e.workerStatus
		}
	}
	// The public host contract owns settlement, independently of any subagent
	// bridge callbacks. Unfinished handoffs can never satisfy successful deps.
	if status == "completed" && result.Review != nil && protocol.HandoffIncomplete(result.Review.Outcome, result.Review.Unfinished) {
		status = "partial"
	}
	// Cancellation has final priority, including while waiting for the queue lock.
	if ctx.Err() != nil {
		status = "interrupted"
	}
	// This short state-locked decision orders completion against cancellation.
	// Later cancellation cannot rewrite an immutable attempt result.
	e.settling = true
	next, commitErr := b.commitResultLocked(e, result, status, err)
	if commitErr != nil {
		err = errors.Join(err, fmt.Errorf("persist background result: %w", commitErr))
		status = "failed"
		if ctx.Err() != nil {
			status = "interrupted"
		}
		next = e.BackgroundTask
		next.ResultRef, next.Review, next.ReviewComplete = "", nil, false
		next.Result, next.ResultTruncated = "", false
		next.Error = boundedBackgroundText(err.Error())
	}
	next.Status = status
	saveErr := b.saveTaskLocked(e, next)
	if saveErr != nil {
		// The canonical outcome is already immutable; only delivery failed.
		e.BackgroundTask = next
		e.Delivery = "failed"
		e.DeliveryError = boundedBackgroundText("Background result receipt persistence failed: " + saveErr.Error())
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
	b.unlockMutation()
	cancel()
	b.runtime.approvals.RejectRun(interactionID)
	b.runtime.questions.RejectRun(interactionID)
	b.runtime.publishStepForSession(RunID("task_"+taskID), protocol.StepEvent{Action: protocol.StepActionSubAgentEnd, SubAgentID: taskID, SubAgentStatus: status, SubAgentAttemptID: attemptID, SubAgentAttemptSeq: attemptSeq, Summary: &summary}, sessionID)
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
			task.Review = cloneTaskReview(task.Review)
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
	// Running cancellation changes no durable state. Signal it under the short
	// state lock; execute will persist the final outcome after the worker exits.
	b.mu.Lock()
	if err := ctx.Err(); err != nil {
		b.mu.Unlock()
		return err
	}
	e := b.entries[id]
	if e == nil || e.SessionID != sessionID {
		b.mu.Unlock()
		return fmt.Errorf("background task not found in session")
	}
	queued := e.Status == "queued" || e.Status == "waiting_dependencies"
	if !queued {
		cancel := e.cancel
		owner := e.interactionID
		if e.settling {
			cancel = nil
		}
		if cancel != nil {
			cancel()
		}
		b.mu.Unlock()
		if cancel != nil {
			r.approvals.RejectRun(owner)
			r.questions.RejectRun(owner)
		}
		return nil
	}
	b.mu.Unlock()

	// A queued task may have started while waiting for the persistent writer.
	// Re-select its current execution before changing or saving its receipt.
	b.lockMutation()
	if err := ctx.Err(); err != nil {
		b.unlockMutation()
		return err
	}
	e = b.entries[id]
	if e == nil || e.SessionID != sessionID {
		b.unlockMutation()
		return fmt.Errorf("background task not found in session")
	}
	cancel, owner := e.cancel, e.interactionID
	settling := e.settling
	queued = e.Status == "queued" || e.Status == "waiting_dependencies"
	if !settling && cancel != nil {
		cancel()
	}
	var err error
	if queued {
		next := e.BackgroundTask
		next.Status = "interrupted"
		next.Error = "cancelled before execution"
		err = b.saveTaskLocked(e, next)
		if err == nil {
			releaseBackgroundWork(e)
			e.work.Run = nil
		}
	}
	b.unlockMutation()
	if !settling {
		r.approvals.RejectRun(owner)
		r.questions.RejectRun(owner)
	}
	if queued && err == nil {
		r.publishStepForSession(RunID("task_"+id), protocol.StepEvent{Action: protocol.StepActionSubAgentEnd, SubAgentID: id, SubAgentStatus: "interrupted"}, sessionID)
	}
	b.signal()
	return err
}

func (b *backgroundTasks) close() {
	// Cancellation does not wait for pending storage commits.
	b.cancel()
	b.lockMutation()
	b.closed = true
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
	b.unlockMutation()
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
