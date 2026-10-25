package policy

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"nekocode/bot/policy/contract"
)

// HookCounters tracks admitted callbacks and decisions in the current run.
type HookCounters struct {
	Evaluations  int64 `json:"evaluations"`
	Hints        int64 `json:"hints"`
	Stops        int64 `json:"stops"`
	BlockTools   int64 `json:"block_tools"`
	RequireTools int64 `json:"require_tools"`
	BlockFinals  int64 `json:"block_finals"`
	Failures     int64 `json:"failures"`
	Rejections   int64 `json:"rejections"`
}

func (c HookCounters) String() string {
	return fmt.Sprintf("hooks: %d eval, %d hints, %d stops, %d block_tool, %d require_tool, %d block_final, %d rejects, %d failures",
		c.Evaluations, c.Hints, c.Stops, c.BlockTools, c.RequireTools, c.BlockFinals, c.Rejections, c.Failures)
}

// hookEngine owns hook registration, hook-private state and audit output.
// Policy is its only production caller.
type hookEngine struct {
	evalGate       chan struct{}
	mu             sync.Mutex
	hooks          []Hook
	state          map[string]hookMemory
	counts         HookCounters
	audit          []HookEvent
	session        string
	sessionVersion uint64
	versions       map[string]uint64
	nextVersion    uint64
	runID          string
	sessionStarted bool
	input          string
	evaluationID   uint64
	active         *HookExecution
}

func newHookEngine() *hookEngine {
	return &hookEngine{evalGate: make(chan struct{}, 1), state: make(map[string]hookMemory), versions: make(map[string]uint64)}
}

func (e *hookEngine) register(h Hook) error {
	if h.Name == "" || (h.On == nil && h.Run == nil) {
		return errors.New("hook requires a name and handler")
	}
	if !contract.KnownPoint(h.Point) {
		return fmt.Errorf("unknown hook point %q", h.Point)
	}
	if h.Timeout < 0 || h.Timeout > time.Minute {
		return errors.New("hook timeout must be between zero and one minute")
	}
	if h.On != nil && h.Run != nil {
		return errors.New("hook must define exactly one handler")
	}
	if h.Point == SessionStart {
		h.Once = true
		h.SessionScoped = true
	}
	e.mu.Lock()
	e.nextVersion++
	e.versions[h.Name] = e.nextVersion
	for i := range e.hooks {
		if e.hooks[i].Name == h.Name {
			e.hooks[i] = h
			delete(e.state, h.Name)
			e.mu.Unlock()
			return nil
		}
	}
	e.hooks = append(e.hooks, h)
	e.mu.Unlock()
	return nil
}

func (e *hookEngine) evaluate(point HookPoint, facts Facts, contexts ...context.Context) []Result {
	ctx := context.Background()
	if len(contexts) > 0 && contexts[0] != nil {
		ctx = contexts[0]
	}
	parent := ctx
	ctx, cancelBudget := context.WithTimeout(parent, 30*time.Second)
	defer cancelBudget()
	select {
	case <-ctx.Done():
		return []Result{evaluationCanceled(parent, ctx)}
	case e.evalGate <- struct{}{}:
	}
	defer func() { <-e.evalGate }()
	if ctx.Err() != nil {
		return []Result{evaluationCanceled(parent, ctx)}
	}

	e.mu.Lock()
	hooks := slices.Clone(e.hooks)
	versions := maps.Clone(e.versions)
	session := e.session
	runID := e.runID
	sessionVersion := e.sessionVersion
	e.evaluationID++
	evaluationID := e.evaluationID
	e.mu.Unlock()

	var results []Result
	for _, h := range hooks {
		if h.Point != point {
			continue
		}
		e.mu.Lock()
		if e.sessionVersion != sessionVersion {
			e.mu.Unlock()
			break
		}
		if e.versions[h.Name] != versions[h.Name] {
			e.mu.Unlock()
			continue
		}
		if ctx.Err() != nil {
			e.mu.Unlock()
			results = append(results, evaluationCanceled(parent, ctx))
			break
		}
		if h.Once && e.state[h.Name].once {
			e.mu.Unlock()
			continue
		}
		e.counts.Evaluations++
		state := e.stateFor(h.Name, facts)
		state.sessionID = session
		state.runID = runID
		state.input = e.input
		state.evaluationID = evaluationID
		started := time.Now()
		actorID := facts.ActorID
		if actorID == "" {
			actorID = "main"
		}
		e.active = &HookExecution{EvaluationID: evaluationID, Hook: h.Name, Point: point, SessionID: session, RunID: runID, ActorID: actorID, StartedAt: started}
		e.mu.Unlock()

		result, valid := executeHook(ctx, h, state)
		if ctx.Err() != nil {
			canceled := evaluationCanceled(parent, ctx)
			result = &canceled
			valid = false
		}

		e.mu.Lock()
		e.active = nil
		current := e.versions[h.Name] == versions[h.Name] && e.sessionVersion == sessionVersion
		if current && valid {
			e.saveState(h.Name, state)
			memory := e.state[h.Name]
			memory.once = h.Once
			e.state[h.Name] = memory
			if result != nil {
				result.StatePatch = nil
			}
		}
		memory := e.state[h.Name]
		entries, bytes := contract.StateSize(contract.StatePatch{Ints: memory.ints, Strings: memory.strings})
		if current && result != nil {
			results = append(results, *result)
			e.count(point, result)
		}
		if current && !valid {
			e.counts.Failures++
		}
		e.mu.Unlock()

		// DescribeTrigger is trusted user code and must run outside locks.
		auditResult := result
		if auditResult == nil {
			auditResult = &Result{Decision: &Decision{Action: Continue}}
		}
		event := HookEvent{SessionID: session, Hook: h.Name, Point: point, Action: "discarded", Detail: "registration or session changed"}
		if current {
			event = safeHookAuditEvent(h, point, state, auditResult, session)
		}
		event.RunID = runID
		event.EvaluationID = evaluationID
		event.ActorID = facts.ActorID
		if event.ActorID == "" {
			event.ActorID = "main"
		}
		event.DurationMS = time.Since(started).Milliseconds()
		event.StateCommitted = current && valid
		event.StateEntries, event.StateBytes = entries, bytes
		e.recordAudit(event)
	}
	e.mu.Lock()
	changed := e.sessionVersion != sessionVersion
	e.mu.Unlock()
	if changed {
		results = []Result{{Decision: &Decision{Action: Terminate, StopReason: StopPolicyBlocked, Reason: "session changed during hook evaluation"}}}
	}

	if len(results) > 0 {
		logHookResolution(session, runID, evaluationID, point, Resolve(results))
	}
	return results
}

func (e *hookEngine) count(point HookPoint, result *Result) {
	e.counts.Hints += int64(len(result.Hints))
	if result.Hint != nil {
		e.counts.Hints++
	}
	d, err := contract.Control(*result)
	if err != nil {
		return
	}
	switch d.Action {
	case Terminate:
		e.counts.Stops++
	case Reject:
		e.counts.Rejections++
		if point == PreToolUse {
			e.counts.BlockTools++
		}
		if point == Stop {
			e.counts.BlockFinals++
		}
	}
	if result.RequireTool != nil {
		e.counts.RequireTools++
	}
}

func (e *hookEngine) reset() {
	e.evalGate <- struct{}{}
	defer func() { <-e.evalGate }()
	e.mu.Lock()
	for _, h := range e.hooks {
		if !h.SessionScoped {
			delete(e.state, h.Name)
		}
	}
	e.runID = uuid.NewString()
	e.evaluationID = 0
	e.counts = HookCounters{}
	e.audit = nil
	e.mu.Unlock()
}

func (e *hookEngine) summary() string {
	e.mu.Lock()
	counts := e.counts
	audit := slices.Clone(e.audit)
	e.mu.Unlock()

	if len(audit) == 0 {
		return " | " + counts.String()
	}
	return " | " + counts.String() + " | hook events: " +
		formatHookEventSummary(audit) + " | recent hooks: " +
		formatHookAudit(recentHookAudit(audit, 5))
}

func (e *hookEngine) setSessionID(id string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.session != id {
		e.sessionVersion++
		clear(e.state)
		e.session = id
		e.runID = ""
		e.input = ""
		e.evaluationID = 0
		e.counts = HookCounters{}
		e.audit = nil
	}
}

func (e *hookEngine) recordAudit(event HookEvent) {
	e.mu.Lock()
	if len(e.audit) == maxHookAuditEvents {
		copy(e.audit, e.audit[1:])
		e.audit[len(e.audit)-1] = event
	} else {
		e.audit = append(e.audit, event)
	}
	e.mu.Unlock()
	logHookAudit(event)
}

func (e *hookEngine) unregisterPrefix(prefix string) {
	if prefix == "" {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	filtered := e.hooks[:0]
	for _, hook := range e.hooks {
		if !strings.HasPrefix(hook.Name, prefix) {
			filtered = append(filtered, hook)
			continue
		}
		delete(e.state, hook.Name)
		delete(e.versions, hook.Name)
	}
	e.hooks = filtered
}
