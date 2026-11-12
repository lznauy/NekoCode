package runtime

import (
	"context"
	"fmt"
	"nekocode/protocol"
	"strings"
)

// AppendBackgroundTask addresses a particular task, never the foreground agent.
// Updates are applied between model turns; an in-progress tool is not interrupted.
func (r *Runtime) AppendBackgroundTask(ctx context.Context, sessionID, id, text string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := r.ensureOpen(); err != nil {
		return err
	}
	text = strings.TrimSpace(text)
	if text == "" || len(text) > 8192 {
		return fmt.Errorf("task update must contain 1–8192 bytes")
	}
	b := r.background
	if err := b.load(sessionID); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	e := b.entries[id]
	if b.closed || e == nil || e.SessionID != sessionID {
		return fmt.Errorf("background task not found in session")
	}
	if (e.Status != "queued" && e.Status != "waiting_dependencies") && (e.cancel == nil || !backgroundActive(e.Status)) {
		return fmt.Errorf("task has ended; resume it explicitly")
	}
	if len(e.PendingUpdates) >= 16 {
		return fmt.Errorf("task update queue is full")
	}
	previous := e.PendingUpdates
	previousSequence := e.UpdateSequence
	if e.UpdateSequence < uint64(len(previous)) {
		e.UpdateSequence = uint64(len(previous))
	}
	if e.UpdateSequence == ^uint64(0) {
		return fmt.Errorf("task update sequence exhausted")
	}
	e.UpdateSequence++
	e.PendingUpdates = append(append([]string(nil), previous...), text)
	if err := b.saveLocked(sessionID); err != nil {
		e.PendingUpdates = previous
		e.UpdateSequence = previousSequence
		return err
	}
	return nil
}

func backgroundActive(status string) bool {
	return status == "running" || strings.HasPrefix(status, "waiting_")
}

// ApplyUpdates removes the captured prefix only after the child checkpoint succeeds.
// No runtime lock is held while calling the worker-owned checkpoint.
func (h backgroundHost) ApplyUpdates(apply func([]protocol.BackgroundUpdate) error) error {
	b := h.runtime.background
	b.mu.Lock()
	if err := h.ctx.Err(); err != nil {
		b.mu.Unlock()
		return err
	}
	e := b.entries[h.id]
	if !h.owns(e) || e.cancel == nil {
		b.mu.Unlock()
		return fmt.Errorf("task is no longer active")
	}
	if e.UpdateSequence < uint64(len(e.PendingUpdates)) {
		e.UpdateSequence = uint64(len(e.PendingUpdates))
	}
	updates := make([]protocol.BackgroundUpdate, len(e.PendingUpdates))
	for i, text := range e.PendingUpdates {
		updates[i] = protocol.BackgroundUpdate{Sequence: e.UpdateSequence - uint64(len(e.PendingUpdates)) + uint64(i) + 1, Text: text}
	}
	b.mu.Unlock()
	if len(updates) == 0 {
		return nil
	}
	if err := apply(updates); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.entries[h.id] != e {
		return fmt.Errorf("task attempt changed")
	}
	previous := e.PendingUpdates
	e.PendingUpdates = append([]string(nil), previous[len(updates):]...)
	if err := b.saveLocked(h.sessionID); err != nil {
		e.PendingUpdates = previous
		return err
	}
	return nil
}
