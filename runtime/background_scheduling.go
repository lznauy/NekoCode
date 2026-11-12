package runtime

import "nekocode/mode"

// Read application configuration before taking the background lock: config
// callbacks may take application locks also used by task admission.
func (r *Runtime) backgroundScheduling() (limit int, foregroundWriter bool) {
	limit = backgroundLimit
	if view := r.services.Config.ConfigView; view != nil {
		if configured := view().MaxConcurrentSubagents; configured > 0 {
			limit = configured
		}
	}
	r.mu.Lock()
	busy := r.status != RunIdle
	r.mu.Unlock()
	foregroundWriter = busy && r.services.Modes.Current != nil && r.services.Modes.Current() == mode.Standard
	return
}

func (b *backgroundTasks) queueReasonLocked(e *backgroundEntry, limit int, foregroundWriter bool) string {
	if e.Status == "waiting_dependencies" {
		return "waiting_dependencies"
	}
	if e.Status != "queued" {
		return ""
	}
	if !e.work.ReadOnly {
		if foregroundWriter {
			return "foreground_writer"
		}
		if b.workspaces[e.work.Workspace] {
			return "workspace_busy"
		}
	}
	if b.active >= limit {
		return "concurrency_limit"
	}
	return "dispatch_pending"
}
