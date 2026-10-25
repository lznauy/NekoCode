// Package policy owns deterministic agent behavior policy. Policy is the only
// runtime entry: it combines the tool-event ledger with hook evaluation. See
// README.md for the package's governance principles and non-goals.
package policy

import (
	"context"
	"sync"
	"time"

	"nekocode/bot/policy/ledger"
)

type Policy struct {
	mu sync.Mutex

	hooks  *hookEngine
	ledger *ledger.Ledger

	garbledCount int
}

// New initializes all policy modules with an empty hook set.
func New() *Policy {
	return &Policy{
		hooks:  newHookEngine(),
		ledger: ledger.New(),
	}
}

// Register installs a hook. A hook with the same name is replaced.
func (p *Policy) Register(hook Hook) error {
	if p != nil {
		return p.hooks.register(hook)
	}
	return nil
}

// UnregisterPrefix removes hooks whose names start with prefix.
func (p *Policy) UnregisterPrefix(prefix string) {
	if p != nil {
		p.hooks.unregisterPrefix(prefix)
	}
}

// SetSessionID attaches the current session id to policy audit events.
func (p *Policy) SetSessionID(id string) {
	if p != nil {
		p.hooks.mu.Lock()
		old := p.hooks.session
		p.hooks.mu.Unlock()
		if old != id {
			p.EndSession(context.Background())

			p.hooks.setSessionID(id)
		}
	}
}

// ResetRun clears all run policy state, including read authorization evidence.
func (p *Policy) ResetRun() {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.garbledCount = 0
	p.mu.Unlock()
	p.ledger.ResetRun()
	p.hooks.reset()
}

// BeforeModel evaluates policy immediately before a model request.
func (p *Policy) BeforeModel(contexts ...context.Context) []Result {
	if p == nil {
		return nil
	}
	return p.evaluate(PreModel, ToolFacts{}, contexts...)
}

// BeforeStop records the response outcome and lets policy accept or reject it.
func (p *Policy) BeforeStop(result TurnResult, contexts ...context.Context) []Result {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	if result.Garbled {
		p.garbledCount++
	}
	p.mu.Unlock()
	facts := p.facts(ToolFacts{})
	facts.Response.Intent = result.Intent
	facts.Response.Text = result.Text
	return p.hooks.evaluate(Stop, facts, contexts...)
}

// OnUserSubmit evaluates hooks after user input enters the run.
func (p *Policy) OnUserSubmit(contexts ...context.Context) []Result {
	if p == nil {
		return nil
	}
	return p.evaluate(UserSubmit, ToolFacts{}, contexts...)
}

// Snapshot returns the persistable event ledger.
func (p *Policy) Snapshot() ledger.Snapshot {
	if p == nil {
		return ledger.Snapshot{}
	}
	return p.ledger.Snapshot()
}

// Restore replaces the event ledger from a persisted snapshot.
func (p *Policy) Restore(snapshot ledger.Snapshot) {
	if p != nil {
		p.ledger.Restore(snapshot)
	}
}

// Summary returns run-level ledger and hook audit statistics.
func (p *Policy) Summary() string {
	if p == nil {
		return ""
	}
	return p.ledger.Snapshot().Summary() + p.hooks.summary()
}

func (p *Policy) evaluate(point HookPoint, tool ToolFacts, contexts ...context.Context) []Result {
	return p.hooks.evaluate(point, p.facts(tool), contexts...)
}

func (p *Policy) facts(tool ToolFacts) Facts {
	p.mu.Lock()
	garbledCount := p.garbledCount
	p.mu.Unlock()
	return Facts{
		Tool:     tool,
		Response: ResponseFacts{GarbledCount: garbledCount},
	}
}

// StartRun is called after ResetRun and after installing the run cancellation.
func (p *Policy) StartRun(ctx context.Context, input string) []Result {
	if p == nil {
		return nil
	}
	p.hooks.mu.Lock()
	p.hooks.input = input
	p.hooks.sessionStarted = true
	p.hooks.mu.Unlock()
	results := p.hooks.evaluate(SessionStart, p.facts(ToolFacts{}), ctx)
	return append(results, p.hooks.evaluate(RunStart, p.facts(ToolFacts{}), ctx)...)
}

func (p *Policy) EndRun(ctx context.Context, outcomes ...TurnResult) {
	if p == nil {
		return
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer cancel()
	facts := p.facts(ToolFacts{})
	if len(outcomes) > 0 {
		facts.Response.Intent = outcomes[0].Intent
		facts.Response.Text = outcomes[0].Text
	}
	p.hooks.evaluate(RunEnd, facts, cleanup)
}

func (p *Policy) EndSession(ctx context.Context) {
	if p == nil {
		return
	}
	p.hooks.mu.Lock()
	started := p.hooks.sessionStarted
	p.hooks.sessionStarted = false
	p.hooks.mu.Unlock()
	if !started {
		return
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer cancel()
	p.hooks.evaluate(SessionEnd, p.facts(ToolFacts{}), cleanup)
}

// Observe exposes lifecycle facts without exposing the ledger or runtime state.
func (p *Policy) Observe(ctx context.Context, point HookPoint, facts Facts) {
	if p != nil {
		p.hooks.evaluate(point, facts, ctx)
	}
}
