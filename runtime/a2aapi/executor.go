package a2aapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"strings"
	"sync"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"

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
}

type taskBinding struct {
	mu               sync.Mutex
	runID            controlruntime.RunID
	after            uint64
	artifactID       a2a.ArtifactID
	pending          *pendingInteraction
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
	rt Runtime

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
		pending := binding.pending
		runID := binding.runID
		binding.mu.Unlock()
		if pending != nil {
			if err := e.resolvePending(ctx, pending, execCtx.Message, text); err != nil {
				yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateInputRequired, statusMessage(execCtx, err.Error(), nil)), nil)
				return
			}
			binding.mu.Lock()
			binding.pending = nil
			binding.mu.Unlock()
			if !yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateWorking, nil), nil) {
				e.abandonRun(execCtx.TaskID, binding)
				return
			}
			e.streamRun(ctx, execCtx, binding, yield)
			return
		}

		if runID != "" {
			yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateFailed, statusMessage(execCtx, "task execution is no longer resumable", nil)), nil)
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
	binding.mu.Lock()
	runID, after := binding.runID, binding.after
	binding.mu.Unlock()
	events, err := e.rt.ReplayEvents(ctx, controlruntime.EventFilter{
		Reliable: true,
		RunID:    runID,
		After:    after,
	})
	if err != nil {
		yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateFailed, statusMessage(execCtx, err.Error(), nil)), nil)
		e.abandonRun(execCtx.TaskID, binding)
		return
	}
	for event := range events {
		binding.mu.Lock()
		if event.Sequence > binding.after {
			binding.after = event.Sequence
		}
		binding.mu.Unlock()
		switch event.Type {
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
		case controlruntime.EventAssistantMessage:
			binding.mu.Lock()
			hasArtifact := binding.artifactID != ""
			binding.mu.Unlock()
			if hasArtifact {
				continue
			}
			text := messageContent(event.Payload)
			if text != "" {
				if !yield(e.artifactEvent(execCtx, binding, text), nil) {
					e.abandonRun(execCtx.TaskID, binding)
					return
				}
			}
		case controlruntime.EventApprovalRequested:
			view, ok := event.Payload.(controlruntime.ApprovalView)
			if !ok {
				continue
			}
			binding.mu.Lock()
			binding.pending = &pendingInteraction{kind: pendingApproval, id: view.ID}
			binding.mu.Unlock()
			data := map[string]any{"type": "approval", "interactionId": view.ID, "toolName": view.ToolName, "args": view.Args}
			msg := statusMessage(execCtx, fmt.Sprintf("Approval required for tool %q. Reply with allow or deny.", view.ToolName), data)
			if !yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateInputRequired, msg), nil) {
				e.abandonRun(execCtx.TaskID, binding)
			}
			return
		case controlruntime.EventQuestionRequested:
			view, ok := event.Payload.(controlruntime.QuestionView)
			if !ok {
				continue
			}
			binding.mu.Lock()
			binding.pending = &pendingInteraction{kind: pendingQuestion, id: view.ID, questions: len(view.Questions)}
			binding.mu.Unlock()
			data := map[string]any{"type": "question", "interactionId": view.ID, "questions": view.Questions}
			msg := statusMessage(execCtx, formatQuestions(view), data)
			if !yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateInputRequired, msg), nil) {
				e.abandonRun(execCtx.TaskID, binding)
			}
			return
		case controlruntime.EventRunDone:
			e.emitTerminalArtifact(execCtx, binding, event.Payload, yield)
			_ = e.rt.WaitRun(ctx, runID)
			binding.mu.Lock()
			binding.terminal = true
			binding.mu.Unlock()
			yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateCompleted, nil), nil)
			e.deleteBinding(execCtx.TaskID, binding)
			return
		case controlruntime.EventRunFailed:
			_ = e.rt.WaitRun(ctx, runID)
			binding.mu.Lock()
			binding.terminal = true
			binding.mu.Unlock()
			yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateFailed, statusMessage(execCtx, runError(event.Payload), nil)), nil)
			e.deleteBinding(execCtx.TaskID, binding)
			return
		case controlruntime.EventRunCancelled:
			_ = e.rt.WaitRun(ctx, runID)
			binding.mu.Lock()
			binding.terminal = true
			binding.mu.Unlock()
			yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateCanceled, nil), nil)
			e.deleteBinding(execCtx.TaskID, binding)
			return
		}
	}
	if ctx.Err() == nil {
		yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateFailed, statusMessage(execCtx, "NekoCode event stream closed unexpectedly", nil)), nil)
	}
	e.abandonRun(execCtx.TaskID, binding)
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
	if binding.artifactID == "" {
		event := a2a.NewArtifactEvent(execCtx, a2a.NewTextPart(text))
		binding.artifactID = event.Artifact.ID
		event.Artifact.Name = "result"
		return event
	}
	return a2a.NewArtifactUpdateEvent(execCtx, binding.artifactID, a2a.NewTextPart(text))
}

func (e *executor) emitTerminalArtifact(execCtx *a2asrv.ExecutorContext, binding *taskBinding, payload any, yield func(a2a.Event, error) bool) {
	binding.mu.Lock()
	hasArtifact := binding.artifactID != ""
	binding.mu.Unlock()
	if hasArtifact {
		return
	}
	result, ok := payload.(controlruntime.RunResult)
	if ok && strings.TrimSpace(result.Output) != "" {
		yield(e.artifactEvent(execCtx, binding, result.Output), nil)
	}
}

func (e *executor) reserveContext(contextID, protectedSessionID string, inputBytes int) (*contextReservation, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.contextClock++
	reservation := &contextReservation{contextID: contextID, inputBytes: inputBytes}
	if existing := e.contexts[contextID]; existing != nil {
		if existing.turns < maxContextTurns && existing.inputBytes+inputBytes <= maxContextInputBytes {
			reservation.sessionID = existing.sessionID
			reservation.previousLastUsed = existing.lastUsed
			reservation.reused = true
			existing.active++
			existing.turns++
			existing.inputBytes += inputBytes
			existing.lastUsed = e.contextClock
			return reservation, nil
		}
		if existing.active != 0 || existing.sessionID == protectedSessionID {
			return nil, fmt.Errorf("A2A context retention limit reached")
		}
		if len(e.pendingSessionDeletes) >= maxContexts {
			return nil, fmt.Errorf("A2A session cleanup backlog limit reached")
		}
		reservation.evictedID, reservation.evicted = contextID, existing
		delete(e.contexts, contextID)
	} else if len(e.pendingSessionDeletes) >= maxContexts {
		return nil, fmt.Errorf("A2A session cleanup backlog limit reached")
	}
	if len(e.contexts) >= maxContexts {
		for id, candidate := range e.contexts {
			if candidate.active != 0 || candidate.sessionID == protectedSessionID || (reservation.evicted != nil && candidate.lastUsed >= reservation.evicted.lastUsed) {
				continue
			}
			reservation.evictedID, reservation.evicted = id, candidate
		}
		if reservation.evicted == nil {
			return nil, fmt.Errorf("A2A context limit reached")
		}
		delete(e.contexts, reservation.evictedID)
	}
	e.contexts[contextID] = &contextBinding{lastUsed: e.contextClock, active: 1, turns: 1, inputBytes: inputBytes}
	return reservation, nil
}

func (e *executor) rememberSession(contextID, sessionID string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if binding := e.contexts[contextID]; binding != nil && binding.sessionID == "" {
		binding.sessionID = sessionID
	}
}

func (e *executor) rollbackContextReservation(reservation *contextReservation) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if reservation.evicted != nil {
		delete(e.contexts, reservation.contextID)
		e.contexts[reservation.evictedID] = reservation.evicted
		return
	}
	context := e.contexts[reservation.contextID]
	if reservation.reused && context != nil {
		if context.active > 0 {
			context.active--
		}
		if context.turns > 0 {
			context.turns--
		}
		context.inputBytes = max(0, context.inputBytes-reservation.inputBytes)
		context.lastUsed = reservation.previousLastUsed
		return
	}
	delete(e.contexts, reservation.contextID)
}

func (e *executor) resolvePending(ctx context.Context, pending *pendingInteraction, message *a2a.Message, text string) error {
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

func (e *executor) deleteBinding(taskID a2a.TaskID, binding *taskBinding) {
	binding.mu.Lock()
	contextID, contextHeld := binding.contextID, binding.contextHeld
	evictedSessionID := binding.evictedSessionID
	binding.contextHeld = false
	binding.evictedSessionID = ""
	binding.mu.Unlock()
	e.mu.Lock()
	if e.tasks[taskID] == binding {
		delete(e.tasks, taskID)
	}
	if contextHeld {
		if contextBinding := e.contexts[contextID]; contextBinding != nil && contextBinding.active > 0 {
			contextBinding.active--
			if contextBinding.active == 0 && contextBinding.sessionID == "" {
				delete(e.contexts, contextID)
			}
		}
	}
	e.mu.Unlock()
	if evictedSessionID != "" {
		e.deleteOrQueueSession(evictedSessionID)
	}
}

func (e *executor) deleteOrQueueSession(sessionID string) {
	err := e.rt.DeleteInactiveSession(sessionID)
	e.mu.Lock()
	defer e.mu.Unlock()
	if err == nil {
		delete(e.pendingSessionDeletes, sessionID)
		return
	}
	if len(e.pendingSessionDeletes) < maxContexts {
		e.pendingSessionDeletes[sessionID] = struct{}{}
	}
}

func (e *executor) retrySessionDeletes() {
	e.mu.Lock()
	sessionIDs := make([]string, 0, len(e.pendingSessionDeletes))
	for sessionID := range e.pendingSessionDeletes {
		sessionIDs = append(sessionIDs, sessionID)
	}
	e.mu.Unlock()
	for _, sessionID := range sessionIDs {
		e.deleteOrQueueSession(sessionID)
	}
}

func messageText(message *a2a.Message) (string, error) {
	if message == nil || message.Role != a2a.MessageRoleUser {
		return "", a2a.NewError(a2a.ErrInvalidParams, "a user message is required")
	}
	var texts []string
	totalBytes := 0
	for _, part := range message.Parts {
		if part == nil {
			return "", a2a.NewError(a2a.ErrInvalidParams, "message contains an empty part")
		}
		if text := strings.TrimSpace(part.Text()); text != "" {
			totalBytes += len(text)
			if totalBytes > maxMessagePartBytes {
				return "", a2a.NewError(a2a.ErrInvalidParams, "message text exceeds size limit")
			}
			texts = append(texts, text)
			continue
		}
		if data := part.Data(); data != nil {
			encoded, err := json.Marshal(data)
			if err != nil || len(encoded) > maxMessagePartBytes {
				return "", a2a.NewError(a2a.ErrInvalidParams, "message data exceeds size limit")
			}
			continue
		}
		return "", a2a.NewError(a2a.ErrUnsupportedContentType, "NekoCode A2A currently accepts text/plain and structured interaction replies")
	}
	text := strings.TrimSpace(strings.Join(texts, "\n"))
	if text == "" && !hasDataPart(message) {
		return "", a2a.NewError(a2a.ErrInvalidParams, "message contains no text")
	}
	return text, nil
}

func validateProtocolIDs(execCtx *a2asrv.ExecutorContext) error {
	if execCtx == nil || execCtx.Message == nil {
		return a2a.NewError(a2a.ErrInvalidParams, "a message is required")
	}
	return validateMessageIDs(execCtx.TaskID, execCtx.ContextID, execCtx.Message.ID)
}

func validateIncomingMessage(message *a2a.Message) error {
	if message == nil {
		return a2a.NewError(a2a.ErrInvalidParams, "a message is required")
	}
	if err := validateMessageIDs(message.TaskID, message.ContextID, message.ID); err != nil {
		return err
	}
	_, err := messageText(message)
	return err
}

func validateMessageIDs(taskID a2a.TaskID, contextID, messageID string) error {
	if len(taskID) > maxProtocolIDBytes || len(contextID) > maxProtocolIDBytes || len(messageID) > maxProtocolIDBytes {
		return a2a.NewError(a2a.ErrInvalidParams, "task, context, or message ID exceeds size limit")
	}
	return nil
}

func hasDataPart(message *a2a.Message) bool {
	for _, part := range message.Parts {
		if part.Data() != nil {
			return true
		}
	}
	return false
}

func approvalDecision(message *a2a.Message, text string) (bool, bool) {
	var structured struct {
		Allowed *bool `json:"allowed"`
	}
	if decodeDataPart(message, &structured) && structured.Allowed != nil {
		return *structured.Allowed, true
	}
	switch strings.ToLower(strings.TrimSpace(text)) {
	case "allow", "approve", "yes", "y", "允许", "批准", "是":
		return true, true
	case "deny", "reject", "no", "n", "拒绝", "否":
		return false, true
	default:
		return false, false
	}
}

func questionReply(message *a2a.Message, text string, count int) controlruntime.QuestionReply {
	var structured controlruntime.QuestionReply
	if decodeDataPart(message, &structured) && (structured.Rejected || len(structured.Answers) > 0) {
		return structured
	}
	answers := make([][]string, count)
	if count > 0 {
		answers[0] = []string{text}
	}
	return controlruntime.QuestionReply{Answers: answers}
}

func decodeDataPart(message *a2a.Message, target any) bool {
	for _, part := range message.Parts {
		data := part.Data()
		if data == nil {
			continue
		}
		encoded, err := json.Marshal(data)
		if err == nil && json.Unmarshal(encoded, target) == nil {
			return true
		}
	}
	return false
}

func statusMessage(execCtx *a2asrv.ExecutorContext, text string, data map[string]any) *a2a.Message {
	parts := []*a2a.Part{a2a.NewTextPart(text)}
	if data != nil {
		parts = append(parts, a2a.NewDataPart(data))
	}
	return a2a.NewMessageForTask(a2a.MessageRoleAgent, execCtx, parts...)
}

func senderName(execCtx *a2asrv.ExecutorContext) string {
	if execCtx.User != nil && execCtx.User.Name != "" {
		return execCtx.User.Name
	}
	return "A2A client"
}

func deltaText(payload any) string {
	if delta, ok := payload.(controlruntime.DeltaPayload); ok {
		return delta.Delta
	}
	return ""
}

func messageContent(payload any) string {
	if message, ok := payload.(controlruntime.MessagePayload); ok {
		return message.Content
	}
	return ""
}

func runError(payload any) string {
	if result, ok := payload.(controlruntime.RunResult); ok && result.Error != "" {
		return result.Error
	}
	return "NekoCode run failed"
}

func formatQuestions(view controlruntime.QuestionView) string {
	var lines []string
	for _, question := range view.Questions {
		lines = append(lines, question.Question)
	}
	if len(lines) == 0 {
		return "Additional input is required."
	}
	return strings.Join(lines, "\n")
}

var _ a2asrv.AgentExecutor = (*executor)(nil)
