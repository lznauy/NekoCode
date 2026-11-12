package runtime

import (
	"context"
	"fmt"
	"nekocode/mode"
	"strings"
)

const backgroundDemandTextLimit = 64 << 10

// BackgroundDemand is a durable receipt. Recovery never replays a claimed turn.
type BackgroundDemand struct {
	ID        RunID  `json:"id"`
	SessionID string `json:"session_id"`
	Input     Input  `json:"input"`
	Status    string `json:"status"`
	Error     string `json:"error,omitempty"`
}

type backgroundState struct {
	RetiredByTask map[string]BackgroundUsage `json:"retired_by_task,omitempty"`
	RetiredUsage  BackgroundUsage            `json:"retired_usage"`
	Version       int                        `json:"version"`
	Tasks         []BackgroundTask           `json:"tasks"`
	Demands       []BackgroundDemand         `json:"demands,omitempty"`
}

func (b *backgroundTasks) addDemandLocked(d BackgroundDemand) error {
	count := 0
	for _, item := range b.demands {
		if item.SessionID == d.SessionID {
			count++
		}
	}
	if count >= backgroundHistoryLimit {
		for i, id := range b.demandOrder {
			item := b.demands[id]
			if item.SessionID == d.SessionID && (item.Status == "done" || item.Status == "cancelled") {
				delete(b.demands, id)
				b.demandOrder = append(b.demandOrder[:i], b.demandOrder[i+1:]...)
				count--
				break
			}
		}
	}
	if count >= backgroundHistoryLimit {
		return fmt.Errorf("demand history is full; inspect and discard pending receipts first")
	}
	b.demands[d.ID] = &d
	b.demandOrder = append(b.demandOrder, d.ID)
	return nil
}

func (r *Runtime) BackgroundDemands(sessionID string) ([]BackgroundDemand, error) {
	if err := r.ensureOpen(); err != nil {
		return nil, err
	}
	if sessionID == "" {
		sessionID = r.CurrentSessionID()
	}
	b := r.background
	if err := b.load(sessionID); err != nil {
		return nil, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	out := []BackgroundDemand{}
	for _, id := range b.demandOrder {
		if item := b.demands[id]; item.SessionID == sessionID {
			out = append(out, *item)
		}
	}
	return out, nil
}

// ResumeBackgroundDemand admits only receipts known not to have started before
// recovery. Interrupted turns require inspection and a fresh explicit demand.
func (r *Runtime) ResumeBackgroundDemand(ctx context.Context, sessionID string, id RunID) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mutationMu.Lock()
	defer r.mutationMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := r.ensureOpen(); err != nil {
		return err
	}
	if sessionID != r.CurrentSessionID() || r.services.Modes.Current == nil || r.services.Modes.Current() != mode.SuperAgent {
		return fmt.Errorf("select this session in Super Agent mode before resuming")
	}
	b := r.background
	if err := b.load(sessionID); err != nil {
		return err
	}
	b.mu.Lock()
	d := b.demands[id]
	if b.closed || d == nil || d.SessionID != sessionID || d.Status != "pending" {
		b.mu.Unlock()
		return fmt.Errorf("only unstarted recovered demands may be resumed")
	}
	if len(b.inputs) >= backgroundQueueLimit {
		b.mu.Unlock()
		return fmt.Errorf("coordinator input queue is full")
	}
	inputCtx, cancel := context.WithCancel(b.ctx)
	in := &backgroundInput{input: d.Input, sessionID: sessionID, id: id, ctx: inputCtx, cancel: cancel, done: make(chan struct{})}
	d.Status = "queued"
	if err := b.saveLocked(sessionID); err != nil {
		d.Status = "pending"
		b.mu.Unlock()
		cancel()
		return err
	}
	b.inputs = append(b.inputs, in)
	b.pending[id] = in
	b.mu.Unlock()
	b.signal()
	return nil
}

func (r *Runtime) DiscardBackgroundDemand(ctx context.Context, sessionID string, id RunID) error {
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
	b.mu.Lock()
	defer b.mu.Unlock()
	d := b.demands[id]
	if b.closed || d == nil || d.SessionID != sessionID {
		return fmt.Errorf("demand not found in session")
	}
	if d.Status != "pending" && d.Status != "interrupted" && d.Status != "failed" {
		return fmt.Errorf("cancel a live demand through its run ID")
	}
	previous := d.Status
	d.Status = "cancelled"
	if err := b.saveLocked(sessionID); err != nil {
		d.Status = previous
		return err
	}
	return nil
}

func validBackgroundDemand(d BackgroundDemand, sessionID string) bool {
	if !strings.HasPrefix(string(d.ID), "queued_") || strings.HasPrefix(strings.TrimSpace(d.Input.Text), "/") || d.SessionID != sessionID || strings.TrimSpace(d.Input.Text) == "" || len(d.Input.Text) > backgroundDemandTextLimit || len(d.Input.Images) > 0 {
		return false
	}
	switch d.Status {
	case "queued", "claimed", "pending", "interrupted", "done", "cancelled", "failed":
		return true
	}
	return false
}
