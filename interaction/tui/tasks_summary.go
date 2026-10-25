package tui

import (
	"fmt"
	"strings"
	"time"

	"nekocode/interaction/tui/components/processing"
	controlruntime "nekocode/runtime"
)

func (m *Model) observeTaskLifecycle(ev controlruntime.Event) {
	if !canObserveTasks(m.Runtime) {
		return
	}
	// The main event stream is intentionally lossy. Reconcile from the run's
	// projection periodically and at termination rather than retaining an
	// unbounded lifecycle event backlog merely to count tasks.
	defer func() {
		if ev.Type != controlruntime.EventRunStarted && ev.RunID != "" && time.Since(m.taskSummarySyncedAt) >= taskActiveResyncInterval {
			m.syncTaskSummary(ev.RunID)
		}
	}()
	switch ev.Type {
	case controlruntime.EventRunDone, controlruntime.EventRunFailed, controlruntime.EventRunCancelled:
		m.syncTaskSummary(ev.RunID)
		if m.tasks != nil && m.tasks.open {
			m.tasks.listDirty = true
		}
	case controlruntime.EventApprovalRequested, controlruntime.EventApprovalResolved, controlruntime.EventQuestionRequested, controlruntime.EventQuestionResolved:
		if m.tasks != nil && m.tasks.open {
			m.tasks.listDirty = true
		}
	case controlruntime.EventSubAgentStarted, controlruntime.EventSubAgentUpdated, controlruntime.EventSubAgentEnded:
		p, ok := ev.Payload.(controlruntime.SubAgentPayload)
		if !ok || p.ID == "" {
			return
		}
		if m.taskStates == nil {
			m.taskStates = make(map[string]string)
		}
		// A resumed task's Started event may be missing from the lossy stream.
		// Reconcile the list even when the summary ignores a late update.
		if m.tasks != nil && m.tasks.open {
			m.tasks.listDirty = true
		}
		if ev.Type == controlruntime.EventSubAgentUpdated {
			previous := m.taskStates[p.ID]
			if previous != "" && previous != "queued" && previous != "running" && previous != "waiting_approval" && previous != "waiting_question" {
				return
			}
		}
		status := p.Status
		if status == "" {
			if ev.Type == controlruntime.EventSubAgentEnded {
				status = "unknown"
			} else {
				status = "running"
			}
		}
		if m.taskStates[p.ID] != status {
			m.taskStates[p.ID] = status
			m.Messages.UpdateProcessing(func(item *processing.ProcessingItem) {
				item.SetSubagentSummary(m.taskSummary() + " · Ctrl+G")
			})
		}
	}
}

func (m *Model) taskSummary() string {
	if len(m.taskStates) == 0 {
		return ""
	}
	var running, queued, completed, waiting, failed, unfinished int
	for _, status := range m.taskStates {
		switch status {
		case "queued":
			queued++
		case "running":
			running++
		case "waiting_approval", "waiting_question":
			running++
			waiting++
		case "completed":
			completed++
		case "failed", "unavailable", "not_started":
			failed++
		default:
			unfinished++
		}
	}
	parts := []string{"子任务"}
	counts := []struct {
		name  string
		count int
	}{{"运行中", running}, {"待处理", waiting}, {"失败", failed}, {"排队", queued}, {"未完成", unfinished}, {"完成", completed}}
	for _, part := range counts {
		if part.count > 0 {
			parts = append(parts, fmt.Sprintf("%s %d", part.name, part.count))
		}
	}
	return strings.Join(parts, " · ")
}

func (m *Model) syncTaskSummary(id controlruntime.RunID) {
	reader, ok := m.Runtime.(interface {
		LookupRun(controlruntime.RunID) (controlruntime.RunSnapshot, bool)
	})
	if !ok || id == "" {
		return
	}
	snapshot, ok := reader.LookupRun(id)
	if !ok {
		return
	}
	m.taskSummarySyncedAt = time.Now()
	m.taskStates = make(map[string]string, len(snapshot.SubAgents))
	for _, task := range snapshot.SubAgents {
		m.taskStates[task.ID] = task.Status
	}
	m.Messages.UpdateProcessing(func(item *processing.ProcessingItem) {
		summary := m.taskSummary()
		if summary != "" {
			summary += " · Ctrl+G"
		}
		item.SetSubagentSummary(summary)
	})
}
