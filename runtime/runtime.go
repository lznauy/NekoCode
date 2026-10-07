// Package runtime provides the interaction control layer above a Runner.
//
// Runtime is the single in-process entry point used by TUI, GUI, HTTP and IM
// connectors. It owns run state, events, approvals, questions, connectors and
// read models; the runner only supplies bot execution and bot-owned views.
package runtime

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"nekocode/runtime/internal/broker"
	"nekocode/runtime/internal/connectors"
	"nekocode/runtime/internal/eventbus"
	"nekocode/runtime/internal/recording"
	"nekocode/runtime/internal/runstore"
)

type commandHandler func(ctx context.Context, args []string) (string, error)

type runtimeCommand struct {
	description string
	handle      commandHandler
}

// Interaction is the stable, transport-neutral contract shared by in-process
// UIs, HTTP adapters, and connectors. CommandMenu belongs here because command
// discovery and execution are two halves of the same interaction protocol.
type Interaction interface {
	StartRun(context.Context, Input) (RunID, error)
	CancelRun(context.Context, RunID) error
	DecideApproval(context.Context, string, ApprovalDecision) error
	AnswerQuestion(context.Context, string, QuestionReply) error
	CommandMenu(context.Context, string) (CommandMenu, bool)
	Events(context.Context, EventFilter) (<-chan Event, error)
}

// Runtime is the interaction kernel. Applications should keep this
// single instance and route every interaction surface through it.
type Runtime struct {
	runner          Runner
	services        Services
	events          *eventbus.EventBus
	approvals       *broker.ApprovalBroker
	questions       *broker.QuestionBroker
	connectors      *connectors.Manager
	runs            *runstore.RunStore
	recorder        *recording.EventRecorder
	runtimeCommands map[string]runtimeCommand

	mu                   sync.Mutex
	mutationMu           sync.Mutex
	recordingMu          sync.Mutex
	mutating             bool
	currentRun           RunID
	runContext           context.Context
	cancelRun            context.CancelFunc
	cancelDone           chan struct{}
	runDone              chan struct{}
	runLease             *runLease
	runExecution         *runExecution
	restoreSession       bool
	restoreSessionID     string
	sessionRestoreFailed bool
	status               RunStatus
	latestMetrics        MetricsSnapshot
	nextRun              uint64
	cancelled            map[RunID]struct{}
	closed               bool
	closeOnce            sync.Once
	closeErr             error
}

var (
	_ Interaction      = (*Runtime)(nil)
	_ ConnectorRuntime = (*Runtime)(nil)
)

// New constructs the interaction runtime with explicitly supplied optional
// application services. Pass Services{} for a core-only runtime.
func New(runner Runner, services Services) *Runtime {
	if runner == nil {
		panic("runtime: nil runner")
	}
	validateServices(services)
	events := eventbus.NewEventBus()
	rt := &Runtime{
		runner:    runner,
		services:  services,
		events:    events,
		runs:      runstore.NewRunStore(0),
		status:    RunIdle,
		cancelled: make(map[RunID]struct{}),
	}
	events.AddObserver(rt.runs.Record)
	events.AddObserver(rt.recordMetrics)
	rt.approvals = broker.NewApprovalBroker(events, SourceRef{Kind: "runtime"}, rt.currentRunID)
	rt.questions = broker.NewQuestionBroker(events, SourceRef{Kind: "runtime"}, rt.currentRunID)
	rt.connectors = connectors.NewManager(rt)
	return rt
}

func validateServices(services Services) {
	requireCompleteService("commands", services.ExecuteCommand != nil, services.CommandMenu != nil)
	requireCompleteService("context", services.ContextSnapshot != nil, services.MemoryView != nil)
	requireCompleteService("config", services.ConfigView != nil, services.ResolveModelProfile != nil, services.ApplyConfig != nil)
	requireCompleteService("sessions", services.CurrentSessionID != nil, services.ListSessions != nil, services.SessionMessages != nil)
}

func requireCompleteService(name string, present ...bool) {
	count := 0
	for _, ok := range present {
		if ok {
			count++
		}
	}
	if count != 0 && count != len(present) {
		panic("runtime: incomplete " + name + " services")
	}
}

func (r *Runtime) registerConnectorCommands() {
	r.registerCommand("connect", "Configure or connect a messaging channel", func(ctx context.Context, args []string) (string, error) {
		return r.connectors.Handle(ctx, args)
	})
	r.registerCommand("disconnect", "Disconnect a messaging channel", func(ctx context.Context, args []string) (string, error) {
		connName := ""
		if len(args) > 0 {
			connName = args[0]
		}
		resp, err := r.connectors.Disconnect(connName)
		if err == nil && connName != "" {
			resp = ""
		}
		return resp, err
	})
}

// registerCommand adds a runtime-owned slash command.
func (r *Runtime) registerCommand(name, description string, handler commandHandler) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" || handler == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.runtimeCommands == nil {
		r.runtimeCommands = make(map[string]runtimeCommand)
	}
	r.runtimeCommands[name] = runtimeCommand{description: strings.TrimSpace(description), handle: handler}
}

func (r *Runtime) currentRunID() RunID {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.currentRun
}

func (r *Runtime) ensureOpen() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return protocolError(ErrorClosed, "", "closed")
	}
	return nil
}

func (r *Runtime) beginWaiting(runID RunID, status RunStatus) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.currentRun != runID || r.status != RunRunning {
		return false
	}
	if _, cancelled := r.cancelled[runID]; cancelled {
		return false
	}
	r.status = status
	return true
}

type cancelControl struct {
	runID     RunID
	cancel    context.CancelFunc
	lease     *runLease
	execution *runExecution
	published chan struct{}
}

func (r *Runtime) beginCancel(runID RunID) (cancelControl, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return cancelControl{}, false, protocolError(ErrorClosed, "cancel_run", "closed")
	}
	if runID == "" {
		runID = r.currentRun
	}
	if runID == "" || r.currentRun == "" {
		return cancelControl{}, false, nil
	}
	if runID != r.currentRun {
		return cancelControl{}, false, protocolError(ErrorConflict, "cancel_run", fmt.Sprintf("run %s is not active; current run is %s", runID, r.currentRun))
	}
	switch r.status {
	case RunRunning, RunWaitingApproval, RunWaitingQuestion:
	default:
		return cancelControl{runID: runID}, false, nil
	}
	r.cancelled[runID] = struct{}{}
	r.status = RunCancelled
	r.cancelDone = make(chan struct{})
	control := cancelControl{
		runID: runID, cancel: r.cancelRun, lease: r.runLease, execution: r.runExecution,
		published: r.cancelDone,
	}
	r.cancelRun = nil
	r.runLease = nil
	return control, true, nil
}

func (r *Runtime) resumeRun(runID RunID, waitingStatus RunStatus) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.currentRun != runID || r.status != waitingStatus {
		return
	}
	_, cancelled := r.cancelled[runID]
	if !cancelled {
		r.status = RunRunning
	}
}
