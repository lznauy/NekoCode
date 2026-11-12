package tui

import (
	controlruntime "nekocode/runtime"
	"strings"
	"testing"
)

type queuedPromptRuntime struct {
	taskFakeRuntime
	prompt string
}

func (r *queuedPromptRuntime) BackgroundTaskPrompt(string, string) (string, error) {
	return r.prompt, nil
}

func TestQueuedTaskDetailLoadsFullPromptBeforeExecution(t *testing.T) {
	for _, resumed := range []bool{false, true} {
		t.Run(map[bool]string{false: "new", true: "resume"}[resumed], func(t *testing.T) {
			m := openedTaskModel(t)
			runtime := &queuedPromptRuntime{prompt: "FULL_PROMPT_START\n" + strings.Repeat("long instruction\n", 100) + "FULL_PROMPT_END"}
			m.Runtime = runtime
			item := controlruntime.SubagentSummary{ID: "queued", Profile: "executor", Status: "queued", Prompt: "short preview", Recorded: resumed}
			if resumed {
				item.LatestAttempt = &controlruntime.SubagentAttemptSummary{ID: "old-attempt", Seq: 1, Status: "completed", Prompt: "old instruction"}
			}
			m.tasks.items = []controlruntime.SubagentSummary{item}
			m.tasks.selected = item.ID
			m.openSelectedTask()
			d := m.tasks.detail
			if d.attempt != nil || d.detailStatus() != "queued" {
				t.Fatal("pending task displayed previous attempt")
			}
			command := m.loadTaskHistory(0)
			if command == nil {
				t.Fatal("queued prompt was not requested")
			}
			if resumed {
				request := d.request
				m.handleTaskMessage(taskEventMsg{generation: m.tasks.generation, request: m.tasks.streamRequest, id: d.id, ok: true, event: controlruntime.Event{Type: controlruntime.EventSubAgentStarted, Payload: controlruntime.SubAgentPayload{ID: d.id, AttemptID: "old-attempt", AttemptSeq: 1, Status: "running", Prompt: "old instruction"}}})
				if d.attempt != nil || d.request != request {
					t.Fatal("replayed old start replaced the queued prompt request")
				}
			}
			m.handleTaskMessage(command())
			s := m.tasks
			m.handleTaskMessage(taskListMsg{generation: s.generation, request: s.listRequest, page: controlruntime.SubagentPage{SessionID: s.sessionID, Items: []controlruntime.SubagentSummary{item}, Total: 1}})
			if d.attempt != nil || d.detailStatus() != "queued" {
				t.Fatal("list polling restored the previous completed attempt")
			}
			view := m.taskPanelView(90, 24)
			// The full content must be in the scrollable detail, beyond list-preview limits.
			if !d.fullPromptLoaded || d.fullPrompt != runtime.prompt || !strings.Contains(taskScrolledContent(d), "FULL_PROMPT_END") {
				t.Fatalf("full queued prompt missing: %s", view)
			}
			oldRequest := d.request
			m.handleTaskMessage(taskEventMsg{generation: s.generation, request: s.streamRequest, id: d.id, ok: true, event: controlruntime.Event{Type: controlruntime.EventSubAgentStarted, Payload: controlruntime.SubAgentPayload{ID: d.id, AttemptID: "new-attempt", AttemptSeq: 2, Status: "running", Prompt: "current instruction"}}})
			if d.attempt == nil || d.attempt.ID != "new-attempt" || d.detailStatus() != "running" {
				t.Fatal("queued task did not enter its new execution")
			}
			m.handleTaskMessage(taskPromptMsg{generation: s.generation, request: oldRequest, id: d.id, prompt: "stale queued response"})
			if d.fullPrompt == "stale queued response" {
				t.Fatal("late queued response overwrote running task")
			}

		})
	}
}
