package runtime

import (
	"context"
	"fmt"
	"maps"
	"nekocode/protocol"
)

// RetryBackgroundNotification replays review only, never the execution closure.
func (r *Runtime) RetryBackgroundNotification(ctx context.Context, sessionID, id string) error {
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
	b.lockMutation()
	defer b.unlockMutation()
	e := b.entries[id]
	if b.closed || e == nil || e.SessionID != sessionID || e.cancel != nil || e.Delivery != "failed" {
		return fmt.Errorf("only failed result reviews may be retried")
	}
	next := e.BackgroundTask
	next.Delivery, next.DeliveryError = "", ""
	if err := b.saveTaskLocked(e, next); err != nil {
		return err
	}
	b.signal()
	return nil
}

func (b *backgroundTasks) dependedOnLocked(id string) bool {
	for _, e := range b.entries {
		if e.Status != "queued" && e.Status != "waiting_dependencies" && e.cancel == nil {
			continue
		}
		for _, upstream := range e.After {
			if upstream == id {
				return true
			}
		}
	}
	return false
}

func (b *backgroundTasks) resolveDependenciesLocked() error {
	// Registration only permits backward edges. Iterate so a blocked upstream
	// cascades in this wake even if admission order changes after recovery.
	for changed := true; changed; {
		changed = false
		for _, e := range b.entries {
			if e.Status != "waiting_dependencies" {
				continue
			}
			ready, failed := true, false
			for _, id := range e.After {
				up := b.entries[id]
				if up == nil || up.Status == "failed" || up.Status == "partial" || up.Status == "interrupted" || up.Status == "blocked" {
					failed = true
					break
				}
				if up.Status != "completed" || up.cancel != nil {
					ready = false
				}
			}
			if !failed && !ready {
				continue
			}
			next := e.BackgroundTask
			if failed {
				next.Status = "blocked"
				next.Error = "upstream did not complete successfully"
			} else {
				next.Status = "queued"
			}
			if err := b.saveTaskLocked(e, next); err != nil {
				return fmt.Errorf("task %s dependency transition: %w", e.ID, err)
			}
			if failed {
				// Discard execution and release captured resources only after the
				// terminal dependency outcome is durably committed.
				e.work.Run = nil
				releaseBackgroundWork(e)
			}
			changed = true
		}
	}
	return nil
}

func (h backgroundHost) Usage(u protocol.BackgroundUsage) error {
	b := h.runtime.background
	b.lockMutation()
	defer b.unlockMutation()
	e := b.entries[h.id]
	if !h.owns(e) || e.cancel == nil {
		return fmt.Errorf("task is no longer active")
	}
	next := e.BackgroundTask
	// Each checkpoint is cumulative within one attempt, not an additive sample.
	next.Usage.TotalTokens += u.TotalTokens - next.AttemptUsage.TotalTokens
	next.Usage.CacheHitTokens += u.CacheHitTokens - next.AttemptUsage.CacheHitTokens
	next.Usage.CacheMissTokens += u.CacheMissTokens - next.AttemptUsage.CacheMissTokens
	next.Usage.ToolUseCount += u.ToolUseCount - next.AttemptUsage.ToolUseCount
	next.Usage.DurationMs += u.DurationMs - next.AttemptUsage.DurationMs
	next.AttemptUsage = BackgroundUsage{u.TotalTokens, u.CacheHitTokens, u.CacheMissTokens, u.ToolUseCount, u.DurationMs}
	return b.saveTaskLocked(e, next)
}

func (r *Runtime) BackgroundUsage(sessionID string) (BackgroundUsage, error) {
	if err := r.ensureOpen(); err != nil {
		return BackgroundUsage{}, err
	}
	if sessionID == "" {
		sessionID = r.CurrentSessionID()
	}
	b := r.background
	if err := b.load(sessionID); err != nil {
		return BackgroundUsage{}, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	total := b.retired[sessionID]
	for _, e := range b.entries {
		if e.SessionID != sessionID {
			continue
		}
		total.TotalTokens += e.Usage.TotalTokens
		total.CacheHitTokens += e.Usage.CacheHitTokens
		total.CacheMissTokens += e.Usage.CacheMissTokens
		total.ToolUseCount += e.Usage.ToolUseCount
		total.DurationMs += e.Usage.DurationMs
	}
	return total, nil
}

// removeTaskLocked publishes eviction and its usage transfer only after the
// owning session's snapshot is saved, including eviction across sessions.
func (b *backgroundTasks) removeTaskLocked(e *backgroundEntry) error {
	state := b.snapshotLocked(e.SessionID)
	for i, task := range state.Tasks {
		if task.ID == e.ID {
			state.Tasks = append(state.Tasks[:i], state.Tasks[i+1:]...)
			break
		}
	}
	state.RetiredUsage = addBackgroundUsage(state.RetiredUsage, e.Usage)
	state.RetiredByTask = maps.Clone(state.RetiredByTask)
	if state.RetiredByTask == nil {
		state.RetiredByTask = map[string]BackgroundUsage{}
	}
	state.RetiredByTask[e.ID] = addBackgroundUsage(state.RetiredByTask[e.ID], e.Usage)
	if err := b.saveStateLocked(e.SessionID, state); err != nil {
		return err
	}
	b.retired[e.SessionID] = state.RetiredUsage
	b.retiredByTask[e.SessionID] = state.RetiredByTask
	delete(b.entries, e.ID)
	for i, id := range b.order {
		if id == e.ID {
			b.order = append(b.order[:i], b.order[i+1:]...)
			break
		}
	}
	return nil
}

// Caller owns the task mutex. Every accepted task releases captured resources,
// including those cancelled or blocked before their execution function starts.
func releaseBackgroundWork(e *backgroundEntry) {
	if e.work.Release != nil {
		release := e.work.Release
		e.work.Release = nil
		release()
	}
}

func addBackgroundUsage(a, b BackgroundUsage) BackgroundUsage {
	return BackgroundUsage{TotalTokens: a.TotalTokens + b.TotalTokens, CacheHitTokens: a.CacheHitTokens + b.CacheHitTokens, CacheMissTokens: a.CacheMissTokens + b.CacheMissTokens, ToolUseCount: a.ToolUseCount + b.ToolUseCount, DurationMs: a.DurationMs + b.DurationMs}
}
func subtractRetiredUsage(a, b BackgroundUsage) BackgroundUsage {
	return BackgroundUsage{TotalTokens: max(0, a.TotalTokens-b.TotalTokens), CacheHitTokens: max(0, a.CacheHitTokens-b.CacheHitTokens), CacheMissTokens: max(0, a.CacheMissTokens-b.CacheMissTokens), ToolUseCount: max(0, a.ToolUseCount-b.ToolUseCount), DurationMs: max(0, a.DurationMs-b.DurationMs)}
}
