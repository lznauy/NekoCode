package tui

import (
	"context"
	"fmt"
	"github.com/charmbracelet/x/ansi"
	controlruntime "nekocode/runtime"
	"strings"
	"testing"
)

func TestAllTasksViewLabelsSessionAndManagementKeys(t *testing.T) {
	m := openedTaskModel(t)
	m.tasks.detail = nil
	m.tasks.allSessions = true
	m.tasks.items = []controlruntime.SubagentSummary{
		{ID: "A", SessionID: "parent-A", Title: "task A", Status: "completed"},
		{ID: "B", SessionID: "parent-B", Title: "task B", Status: "completed"},
	}
	m.tasks.selected = "A"
	m.tasks.listPage.Total = 2
	view := ansi.Strip(m.taskPanelView(120, 24))
	for _, want := range []string{"全部会话", "[parent-A]", "[parent-B]", "a 范围", "d 删除"} {
		if !strings.Contains(view, want) {
			t.Fatalf("missing %q in task panel: %s", want, view)
		}
	}
}

func TestDependencyTaskStatusesHaveHumanReadableLabels(t *testing.T) {
	for status, want := range map[string]string{"waiting_dependencies": "等待上游", "blocked": "上游未成功"} {
		if got := taskStatus(status); !strings.Contains(got, want) {
			t.Fatalf("status %s = %s", status, got)
		}
	}
}

func TestBackgroundEventsDoNotChangeForegroundProjection(t *testing.T) {
	m := openedTaskModel(t)
	m.closeTasks()
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventRunStarted, RunID: "foreground"})
	m.taskStates = map[string]string{"foreground-child": "running"}
	m.metrics = controlruntime.MetricsSnapshot{TurnPrompt: 7}
	before := ansi.Strip(m.Messages.Render())
	events := []controlruntime.Event{
		{Type: controlruntime.EventCompaction, Payload: controlruntime.CompactionPayload{ID: "background-compact", Status: "started", Trigger: "auto"}},
		{Type: controlruntime.EventMetricsUpdated, Payload: controlruntime.MetricsSnapshot{TurnPrompt: 99}},
		{Type: controlruntime.EventToolStarted, Payload: controlruntime.ToolPayload{CallID: "background-call", SubAgentID: "background-child", ToolName: "shell", Preview: "background-tool"}},
		{Type: controlruntime.EventSubAgentStarted, Payload: controlruntime.SubAgentPayload{ID: "background-child", SessionID: "other-session", Status: "running"}},
	}
	for _, event := range events {
		event.RunID = "task_background-child"
		m.handleRuntimeEvent(event)
	}
	after := ansi.Strip(m.Messages.Render())
	if after != before {
		t.Fatalf("background event changed foreground render:\nbefore=%s\nafter=%s", before, after)
	}
	if m.metrics.TurnPrompt != 7 {
		t.Fatal("background metrics replaced foreground usage")
	}
	if len(m.taskStates) != 1 || m.taskStates["foreground-child"] != "running" {
		t.Fatalf("background lifecycle polluted foreground summary: %+v", m.taskStates)
	}
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventApprovalRequested, RunID: "task_background-child", Payload: controlruntime.ApprovalView{ID: "background-approval", SubAgentID: "background-child", ToolName: "shell"}})
	if m.state != stateConfirming {
		t.Fatal("background approval was hidden")
	}
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventApprovalResolved, RunID: "task_background-child", Payload: controlruntime.ApprovalView{ID: "background-approval", SubAgentID: "background-child"}})
	if m.state != stateProcessing {
		t.Fatal("background approval resolution was hidden")
	}
}

type taskDeleteIdentityRuntime struct {
	taskFakeRuntime
	deletedParent, deletedID string
}

func (r *taskDeleteIdentityRuntime) DeleteSubagent(_ context.Context, parent, id string) error {
	r.deletedParent, r.deletedID = parent, id
	return nil
}

func TestCrossSessionTasksWithSameIDKeepIndependentSelectionAndDeletion(t *testing.T) {
	m := openedTaskModel(t)
	rt := &taskDeleteIdentityRuntime{}
	m.Runtime = rt
	s := m.tasks
	s.detail = nil
	s.allSessions = true
	a := controlruntime.SubagentSummary{ID: "same", SessionID: "parent-A", Title: "first task", Status: "completed"}
	b := controlruntime.SubagentSummary{ID: "same", SessionID: "parent-B", Title: "second task", Status: "completed"}
	s.items = []controlruntime.SubagentSummary{a, b}
	s.selected = s.selectionKey(b)
	m.openSelectedTask()
	if s.detail == nil || s.detail.info.SessionID != "parent-B" || m.taskParentSession() != "parent-B" {
		t.Fatal("same ID opened the wrong parent session")
	}
	if m.subscribeTask() != nil {
		t.Fatal("read-only archive subscribed to ambiguous task-ID events")
	}
	s.savedDetail = s.detail
	s.detail = nil
	s.selected = s.selectionKey(a)
	m.openSelectedTask()
	if s.detail.info.SessionID != "parent-A" {
		t.Fatal("same ID restored another session's saved detail")
	}
	s.detail = nil
	s.selected = s.selectionKey(b)
	m.requestTaskDelete()
	m.ConfirmBar.Respond(true, false)
	if rt.deletedParent != "parent-B" || rt.deletedID != "same" {
		t.Fatalf("deleted wrong identity: %s/%s", rt.deletedParent, rt.deletedID)
	}
	if len(s.items) != 1 || s.items[0].SessionID != "parent-A" {
		t.Fatalf("deletion removed another session's task: %+v", s.items)
	}
}

func TestTaskRowRefreshRetainsDuplicateIDsFromDistinctParents(t *testing.T) {
	old := []controlruntime.SubagentSummary{{ID: "same", SessionID: "parent-A", Status: "running"}, {ID: "same", SessionID: "parent-B", Status: "running"}}
	updated := []controlruntime.SubagentSummary{{ID: "same", SessionID: "parent-A", Status: "completed"}, {ID: "same", SessionID: "parent-B", Status: "failed"}}
	rows := mergeTaskRows(old, updated, false)
	if len(rows) != 2 || rows[0].SessionID != "parent-A" || rows[0].Status != "completed" || rows[1].SessionID != "parent-B" || rows[1].Status != "failed" {
		t.Fatalf("parent task identity collapsed: %+v", rows)
	}
}

func TestTaskDeleteRepairsSelectionAndRejectsPreDeleteRefresh(t *testing.T) {
	for _, all := range []bool{false, true} {
		t.Run(fmt.Sprintf("all_sessions=%v", all), func(t *testing.T) {
			m := openedTaskModel(t)
			m.Runtime = &taskDeleteIdentityRuntime{}
			s := m.tasks
			s.allSessions = all
			s.detail = nil
			a := controlruntime.SubagentSummary{ID: "A", SessionID: "parent", Status: "completed"}
			b := controlruntime.SubagentSummary{ID: "B", SessionID: "parent", Status: "completed"}
			s.items = []controlruntime.SubagentSummary{a, b}
			s.selected = s.selectionKey(b)
			m.openSelectedTask()
			s.savedDetail = s.detail
			s.detail = nil
			oldRequest := s.listRequest
			s.listLoading = true
			m.requestTaskDelete()
			m.ConfirmBar.Respond(true, false)
			if s.selected != s.selectionKey(a) || s.savedDetail != nil {
				t.Errorf("selection/detail not repaired: selected=%q saved=%v", s.selected, s.savedDetail != nil)
			}
			m.handleTaskMessage(taskListMsg{generation: s.generation, request: oldRequest, page: controlruntime.SubagentPage{Items: []controlruntime.SubagentSummary{a, b}, Total: 2}})
			if len(s.items) != 1 || s.items[0].ID != "A" {
				t.Errorf("stale refresh resurrected deleted task: %+v", s.items)
			}
			m.openSelectedTask()
			if s.detail == nil || s.detail.id != "A" {
				t.Error("Enter did not open remaining task immediately")
			}
			s.detail = nil
			s.selected = s.selectionKey(a)
			m.requestTaskDelete()
			m.ConfirmBar.Respond(true, false)
			if len(s.items) != 0 || s.selected != "" {
				t.Error("deleting final task retained a stale selection")
			}
		})
	}
}

func TestTaskDeleteKeepsSelectionChangedWhileConfirming(t *testing.T) {
	m := openedTaskModel(t)
	m.Runtime = &taskDeleteIdentityRuntime{}
	s := m.tasks
	s.detail = nil
	a := controlruntime.SubagentSummary{ID: "A", SessionID: "parent", Status: "completed"}
	b := controlruntime.SubagentSummary{ID: "B", SessionID: "parent", Status: "completed"}
	s.items = []controlruntime.SubagentSummary{a, b}
	s.selected = a.ID
	m.requestTaskDelete()
	s.selected = b.ID
	m.ConfirmBar.Respond(true, false)
	if s.selected != b.ID || len(s.items) != 1 || s.items[0].ID != b.ID {
		t.Fatal("deletion overwrote a newer selection")
	}
}

func TestDeletingLastTaskOnFinalPageMovesBack(t *testing.T) {
	m := openedTaskModel(t)
	m.Runtime = &taskDeleteIdentityRuntime{}
	s := m.tasks
	s.detail = nil
	item := controlruntime.SubagentSummary{ID: "last", SessionID: "parent", Status: "completed"}
	s.items = []controlruntime.SubagentSummary{item}
	s.selected = item.ID
	s.listOffset = 1000
	s.listPage.Total = 1001
	m.requestTaskDelete()
	m.ConfirmBar.Respond(true, false)
	if s.listOffset != 0 || s.listPage.Total != 1000 || s.selected != "" {
		t.Fatalf("deleted final page left invalid pagination: offset=%d total=%d selection=%q", s.listOffset, s.listPage.Total, s.selected)
	}
}

func TestTaskDeleteConfirmationDoesNotAffectReplacedPanel(t *testing.T) {
	m := openedTaskModel(t)
	rt := &taskDeleteIdentityRuntime{}
	m.Runtime = rt
	old := controlruntime.SubagentSummary{ID: "old", SessionID: "old-parent", Status: "completed"}
	m.tasks.detail = nil
	m.tasks.items = []controlruntime.SubagentSummary{old}
	m.tasks.selected = old.ID
	m.requestTaskDelete()
	current := controlruntime.SubagentSummary{ID: "new", SessionID: "new-parent", Status: "completed"}
	next := &taskOverlay{open: true, sessionID: "new-parent", items: []controlruntime.SubagentSummary{current}, selected: current.ID}
	m.tasks = next
	m.ConfirmBar.Respond(true, false)
	if rt.deletedID != "" || next.listDirty || next.listRequest != 0 || next.selected != current.ID {
		t.Fatal("old confirmation deleted history or changed the new panel")
	}
}
