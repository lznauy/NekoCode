package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	controlruntime "nekocode/runtime"
)

func TestTaskHistoryDoesNotReloadUnchangedOrEmptyRecords(t *testing.T) {
	for _, empty := range []bool{false, true} {
		m := openedTaskModel(t)
		openTestDetail(m)
		d := m.tasks.detail
		d.info.TranscriptSeq = 0 // summary may lag a newer committed history page
		if empty {
			d.page = controlruntime.SubagentHistoryPage{}
		}
		request := d.request
		for i := 0; i < 3; i++ {
			m.handleTaskMessage(taskTickMsg{generation: m.tasks.generation})
		}
		if d.request != request || d.loading {
			t.Fatal("unchanged history repeatedly enters loading state")
		}
		m.closeTasks()
	}
}

func TestHundredTasksProduceOneMainSummary(t *testing.T) {
	m := openedTaskModel(t)
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventRunStarted})
	for i := 0; i < 100; i++ {
		id := fmt.Sprint(i)
		m.applyRuntimeToolEvent(controlruntime.Event{Type: controlruntime.EventToolStarted, Payload: controlruntime.ToolPayload{CallID: id, ToolName: "task"}})
		status := "queued"
		kind := controlruntime.EventSubAgentStarted
		if i < 60 {
			status, kind = "completed", controlruntime.EventSubAgentEnded
		} else if i < 68 {
			status = "running"
		}
		m.handleRuntimeEvent(controlruntime.Event{Type: kind, Payload: controlruntime.SubAgentPayload{ID: id, Status: status}})
		m.applyRuntimeToolEvent(controlruntime.Event{Type: controlruntime.EventToolCompleted, Payload: controlruntime.ToolPayload{CallID: id, ToolName: "task", Output: "individual task output"}})
	}
	view := ansi.Strip(m.Messages.Render())
	for _, want := range []string{"运行中 8", "排队 32", "完成 60", "Ctrl+G"} {
		if !strings.Contains(view, want) {
			t.Fatalf("missing %q in summary: %s", want, view)
		}
	}
	if len(m.Messages.ProcessingBlocks()) != 0 || strings.Contains(view, "individual task output") {
		t.Fatal("successful tasks expanded into main conversation")
	}
	m.applyRuntimeToolEvent(controlruntime.Event{Type: controlruntime.EventToolCompleted, Payload: controlruntime.ToolPayload{CallID: "failed", ToolName: "task", IsError: true, Output: "task could not start"}})
	if blocks := m.Messages.ProcessingBlocks(); len(blocks) != 1 || !blocks[0].IsError || blocks[0].Content != "task could not start" {
		t.Fatal("actionable task failure was hidden")
	}
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventRunStarted})
	if m.taskSummary() != "" {
		t.Fatal("summary leaked into next parent run")
	}
}

func TestPartialTaskDetailsSurviveEmptyParentResponse(t *testing.T) {
	m := openedTaskModel(t)
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventRunStarted})
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventSubAgentStarted, Payload: controlruntime.SubAgentPayload{ID: "partial", Status: "running"}})
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventSubAgentEnded, Payload: controlruntime.SubAgentPayload{ID: "partial", Status: "partial"}})
	m.applyRuntimeToolEvent(controlruntime.Event{Type: controlruntime.EventToolCompleted, Payload: controlruntime.ToolPayload{CallID: "partial", ToolName: "task", Output: "[Subagent returned a partial result]\nstep limit reached"}})
	m.handleDone(doneMsg{})
	view := ansi.Strip(m.Messages.Render())
	if !strings.Contains(view, "子任务未完成") || !strings.Contains(view, "step limit reached") {
		t.Fatalf("partial task details were hidden after completion: %s", view)
	}
}

func TestTaskListRefreshUsesEventsAndLowFrequencyFallback(t *testing.T) {
	m := openedTaskModel(t)
	s := m.tasks
	now := time.Now()
	s.listQueriedAt = now
	if m.shouldRefreshTaskList(now.Add(time.Second)) {
		t.Fatal("idle list still polls every tick")
	}
	if !m.shouldRefreshTaskList(now.Add(taskIdleResyncInterval)) {
		t.Fatal("idle fallback disabled")
	}
	m.state = stateProcessing
	if m.shouldRefreshTaskList(now.Add(time.Second)) || !m.shouldRefreshTaskList(now.Add(taskActiveResyncInterval)) {
		t.Fatal("active fallback frequency incorrect")
	}
	m.observeTaskLifecycle(controlruntime.Event{Type: controlruntime.EventSubAgentUpdated, Payload: controlruntime.SubAgentPayload{ID: "task", Status: "running"}})
	if m.shouldRefreshTaskList(now.Add(taskPollInterval/2)) || !m.shouldRefreshTaskList(now.Add(taskPollInterval)) {
		t.Fatal("event refresh is not coalesced")
	}
	request := s.listRequest
	m.loadTasks()
	if s.listDirty || s.listRequest != request+1 || m.shouldRefreshTaskList(now.Add(time.Hour)) {
		t.Fatal("refresh issued duplicate in-flight queries")
	}
	m.closeTasks()
	if m.shouldRefreshTaskList(now.Add(time.Hour)) {
		t.Fatal("closed overlay queried task list")
	}
}

type taskSnapshotRuntime struct {
	taskFakeRuntime
	snapshot controlruntime.RunSnapshot
}

func (r *taskSnapshotRuntime) LookupRun(controlruntime.RunID) (controlruntime.RunSnapshot, bool) {
	return r.snapshot, true
}

func TestTaskSummaryReconcilesDroppedLifecycleEvents(t *testing.T) {
	r := &taskSnapshotRuntime{snapshot: controlruntime.RunSnapshot{SubAgents: []controlruntime.SubAgentView{{ID: "child", Status: "completed"}}}}
	m, err := NewModel(r)
	if err != nil {
		t.Fatal(err)
	}
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventRunStarted, RunID: "parent"})
	// No child lifecycle events arrived, but the terminal run projection is complete.
	m.observeTaskLifecycle(controlruntime.Event{Type: controlruntime.EventRunDone, RunID: "parent"})
	if got := m.taskSummary(); got != "子任务 · 完成 1" {
		t.Fatalf("summary = %q", got)
	}
}

func TestTaskResumeRefreshesOpenListWithoutPolling(t *testing.T) {
	m := openedTaskModel(t)
	s := m.tasks
	s.listQueriedAt = time.Now()
	request := s.listRequest
	cmd := m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventSubAgentStarted, Payload: controlruntime.SubAgentPayload{ID: s.selected, Status: "running"}})
	if cmd == nil || !s.listLoading || s.listRequest != request+1 {
		t.Fatal("resumed task waits for polling to refresh the open list")
	}
	// A lifecycle change while the query is in flight must cause a follow-up
	// query: the current response may have been captured before the change.
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventSubAgentEnded, Payload: controlruntime.SubAgentPayload{ID: s.selected, Status: "completed"}})
	_, next := m.handleTaskMessage(taskListMsg{generation: s.generation, request: s.listRequest, page: taskTestPage()})
	if next == nil || !s.listLoading || s.listRequest != request+2 {
		t.Fatal("in-flight response swallowed a newer lifecycle change")
	}
}

func TestTaskListReconcilesWhenLifecycleEventsAreMissing(t *testing.T) {
	for _, kind := range []controlruntime.EventType{
		controlruntime.EventRunDone, controlruntime.EventRunFailed, controlruntime.EventRunCancelled,
		controlruntime.EventSubAgentUpdated,
	} {
		t.Run(string(kind), func(t *testing.T) {
			m := openedTaskModel(t)
			s := m.tasks
			s.listQueriedAt = time.Now()
			m.taskStates = map[string]string{s.selected: "completed"}
			request := s.listRequest
			// The resumed task's Started event was dropped. Status guards may
			// protect the summary, but must not prevent querying the current list.
			cmd := m.handleRuntimeEvent(controlruntime.Event{Type: kind, Payload: controlruntime.SubAgentPayload{ID: s.selected, Status: "running"}})
			if cmd == nil || !s.listLoading || s.listRequest != request+1 {
				t.Fatal("missing lifecycle event leaves the open list stale")
			}
		})
	}
}
