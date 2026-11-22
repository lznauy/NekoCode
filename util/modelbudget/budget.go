// Package modelbudget admits physical model requests across one host-owned
// execution tree. Admission counts in-flight requests and never refunds errors.
package modelbudget

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

var ErrExhausted = errors.New("shared model call budget exhausted")

type Role uint8

const (
	Work Role = iota
	Handoff
	Coordinator
)

type Snapshot struct {
	ExternalTokenLimit   int64 `json:"external_token_limit,omitempty"`
	ObservedTokens       int64 `json:"observed_tokens,omitempty"`
	ParallelStepSharing  bool  `json:"parallel_step_sharing,omitempty"`
	Limit                int   `json:"limit"`
	Admitted             int   `json:"admitted"`
	WorkRemaining        int   `json:"work_remaining"`
	HandoffRemaining     int   `json:"handoff_remaining"`
	CoordinatorRemaining int   `json:"coordinator_remaining"`
}
type Budget struct {
	mu       sync.Mutex
	snapshot Snapshot
	handoffs map[string]bool
}

// New reserves at most eight emergency worker handoffs and four coordinator
// requests. These are safety reserves, not allocations to individual tasks.
func New(limit int) (*Budget, error) {
	return NewWithLimits(limit, false, 0)
}

// NewWithLimits publishes the external token cutoff as advisory context.
// Only request counts take part in native admission.
func NewWithLimits(limit int, enabled bool, externalTokens int64) (*Budget, error) {
	if externalTokens < 0 {
		return nil, fmt.Errorf("external_token_limit must be nonnegative")
	}
	if limit < 1 || limit > 1000000 {
		return nil, fmt.Errorf("model_call_budget must be between 1 and 1000000")
	}
	coordinator := min(4, max(1, limit/10))
	handoff := min(8, max(0, (limit-coordinator)/5))
	return &Budget{snapshot: Snapshot{ExternalTokenLimit: externalTokens, ParallelStepSharing: enabled, Limit: limit, WorkRemaining: limit - coordinator - handoff, HandoffRemaining: handoff, CoordinatorRemaining: coordinator}, handoffs: map[string]bool{}}, nil
}
func (b *Budget) Snapshot() Snapshot {
	if b == nil {
		return Snapshot{}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.snapshot
}

type contextKey struct{}
type scope struct {
	budget         *Budget
	role           Role
	actor          string
	parallelSteps  int
	settleHandoffs func(*Budget)
}

func WithBudget(ctx context.Context, b *Budget) context.Context {
	s, _ := ctx.Value(contextKey{}).(scope)
	s.budget = b
	return context.WithValue(ctx, contextKey{}, s)
}
func FromContext(ctx context.Context) *Budget {
	if ctx == nil {
		return nil
	}
	s, _ := ctx.Value(contextKey{}).(scope)
	return s.budget
}
func WithRole(ctx context.Context, role Role, actor string) context.Context {
	s, _ := ctx.Value(contextKey{}).(scope)
	s.role = role
	s.actor = actor
	return context.WithValue(ctx, contextKey{}, s)
}

// WithParallelWorkers snapshots an attempt ceiling once before dispatch.
// Split ordinary calls across the batch plus one share for integration, and
// leave room in each worker share for the structured handoff. Physical
// requests (including retries) remain governed by Admit.
func WithParallelWorkers(ctx context.Context, workers int) context.Context {
	s, _ := ctx.Value(contextKey{}).(scope)
	if workers < 2 || s.budget == nil {
		return ctx
	}
	snapshot := s.budget.Snapshot()
	if !snapshot.ParallelStepSharing {
		return ctx
	}
	s.parallelSteps = max(1, snapshot.WorkRemaining/(workers+1)-1)
	return context.WithValue(ctx, contextKey{}, s)
}

func ParallelStepLimit(ctx context.Context) int {
	s, _ := ctx.Value(contextKey{}).(scope)
	return s.parallelSteps
}

// WithHandoffSettlement lets the root coordinator reclaim unused handoffs at
// its next request boundary, after synchronous tools have returned. The host
// checks outstanding asynchronous work under its scheduler lock.
func WithHandoffSettlement(ctx context.Context, settle func(*Budget)) context.Context {
	s, _ := ctx.Value(contextKey{}).(scope)
	s.settleHandoffs = settle
	return context.WithValue(ctx, contextKey{}, s)
}

func (s scope) settleIfNeeded() {
	if s.role != Coordinator || s.settleHandoffs == nil || s.budget == nil {
		return
	}
	snapshot := s.budget.Snapshot()
	if snapshot.WorkRemaining == 0 && snapshot.HandoffRemaining > 0 {
		s.settleHandoffs(s.budget)
	}
}

func Admit(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s, _ := ctx.Value(contextKey{}).(scope)
	b := s.budget
	if b == nil {
		return nil
	}
	s.settleIfNeeded()
	b.mu.Lock()
	defer b.mu.Unlock()
	switch {
	case b.snapshot.WorkRemaining > 0:
		b.snapshot.WorkRemaining--
	case s.role == Coordinator && b.snapshot.CoordinatorRemaining > 0:
		b.snapshot.CoordinatorRemaining--
	case s.role == Handoff && s.actor != "" && !b.handoffs[s.actor] && b.snapshot.HandoffRemaining > 0:
		b.snapshot.HandoffRemaining--
		b.handoffs[s.actor] = true
	default:
		return ErrExhausted
	}
	b.snapshot.Admitted++
	return nil
}

// ObserveTokens counts reported usage from a finished physical request.
func (b *Budget) ObserveTokens(total int, reported bool) {
	if b == nil || !reported || total <= 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.snapshot.ExternalTokenLimit > 0 {
		b.snapshot.ObservedTokens += int64(total)
	}
}

func Notice(ctx context.Context) string {
	b := FromContext(ctx)
	if b == nil {
		return ""
	}
	state, _ := ctx.Value(contextKey{}).(scope)
	state.settleIfNeeded()
	s := b.Snapshot()
	notice := fmt.Sprintf("[Shared execution budget] %d/%d model requests admitted (includes in-flight requests and retries). Ordinary requests remaining: %d; emergency worker handoffs: %d; coordinator reserve: %d.", s.Admitted, s.Limit, s.WorkRemaining, s.HandoffRemaining, s.CoordinatorRemaining)
	if s.ExternalTokenLimit > 0 {
		notice += fmt.Sprintf(" External token cutoff: %d. Observed input + output tokens across this execution: %d (lower bound; excludes in-flight and unreported usage).", s.ExternalTokenLimit, s.ObservedTokens)
	}
	if s.WorkRemaining == 0 {
		notice += " Ordinary requests exhausted. Remaining model requests are reserved for handoff and final synthesis."
	}
	return notice
}

// ReleaseHandoffs transfers unused emergency worker capacity to final synthesis
// only after the trusted host observes every worker settled and ordinary work
// exhausted. Total admissions never increase; no new investigation is admitted.
func (b *Budget) ReleaseHandoffs() {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.snapshot.WorkRemaining != 0 {
		return
	}
	b.snapshot.CoordinatorRemaining += b.snapshot.HandoffRemaining
	b.snapshot.HandoffRemaining = 0
}
