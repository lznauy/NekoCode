package a2aapi

import (
	"context"
	"errors"
	"iter"
	"strings"
	"sync"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/a2aproject/a2a-go/v2/a2asrv/taskstore"

	controlruntime "nekocode/runtime"
)

// Runtime is the transport-neutral subset used by the A2A adapter.
type Runtime interface {
	StartRunInSession(context.Context, string, controlruntime.Input) (controlruntime.RunID, string, error)
	CancelRun(context.Context, controlruntime.RunID) error
	DecideApproval(context.Context, string, controlruntime.ApprovalDecision) error
	AnswerQuestion(context.Context, string, controlruntime.QuestionReply) error
	ReplayEvents(context.Context, controlruntime.EventFilter) (<-chan controlruntime.Event, error)
	WaitRun(context.Context, controlruntime.RunID) error
	DeleteInactiveSession(string) error
	CurrentSessionID() string
}

type pendingKind string

const (
	pendingApproval      pendingKind = "approval"
	pendingQuestion      pendingKind = "question"
	maxContexts                      = 64
	maxMessagePartBytes              = 1 << 20
	maxProtocolIDBytes               = 256
	maxContextTurns                  = 128
	maxContextInputBytes             = 1 << 20
)

type pendingInteraction struct {
	kind      pendingKind
	id        string
	questions int
	message   *a2a.Message
}

type taskBinding struct {
	mu               sync.Mutex
	monitorCancel    context.CancelFunc
	monitorDone      chan struct{}
	foreground       int
	monitorPauses    int
	runID            controlruntime.RunID
	after            uint64
	artifactID       a2a.ArtifactID
	artifactText     strings.Builder
	confirmedText    string
	pending          []*pendingInteraction
	terminal         bool
	contextID        string
	contextHeld      bool
	evictedSessionID string
}

type contextBinding struct {
	sessionID  string
	lastUsed   uint64
	active     int
	turns      int
	inputBytes int
}

type contextReservation struct {
	contextID        string
	sessionID        string
	inputBytes       int
	previousLastUsed uint64
	reused           bool
	evictedID        string
	evicted          *contextBinding
}

type executor struct {
	rt    Runtime
	store taskstore.Store

	startMu               sync.Mutex
	mu                    sync.Mutex
	tasks                 map[a2a.TaskID]*taskBinding
	contexts              map[string]*contextBinding
	pendingSessionDeletes map[string]struct{}
	contextClock          uint64
}

func newExecutor(rt Runtime) *executor {
	return &executor{
		rt:                    rt,
		tasks:                 make(map[a2a.TaskID]*taskBinding),
		contexts:              make(map[string]*contextBinding),
		pendingSessionDeletes: make(map[string]struct{}),
	}
}

func (e *executor) Execute(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		if err := validateProtocolIDs(execCtx); err != nil {
			yield(nil, err)
			return
		}
		text, err := messageText(execCtx.Message)
		if err != nil {
			yield(nil, err)
			return
		}

		binding := e.binding(execCtx.TaskID)
		binding.mu.Lock()
		if handoff, _ := ctx.Value(monitorHandoffKey{}).(*monitorHandoff); handoff == nil {
			binding.foreground++
		}
		binding.mu.Unlock()
		e.stopMonitor(binding)
		if execCtx.StoredTask != nil && e.store != nil {
			stored, err := e.store.Get(ctx, execCtx.TaskID)
			if err != nil {
				yield(nil, err)
				return
			}
			if stored.Task.Status.State.Terminal() {
				yield(a2a.NewStatusUpdateEvent(execCtx, stored.Task.Status.State, stored.Task.Status.Message), nil)
				e.deleteBinding(execCtx.TaskID, binding)
				return
			}
		}
		if execCtx.StoredTask == nil && text == "" {
			e.deleteBinding(execCtx.TaskID, binding)
			yield(nil, a2a.NewError(a2a.ErrInvalidParams, "a new task requires text input"))
			return
		}
		if execCtx.StoredTask == nil {
			if !yield(a2a.NewSubmittedTask(execCtx, execCtx.Message), nil) {
				e.deleteBinding(execCtx.TaskID, binding)
				return
			}
		}

		binding.mu.Lock()
		pending := binding.firstInteraction()
		runID := binding.runID
		binding.mu.Unlock()
		if pending != nil {
			if err := e.resolvePending(ctx, pending, execCtx.Message, text); err != nil {
				yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateInputRequired, pendingReplyMessage(execCtx, pending, err.Error())), nil)
				return
			}
			binding.mu.Lock()
			binding.removeInteraction(pending.id)
			next := binding.firstInteraction()
			binding.mu.Unlock()
			if next != nil {
				if !yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateInputRequired, next.message), nil) {
					e.abandonRun(execCtx.TaskID, binding)
				}
				return
			}
			if !yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateWorking, nil), nil) {
				e.abandonRun(execCtx.TaskID, binding)
				return
			}
			e.streamRun(ctx, execCtx, binding, yield)
			return
		}

		if runID != "" {
			if !yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateWorking, nil), nil) {
				e.abandonRun(execCtx.TaskID, binding)
				return
			}
			e.streamRun(ctx, execCtx, binding, yield)
			return
		}
		if execCtx.StoredTask != nil {
			e.deleteBinding(execCtx.TaskID, binding)
			yield(nil, a2a.NewError(a2a.ErrTaskNotFound, "NekoCode run for task was not found"))
			return
		}

		e.startMu.Lock()
		e.retrySessionDeletes()
		reservation, err := e.reserveContext(execCtx.ContextID, e.rt.CurrentSessionID(), len(text))
		if err != nil {
			e.startMu.Unlock()
			yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateFailed, statusMessage(execCtx, err.Error(), nil)), nil)
			e.deleteBinding(execCtx.TaskID, binding)
			return
		}
		binding.mu.Lock()
		binding.contextID, binding.contextHeld = execCtx.ContextID, true
		binding.mu.Unlock()
		// The HTTP request can be shorter-lived than the agent run:
		// returnImmediately ends the client response while the SDK execution
		// continues, and INPUT_REQUIRED ends one SDK execution while the Runtime
		// waits for a follow-up. Only CancelTask or Runtime.Close should cancel it.
		runID, sessionID, err := e.rt.StartRunInSession(context.WithoutCancel(ctx), reservation.sessionID, controlruntime.Input{
			Source: controlruntime.SourceRef{Kind: "a2a", ID: string(execCtx.TaskID)},
			Sender: controlruntime.SenderRef{ID: execCtx.Message.ID, Display: senderName(execCtx)},
			Text:   text,
		})
		if err != nil {
			e.rollbackContextReservation(reservation)
			binding.mu.Lock()
			binding.contextHeld = false
			binding.mu.Unlock()
			e.startMu.Unlock()
			if reservation.sessionID == "" && sessionID != "" {
				e.deleteOrQueueSession(sessionID)
			}
			yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateFailed, statusMessage(execCtx, err.Error(), nil)), nil)
			e.deleteBinding(execCtx.TaskID, binding)
			return
		}
		e.rememberSession(execCtx.ContextID, sessionID)
		binding.mu.Lock()
		binding.runID = runID
		if reservation.evicted != nil {
			binding.evictedSessionID = reservation.evicted.sessionID
		}
		binding.mu.Unlock()
		e.startMu.Unlock()
		if !yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateWorking, nil), nil) {
			e.abandonRun(execCtx.TaskID, binding)
			return
		}
		e.streamRun(ctx, execCtx, binding, yield)
	}
}

func (e *executor) Cancel(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		binding := e.lookup(execCtx.TaskID)
		if binding == nil {
			yield(nil, a2a.NewError(a2a.ErrTaskNotFound, "NekoCode run for task was not found"))
			return
		}
		e.stopMonitor(binding)
		binding.mu.Lock()
		runID, terminal := binding.runID, binding.terminal
		binding.mu.Unlock()
		if runID == "" {
			yield(nil, a2a.NewError(a2a.ErrTaskNotFound, "NekoCode run for task was not found"))
			return
		}
		if terminal {
			yield(nil, a2a.NewError(a2a.ErrTaskNotCancelable, "NekoCode run has already finished"))
			return
		}
		if err := e.rt.CancelRun(ctx, runID); err != nil {
			yield(nil, err)
			return
		}
		binding.mu.Lock()
		binding.terminal = true
		binding.mu.Unlock()
		_ = e.rt.WaitRun(context.WithoutCancel(ctx), runID)
		yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateCanceled, nil), nil)
		e.deleteBinding(execCtx.TaskID, binding)
	}
}

func (e *executor) streamRun(ctx context.Context, execCtx *a2asrv.ExecutorContext, binding *taskBinding, yield func(a2a.Event, error) bool) {
	e.streamRunMode(ctx, execCtx, binding, yield, false)
}

func (e *executor) streamRunMode(ctx context.Context, execCtx *a2asrv.ExecutorContext, binding *taskBinding, yield func(a2a.Event, error) bool, monitoring bool) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	binding.mu.Lock()
	runID, after := binding.runID, binding.after
	binding.mu.Unlock()
	events, err := e.rt.ReplayEvents(ctx, controlruntime.EventFilter{
		Reliable: true,
		RunID:    runID,
		After:    after,
	})
	if err != nil {
		if monitoring && ctx.Err() != nil {
			return
		}
		yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateFailed, statusMessage(execCtx, err.Error(), nil)), nil)
		e.abandonRun(execCtx.TaskID, binding)
		return
	}
	for {
		var event controlruntime.Event
		var ok bool
		select {
		case <-ctx.Done():
			if !monitoring {
				e.abandonRun(execCtx.TaskID, binding)
			}
			return
		case event, ok = <-events:
			if !ok {
				if monitoring && ctx.Err() != nil {
					return
				}
				if ctx.Err() == nil {
					yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateFailed, statusMessage(execCtx, "NekoCode event stream closed unexpectedly", nil)), nil)
				}
				e.abandonRun(execCtx.TaskID, binding)
				return
			}
		}
		binding.mu.Lock()
		if event.Sequence > binding.after {
			binding.after = event.Sequence
		}
		binding.mu.Unlock()
		switch event.Type {
		case controlruntime.EventAssistantReset:
			binding.mu.Lock()
			confirmed := binding.confirmedText
			binding.mu.Unlock()
			if reset := e.replaceArtifact(execCtx, binding, confirmed); reset != nil && !yield(reset, nil) {
				e.abandonRun(execCtx.TaskID, binding)
				return
			}
		case controlruntime.EventAssistantDelta:
			delta := deltaText(event.Payload)
			if delta == "" {
				continue
			}
			artifact := e.artifactEvent(execCtx, binding, delta)
			if !yield(artifact, nil) {
				e.abandonRun(execCtx.TaskID, binding)
				return
			}
		case controlruntime.EventSystemMessage:
			text := messageContent(event.Payload)
			if text != "" {
				// System output must not suppress a later assistant message or
				// final run result when no assistant deltas were emitted.
				artifact := a2a.NewArtifactEvent(execCtx, a2a.NewTextPart(text))
				artifact.Artifact.Name = "system"
				if !yield(artifact, nil) {
					e.abandonRun(execCtx.TaskID, binding)
					return
				}
			}
		case controlruntime.EventApprovalResolved, controlruntime.EventQuestionResolved:
			var id string
			switch view := event.Payload.(type) {
			case controlruntime.ApprovalView:
				id = view.ID
			case controlruntime.QuestionView:
				id = view.ID
			}
			binding.mu.Lock()
			changed := binding.removeInteraction(id)
			next := binding.firstInteraction()
			binding.mu.Unlock()
			if !changed {
				continue
			}
			state := a2a.TaskStateWorking
			var message *a2a.Message
			if next != nil {
				state, message = a2a.TaskStateInputRequired, next.message
			}
			if !yield(a2a.NewStatusUpdateEvent(execCtx, state, message), nil) {
				e.abandonRun(execCtx.TaskID, binding)
				return
			}
			if next != nil && !monitoring {
				return
			}
		case controlruntime.EventAssistantMessage:
			text := messageContent(event.Payload)
			binding.mu.Lock()
			binding.confirmedText = text
			binding.mu.Unlock()
			if update := e.replaceArtifact(execCtx, binding, text); update != nil && !yield(update, nil) {
				e.abandonRun(execCtx.TaskID, binding)
				return
			}
		case controlruntime.EventApprovalRequested, controlruntime.EventQuestionRequested:
			pending := interactionRequest(execCtx, event)
			if pending == nil {
				continue
			}
			binding.mu.Lock()
			first := len(binding.pending) == 0
			binding.pending = append(binding.pending, pending)
			binding.mu.Unlock()
			// A2A exposes one actionable prompt per task. The monitor keeps
			// later requests until the visible one resolves on any transport.
			if !first {
				continue
			}
			if !yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateInputRequired, pending.message), nil) {
				e.abandonRun(execCtx.TaskID, binding)
				return
			}
			if !monitoring {
				return
			}
		case controlruntime.EventRunDone:
			if !e.emitTerminalArtifact(execCtx, binding, event.Payload, yield) {
				e.abandonRun(execCtx.TaskID, binding)
				return
			}
			e.finishTask(ctx, execCtx, binding, event.Sequence, a2a.TaskStateCompleted, "", yield)
			return
		case controlruntime.EventRunFailed:
			e.finishTask(ctx, execCtx, binding, event.Sequence, a2a.TaskStateFailed, runError(event.Payload), yield)
			return
		case controlruntime.EventRunCancelled:
			e.finishTask(ctx, execCtx, binding, event.Sequence, a2a.TaskStateCanceled, "", yield)
			return
		}
	}
}

func (e *executor) abandonRun(taskID a2a.TaskID, binding *taskBinding) {
	binding.mu.Lock()
	if binding.terminal || binding.runID == "" {
		binding.mu.Unlock()
		return
	}
	binding.terminal = true
	runID := binding.runID
	binding.mu.Unlock()
	go func() {
		_ = e.rt.CancelRun(context.Background(), runID)
		_ = e.rt.WaitRun(context.Background(), runID)
		e.deleteBinding(taskID, binding)
	}()
}

func (e *executor) artifactEvent(execCtx *a2asrv.ExecutorContext, binding *taskBinding, text string) *a2a.TaskArtifactUpdateEvent {
	binding.mu.Lock()
	defer binding.mu.Unlock()
	binding.artifactText.WriteString(text)
	if binding.artifactID == "" {
		event := a2a.NewArtifactEvent(execCtx, a2a.NewTextPart(text))
		binding.artifactID = event.Artifact.ID
		event.Artifact.Name = "result"
		return event
	}
	return a2a.NewArtifactUpdateEvent(execCtx, binding.artifactID, a2a.NewTextPart(text))
}

func (e *executor) replaceArtifact(execCtx *a2asrv.ExecutorContext, binding *taskBinding, text string) *a2a.TaskArtifactUpdateEvent {
	binding.mu.Lock()
	defer binding.mu.Unlock()
	if binding.artifactText.String() == text {
		return nil
	}
	binding.artifactText.Reset()
	binding.artifactText.WriteString(text)
	if binding.artifactID == "" {
		event := a2a.NewArtifactEvent(execCtx, a2a.NewTextPart(text))
		event.Artifact.Name = "result"
		binding.artifactID = event.Artifact.ID
		return event
	}
	event := a2a.NewArtifactUpdateEvent(execCtx, binding.artifactID, a2a.NewTextPart(text))
	event.Append = false
	event.Artifact.Name = "result"
	return event
}

func (e *executor) emitTerminalArtifact(execCtx *a2asrv.ExecutorContext, binding *taskBinding, payload any, yield func(a2a.Event, error) bool) bool {
	if result, ok := payload.(controlruntime.RunResult); ok {
		if event := e.replaceArtifact(execCtx, binding, result.Output); event != nil {
			return yield(event, nil)
		}
	}
	return true
}

// Finalize only after cleanup. Its system messages are emitted after RunDone,
// so read the bounded history behind the WaitRun barrier before publishing the
// A2A terminal status. Custom runtimes may omit the optional history reader.
func (e *executor) finishTask(ctx context.Context, execCtx *a2asrv.ExecutorContext, binding *taskBinding, after uint64, state a2a.TaskState, detail string, yield func(a2a.Event, error) bool) {
	binding.mu.Lock()
	runID := binding.runID
	binding.mu.Unlock()
	if err := e.rt.WaitRun(context.WithoutCancel(ctx), runID); err != nil {
		state = a2a.TaskStateFailed
		detail = err.Error()
	}
	binding.mu.Lock()
	binding.terminal = true
	binding.mu.Unlock()
	defer e.deleteBinding(execCtx.TaskID, binding)
	if history, ok := e.rt.(interface {
		EventHistory(controlruntime.EventFilter) []controlruntime.Event
	}); ok {
		for _, event := range history.EventHistory(controlruntime.EventFilter{RunID: runID, After: after, Types: []controlruntime.EventType{controlruntime.EventSystemMessage}}) {
			text := messageContent(event.Payload)
			if text == "" {
				continue
			}
			artifact := a2a.NewArtifactEvent(execCtx, a2a.NewTextPart(text))
			artifact.Artifact.Name = "system"
			if !yield(artifact, nil) {
				return
			}
			if detail != "" {
				detail += "\n"
			}
			detail += text
		}
	}
	var message *a2a.Message
	if detail != "" {
		message = statusMessage(execCtx, detail, nil)
	}
	yield(a2a.NewStatusUpdateEvent(execCtx, state, message), nil)
}

func (e *executor) resolvePending(ctx context.Context, pending *pendingInteraction, message *a2a.Message, text string) error {
	if err := validateInteractionReply(message, pending.id); err != nil {
		return err
	}
	switch pending.kind {
	case pendingApproval:
		allowed, ok := approvalDecision(message, text)
		if !ok {
			return errors.New("reply with allow or deny")
		}
		return e.rt.DecideApproval(ctx, pending.id, controlruntime.ApprovalDecision{Allowed: allowed})
	case pendingQuestion:
		reply := questionReply(message, text, pending.questions)
		return e.rt.AnswerQuestion(ctx, pending.id, reply)
	default:
		return errors.New("unknown pending interaction")
	}
}

func (e *executor) binding(taskID a2a.TaskID) *taskBinding {
	e.mu.Lock()
	defer e.mu.Unlock()
	binding := e.tasks[taskID]
	if binding == nil {
		binding = &taskBinding{}
		e.tasks[taskID] = binding
	}
	return binding
}

func (e *executor) lookup(taskID a2a.TaskID) *taskBinding {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.tasks[taskID]
}

var _ a2asrv.AgentExecutor = (*executor)(nil)
