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
	Limit                int `json:"limit"`
	Admitted             int `json:"admitted"`
	WorkRemaining        int `json:"work_remaining"`
	HandoffRemaining     int `json:"handoff_remaining"`
	CoordinatorRemaining int `json:"coordinator_remaining"`
}
type Budget struct {
	mu       sync.Mutex
	snapshot Snapshot
	handoffs map[string]bool
}

// New reserves at most eight emergency worker handoffs and four coordinator
// requests. These are safety reserves, not allocations to individual tasks.
func New(limit int) (*Budget, error) {
	if limit < 1 || limit > 1000000 {
		return nil, fmt.Errorf("model_call_budget must be between 1 and 1000000")
	}
	coordinator := min(4, max(1, limit/10))
	handoff := min(8, max(0, (limit-coordinator)/5))
	return &Budget{snapshot: Snapshot{Limit: limit, WorkRemaining: limit - coordinator - handoff, HandoffRemaining: handoff, CoordinatorRemaining: coordinator}, handoffs: map[string]bool{}}, nil
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
	budget *Budget
	role   Role
	actor  string
}

func WithBudget(ctx context.Context, b *Budget) context.Context {
	s, _ := ctx.Value(contextKey{}).(scope)
	s.budget = b
	return context.WithValue(ctx, contextKey{}, s)
}
func FromContext(ctx context.Context) *Budget {
	s, _ := ctx.Value(contextKey{}).(scope)
	return s.budget
}
func WithRole(ctx context.Context, role Role, actor string) context.Context {
	s, _ := ctx.Value(contextKey{}).(scope)
	s.role = role
	s.actor = actor
	return context.WithValue(ctx, contextKey{}, s)
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
func Notice(ctx context.Context) string {
	b := FromContext(ctx)
	if b == nil {
		return ""
	}
	s := b.Snapshot()
	return fmt.Sprintf("[Shared execution budget] %d/%d model requests admitted (includes in-flight requests and retries). Ordinary requests remaining: %d; emergency worker handoffs: %d; coordinator reserve: %d. Reuse evidence and continue the original task when justified. When ordinary requests reach zero, do not start or resume investigation: hand off current evidence and synthesize an explicitly limited answer. Reserves do not authorize more investigation.", s.Admitted, s.Limit, s.WorkRemaining, s.HandoffRemaining, s.CoordinatorRemaining)
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
