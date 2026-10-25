package policy

import (
	"time"

	"nekocode/bot/policy/contract"
)

// HookInfo describes a registration and state usage, never its callbacks or values.
type HookInfo struct {
	Name          string    `json:"name"`
	Point         HookPoint `json:"point"`
	Generation    uint64    `json:"generation"`
	Guard         bool      `json:"guard"`
	TimeoutMS     int64     `json:"timeout_ms"`
	SessionScoped bool      `json:"session_scoped"`
	Once          bool      `json:"once"`
	OnceCompleted bool      `json:"once_completed"`
	StateEntries  int       `json:"state_entries"`
	StateBytes    int       `json:"state_bytes"`
}

// HookExecution remains inspectable while a trusted Go callback is blocked.
type HookExecution struct {
	EvaluationID uint64    `json:"evaluation_id"`
	Hook         string    `json:"hook"`
	Point        HookPoint `json:"point"`
	SessionID    string    `json:"session_id"`
	RunID        string    `json:"run_id"`
	ActorID      string    `json:"actor_id"`
	StartedAt    time.Time `json:"started_at"`
	ElapsedMS    int64     `json:"elapsed_ms"`
}

// Inspection is a detached view, safe to serialize or poll without executing hooks.
// Evaluation IDs are scoped to RunID; callback counters reset at ResetRun.
type Inspection struct {
	SessionID        string         `json:"session_id"`
	RunID            string         `json:"run_id"`
	LastEvaluationID uint64         `json:"last_evaluation_id"`
	Counters         HookCounters   `json:"counters"`
	Active           *HookExecution `json:"active,omitempty"`
	Hooks            []HookInfo     `json:"hooks"`
	Recent           []HookEvent    `json:"recent"`
}

func (p *Policy) Inspect() Inspection {
	out := Inspection{Hooks: []HookInfo{}, Recent: []HookEvent{}}
	if p == nil {
		return out
	}
	e := p.hooks
	e.mu.Lock()
	defer e.mu.Unlock()
	out.SessionID, out.RunID = e.session, e.runID
	out.LastEvaluationID = e.evaluationID
	out.Counters = e.counts
	out.Recent = append(out.Recent, e.audit...)
	if e.active != nil {
		active := *e.active
		active.ElapsedMS = time.Since(active.StartedAt).Milliseconds()
		out.Active = &active
	}
	for _, h := range e.hooks {
		memory := e.state[h.Name]
		entries, bytes := contract.StateSize(contract.StatePatch{Ints: memory.ints, Strings: memory.strings})
		out.Hooks = append(out.Hooks, HookInfo{Name: h.Name, Point: h.Point, Generation: e.versions[h.Name], Guard: h.Guard, TimeoutMS: hookTimeout(h.Timeout).Milliseconds(), SessionScoped: h.SessionScoped, Once: h.Once, OnceCompleted: memory.once, StateEntries: entries, StateBytes: bytes})
	}
	return out
}
