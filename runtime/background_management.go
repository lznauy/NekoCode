package runtime

import (
	"context"
	"fmt"
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
	b.mu.Lock()
	defer b.mu.Unlock()
	e := b.entries[id]
	if b.closed || e == nil || e.SessionID != sessionID || e.cancel != nil || e.Delivery != "failed" {
		return fmt.Errorf("only failed result reviews may be retried")
	}
	previous := e.DeliveryError
	e.Delivery = ""
	e.DeliveryError = ""
	if err := b.saveLocked(sessionID); err != nil {
		e.Delivery = "failed"
		e.DeliveryError = previous
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
			previousStatus, previousError := e.Status, e.Error
			if failed {
				e.Status = "blocked"
				e.Error = "upstream did not complete successfully"
			} else {
				e.Status = "queued"
			}
			if err := b.saveLocked(e.SessionID); err != nil {
				e.Status, e.Error = previousStatus, previousError
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
	b.mu.Lock()
	defer b.mu.Unlock()
	e := b.entries[h.id]
	if !h.owns(e) || e.cancel == nil {
		return fmt.Errorf("task is no longer active")
	}
	previous, previousAttempt := e.Usage, e.AttemptUsage
	// Each checkpoint is cumulative within one attempt, not an additive sample.
	e.Usage.TotalTokens += u.TotalTokens - e.AttemptUsage.TotalTokens
	e.Usage.CacheHitTokens += u.CacheHitTokens - e.AttemptUsage.CacheHitTokens
	e.Usage.CacheMissTokens += u.CacheMissTokens - e.AttemptUsage.CacheMissTokens
	e.Usage.ToolUseCount += u.ToolUseCount - e.AttemptUsage.ToolUseCount
	e.Usage.DurationMs += u.DurationMs - e.AttemptUsage.DurationMs
	e.AttemptUsage = BackgroundUsage{u.TotalTokens, u.CacheHitTokens, u.CacheMissTokens, u.ToolUseCount, u.DurationMs}
	if err := b.saveLocked(h.sessionID); err != nil {
		e.Usage = previous
		e.AttemptUsage = previousAttempt
		return err
	}
	return nil
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

func (b *backgroundTasks) retireUsageLocked(e *backgroundEntry) {
	if b.retiredByTask == nil {
		b.retiredByTask = map[string]map[string]BackgroundUsage{}
	}
	if b.retiredByTask[e.SessionID] == nil {
		b.retiredByTask[e.SessionID] = map[string]BackgroundUsage{}
	}
	b.retired[e.SessionID] = addBackgroundUsage(b.retired[e.SessionID], e.Usage)
	byTask := b.retiredByTask[e.SessionID]
	byTask[e.ID] = addBackgroundUsage(byTask[e.ID], e.Usage)
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
