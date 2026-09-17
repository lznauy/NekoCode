package runtime

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"nekocode/protocol"
	textutil "nekocode/util/text"
)

type runLease struct {
	mu          sync.Mutex
	drained     chan struct{}
	drainOnce   sync.Once
	interaction sync.Mutex
	active      bool
	inFlight    int
}

// runExecution tracks runner completion separately from callback drainage.
// Cancellation must wait for both: the runner owns the final usage snapshot,
// while the lease owns event callbacks that may still be in flight.
type runExecution struct {
	mu          sync.RWMutex
	settled     chan struct{}
	settleOnce  sync.Once
	withMetrics bool
}

func newRunExecution() *runExecution {
	return &runExecution{settled: make(chan struct{})}
}

func (e *runExecution) settle(withMetrics bool) {
	if e == nil {
		return
	}
	e.mu.Lock()
	e.withMetrics = withMetrics
	e.mu.Unlock()
	e.settleOnce.Do(func() { close(e.settled) })
}

func (e *runExecution) wait() {
	if e != nil {
		<-e.settled
	}
}

func (e *runExecution) hasMetrics() bool {
	if e == nil {
		return false
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.withMetrics
}

func newRunLease() *runLease {
	return &runLease{active: true, drained: make(chan struct{})}
}

func (l *runLease) emit(fn func()) {
	l.guard(fn)
}

func (l *runLease) guard(fn func()) bool {
	if l == nil {
		return false
	}
	l.mu.Lock()
	if !l.active {
		l.mu.Unlock()
		return false
	}
	l.inFlight++
	l.mu.Unlock()

	defer func() {
		l.mu.Lock()
		l.inFlight--
		l.signalDrainedLocked()
		l.mu.Unlock()
	}()
	fn()
	return true
}

func (l *runLease) deactivate() {
	if l == nil {
		return
	}
	l.mu.Lock()
	l.active = false
	l.signalDrainedLocked()
	l.mu.Unlock()
}

func (l *runLease) wait(ctx context.Context) error {
	if l == nil {
		return nil
	}
	select {
	case <-l.drained:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (l *runLease) signalDrainedLocked() {
	if !l.active && l.inFlight == 0 {
		l.drainOnce.Do(func() { close(l.drained) })
	}
}

type runHost struct {
	runtime *Runtime
	runID   RunID
	lease   *runLease
}

func (h runHost) Text(delta string) {
	h.lease.emit(func() {
		h.runtime.events.Publish(Event{
			RunID: h.runID, Type: EventAssistantDelta, Source: SourceRef{Kind: "bot"},
			Payload: DeltaPayload{Delta: delta},
		})
	})
}

func (h runHost) Reason(delta string) {
	h.lease.emit(func() {
		h.runtime.events.Publish(Event{
			RunID: h.runID, Type: EventReasoningDelta, Source: SourceRef{Kind: "bot"},
			Payload: DeltaPayload{Delta: delta},
		})
	})
}

func (h runHost) Step(event protocol.StepEvent) {
	h.lease.emit(func() { h.runtime.publishStep(h.runID, event) })
}

func (h runHost) Phase(phase string) {
	h.lease.emit(func() {
		h.runtime.events.Publish(Event{
			RunID: h.runID, Type: EventPhaseChanged, Source: SourceRef{Kind: "bot"},
			Payload: PhasePayload{Phase: phase},
		})
	})
}

func (h runHost) Todos(items []TodoItem) {
	h.lease.emit(func() {
		h.runtime.events.Publish(Event{
			RunID: h.runID, Type: EventTodosUpdated, Source: SourceRef{Kind: "bot"},
			Payload: items,
		})
	})
}

func (h runHost) system(message string, source SourceRef) {
	if strings.TrimSpace(message) == "" {
		return
	}
	h.lease.emit(func() {
		h.runtime.events.Publish(Event{
			RunID: h.runID, Type: EventSystemMessage, Source: source,
			Payload: MessagePayload{Role: "system", Content: message, Source: source},
		})
	})
}

func (h runHost) Confirm(request ConfirmRequest) ConfirmReply {
	h.lease.interaction.Lock()
	defer h.lease.interaction.Unlock()

	var wait func() ConfirmReply
	var waiting bool
	ok := h.lease.guard(func() {
		wait = h.runtime.approvals.Register(request)
		if wait != nil {
			waiting = h.runtime.beginWaiting(h.runID, RunWaitingApproval)
		}
	})
	if !ok || wait == nil {
		return ConfirmReply{Allowed: false}
	}
	reply := wait()
	if waiting {
		h.runtime.resumeRun(h.runID, RunWaitingApproval)
	}
	return reply
}

func (h runHost) Ask(request QuestionRequest) QuestionReply {
	h.lease.interaction.Lock()
	defer h.lease.interaction.Unlock()

	var wait func() QuestionReply
	var waiting bool
	ok := h.lease.guard(func() {
		wait = h.runtime.questions.Register(request)
		if wait != nil {
			waiting = h.runtime.beginWaiting(h.runID, RunWaitingQuestion)
		}
	})
	if !ok || wait == nil {
		return QuestionReply{Rejected: true}
	}
	reply := wait()
	if waiting {
		h.runtime.resumeRun(h.runID, RunWaitingQuestion)
	}
	return reply
}

func (r *Runtime) run(ctx context.Context, runID RunID, input Input, lease *runLease, execution *runExecution) {
	stopMetricsUpdates := func() {}
	withMetrics := false
	defer func() {
		stopMetricsUpdates()
		if recovered := recover(); recovered != nil {
			r.finishRun(runID, "", fmt.Errorf("runtime: run %s panicked: %v", runID, recovered), withMetrics)
		}
		execution.settle(withMetrics)
		r.endRun(runID)
	}()

	host := runHost{runtime: r, runID: runID, lease: lease}
	if !lease.guard(func() {
		r.events.Publish(Event{RunID: runID, Type: EventRunStarted, Source: SourceRef{Kind: "runtime"}})
	}) {
		return
	}
	if r.handleRuntimeCommand(ctx, runID, input, host) {
		return
	}

	agentInput, proceed := r.handleBotCommand(ctx, runID, input.Text, host)
	if !proceed {
		return
	}
	if !lease.guard(func() { r.publishAcceptedInput(runID, input) }) {
		return
	}

	withMetrics = true
	stopMetricsUpdates = r.startMetricsUpdates(runID, lease)
	result, err := r.runner.Run(ctx, agentInput, host)
	r.finishRun(runID, result, err, true)
}

// publishAcceptedInput records only inputs that continue to the agent. Runtime
// and bot commands are control-plane operations: their system response is
// visible, but the command text is not projected as a conversation message.
func (r *Runtime) publishAcceptedInput(runID RunID, input Input) {
	r.events.Publish(Event{
		RunID: runID, Type: EventInputAccepted, Source: input.Source,
		Payload: MessagePayload{
			Role: "user", Content: RedactInputText(input.Text),
			Source: input.Source, Sender: input.Sender,
		},
	})
}

func (r *Runtime) handleBotCommand(ctx context.Context, runID RunID, input string, host runHost) (string, bool) {
	if r.services.ExecuteCommand == nil {
		return input, true
	}
	sessionID := ""
	if r.services.CurrentSessionID != nil {
		sessionID = r.services.CurrentSessionID()
	}
	result, err := r.services.ExecuteCommand(ctx, input, host)
	if err != nil {
		r.finishRun(runID, "", err, false)
		return "", false
	}
	if result.Output != "" {
		host.system(result.Output, SourceRef{Kind: "bot"})
	}
	if r.services.CurrentSessionID != nil {
		nextSessionID := r.services.CurrentSessionID()
		// Only announce a real switch to another non-empty session (e.g.
		// /sessions <id>). Commands like /model leave the session empty via
		// saveSession's no-message cleanup; announcing that would wipe the
		// command's own system output from the UI.
		if nextSessionID != "" && nextSessionID != sessionID {
			host.lease.emit(func() {
				r.events.Publish(Event{
					RunID: runID, Type: EventSessionChanged, Source: SourceRef{Kind: "bot"},
					Payload: SessionPayload{ID: nextSessionID},
				})
			})
		}
	}
	switch result.Action {
	case CommandIgnored:
		return input, true
	case CommandContinue:
		if strings.TrimSpace(result.AgentInput) != "" {
			return result.AgentInput, true
		}
		return input, true
	default:
		r.finishRun(runID, "", nil, false)
		return "", false
	}
}

func (r *Runtime) handleRuntimeCommand(ctx context.Context, runID RunID, input Input, host runHost) bool {
	fields := strings.Fields(strings.TrimSpace(input.Text))
	if len(fields) == 0 {
		return false
	}
	head := strings.ToLower(fields[0])
	if !strings.HasPrefix(head, "/") {
		return false
	}
	r.mu.Lock()
	command, ok := r.runtimeCommands[strings.TrimPrefix(head, "/")]
	r.mu.Unlock()
	if !ok {
		return false
	}
	response, err := command.handle(ctx, fields[1:])
	if err != nil {
		response = err.Error()
	}
	host.system(response, SourceRef{Kind: "runtime"})
	r.finishRun(runID, "", nil, false)
	return true
}

func (r *Runtime) finishRun(runID RunID, output string, err error, withMetrics bool) {
	payload := RunResult{Output: output}
	eventType := EventRunDone
	status := RunDone
	if err != nil {
		eventType = EventRunFailed
		status = RunFailed
		payload.Error = err.Error()
	}

	r.mu.Lock()
	if r.closed || r.currentRun != runID {
		r.mu.Unlock()
		return
	}
	if _, cancelled := r.cancelled[runID]; cancelled {
		r.mu.Unlock()
		return
	}
	switch r.status {
	case RunRunning, RunWaitingApproval, RunWaitingQuestion:
	default:
		r.mu.Unlock()
		return
	}
	r.status = status
	cancel := r.cancelRun
	r.cancelRun = nil
	r.runContext = nil
	lease := r.runLease
	r.runLease = nil
	r.mu.Unlock()

	lease.deactivate()
	if cancel != nil {
		cancel()
	}
	_ = lease.wait(context.Background())
	r.approvals.RejectAll()
	r.questions.RejectAll()
	if withMetrics {
		r.publishMetrics(runID)
	}
	r.events.Publish(Event{RunID: runID, Type: eventType, Source: SourceRef{Kind: "bot"}, Payload: payload})

}

func (r *Runtime) endRun(runID RunID) {
	r.mu.Lock()
	if r.currentRun != runID {
		r.mu.Unlock()
		return
	}
	status := r.status
	cancelDone := r.cancelDone
	r.mu.Unlock()

	if status == RunCancelled && cancelDone != nil {
		<-cancelDone
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.currentRun != runID {
		return
	}
	if r.status == RunDone || r.status == RunFailed || r.status == RunCancelled {
		r.status = RunIdle
		r.runContext = nil
		r.runExecution = nil
	}
	delete(r.cancelled, runID)
	if r.runDone != nil {
		close(r.runDone)
		r.runDone = nil
	}
}

func (r *Runtime) publishStep(runID RunID, step protocol.StepEvent) {
	if step.Action == protocol.StepActionCompaction && step.Compaction != nil {
		r.events.Publish(Event{RunID: runID, Type: EventCompaction, Source: SourceRef{Kind: "bot"}, Payload: *step.Compaction})
		return
	}
	var eventType EventType
	payload := ToolPayload{
		ToolName: step.ToolName, CallID: step.CallID, Args: step.ToolArgs,
		Input:   append([]byte(nil), step.ToolInput...),
		IsError: step.IsError, SubAgentID: step.SubAgentID, SubAgentColor: step.SubAgentColor,
	}
	switch step.Action {
	case protocol.StepActionRunSummary:
		if step.Summary != nil {
			r.events.Publish(Event{RunID: runID, Type: EventRunSummary, Source: SourceRef{Kind: "bot"}, Payload: *step.Summary})
		}
		return
	case protocol.StepActionSubAgentText, protocol.StepActionSubAgentReason, protocol.StepActionSubAgentMessage:
		kind := "text_delta"
		if step.Action == protocol.StepActionSubAgentReason {
			kind = "reasoning_delta"
		}
		if step.Action == protocol.StepActionSubAgentMessage {
			kind = "message"
		}
		r.events.Publish(Event{RunID: runID, Type: EventSubAgentOutput, Source: SourceRef{Kind: "bot"}, Payload: SubAgentOutput{ID: step.SubAgentID, Kind: kind, Text: step.Output}})
		return

	case protocol.StepActionChat, protocol.StepActionThink:
		r.events.Publish(Event{
			RunID: runID, Type: EventAssistantMessage, Source: SourceRef{Kind: "bot"},
			Payload: MessagePayload{Role: "assistant", Content: step.Output},
		})
		return
	case protocol.StepActionToolStart:
		eventType = EventToolStarted
		payload.Preview = textutil.NormalizeTerminalOutput(step.Output)
	case protocol.StepActionToolBlocked:
		eventType = EventToolBlocked
		payload.Output = textutil.NormalizeTerminalOutput(step.Output)
	case protocol.StepActionToolPreview:
		eventType = EventToolPreview
		payload.Preview = textutil.NormalizeTerminalOutput(step.Output)
	case protocol.StepActionExecuteTool:
		eventType = EventToolCompleted
		payload.Output = textutil.NormalizeTerminalOutput(step.Output)
	case protocol.StepActionSubAgentStart:
		r.events.Publish(Event{
			RunID: runID, Type: EventSubAgentStarted, Source: SourceRef{Kind: "bot"},
			Payload: SubAgentPayload{
				ID: step.SubAgentID, Type: step.SubAgentType, Profile: step.SubAgentProfile,
				Skills: append([]string(nil), step.SubAgentSkills...), Color: step.SubAgentColor,
			},
		})
		r.publishMetrics(runID)
		return
	case protocol.StepActionSubAgentEnd:
		r.events.Publish(Event{
			RunID: runID, Type: EventSubAgentEnded, Source: SourceRef{Kind: "bot"},
			Payload: SubAgentPayload{ID: step.SubAgentID},
		})
		r.publishMetrics(runID)
		return
	default:
		return
	}
	r.events.Publish(Event{RunID: runID, Type: eventType, Source: SourceRef{Kind: "bot"}, Payload: payload})
	r.publishMetrics(runID)
}
