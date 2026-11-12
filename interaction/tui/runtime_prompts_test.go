package tui

import (
	tea "charm.land/bubbletea/v2"
	controlruntime "nekocode/runtime"
	"strings"
	"testing"
)

func TestRuntimeApprovalsQueueAndResolveByID(t *testing.T) {
	m, err := NewModel(&statusFakeBot{})
	if err != nil {
		t.Fatal(err)
	}
	m.state = stateProcessing
	for _, id := range []string{"first", "second"} {
		m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventApprovalRequested, Payload: controlruntime.ApprovalView{ID: id, ToolName: id}})
	}
	if !strings.Contains(m.ConfirmBar.View(80, 24), "first") {
		t.Fatal("later approval replaced active prompt")
	}
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventApprovalResolved, Payload: controlruntime.ApprovalView{ID: "first"}})
	if m.state != stateConfirming || !strings.Contains(m.ConfirmBar.View(80, 24), "second") {
		t.Fatal("resolving first lost pending second approval")
	}
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventApprovalResolved, Payload: controlruntime.ApprovalView{ID: "first"}})
	if m.state != stateConfirming || !strings.Contains(m.ConfirmBar.View(80, 24), "second") {
		t.Fatal("stale resolution cleared a different approval")
	}
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventApprovalResolved, Payload: controlruntime.ApprovalView{ID: "second"}})
	if m.state != stateProcessing {
		t.Fatalf("restored nested modal instead of processing: %v", m.state)
	}
}

func TestRuntimeApprovalAndQuestionShareQueue(t *testing.T) {
	m, err := NewModel(&statusFakeBot{})
	if err != nil {
		t.Fatal(err)
	}
	m.state = stateReady
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventQuestionRequested, Payload: controlruntime.QuestionView{ID: "same", Questions: []controlruntime.QuestionItem{{Question: "first question"}}}})
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventApprovalRequested, Payload: controlruntime.ApprovalView{ID: "same", ToolName: "next approval"}})
	if m.state != stateQuestioning {
		t.Fatal("approval replaced pending question")
	}
	m.handleQuestionKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
	if m.state != stateConfirming || !strings.Contains(m.ConfirmBar.View(80, 24), "next approval") {
		t.Fatal("answering question did not advance approval queue")
	}
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventQuestionResolved, Payload: controlruntime.QuestionView{ID: "same"}})
	if m.state != stateConfirming {
		t.Fatal("question resolution cleared approval with same ID")
	}
	m.handleConfirmKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
	if m.state != stateReady {
		t.Fatalf("queue did not restore ready state: %v", m.state)
	}
}

func TestRuntimePromptQueuedResolutionAndDuplicateRequest(t *testing.T) {
	m, err := NewModel(&statusFakeBot{})
	if err != nil {
		t.Fatal(err)
	}
	m.state = stateReady
	request := func(id string) {
		m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventApprovalRequested, Payload: controlruntime.ApprovalView{ID: id, ToolName: id}})
	}
	request("a")
	m.ConfirmBar.Move(1)
	selected := m.ConfirmBar.Selected()
	request("a")
	request("b")
	if len(m.runtimePrompts) != 2 || m.ConfirmBar.Selected() != selected {
		t.Fatal("duplicate reset selection or grew queue")
	}
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventApprovalResolved, Payload: controlruntime.ApprovalView{ID: "b"}})
	if len(m.runtimePrompts) != 1 || !strings.Contains(m.ConfirmBar.View(80, 24), "a") {
		t.Fatal("queued resolution changed active prompt")
	}
	m.handleConfirmKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
	if m.state != stateReady || len(m.runtimePrompts) != 0 || m.runtimePromptActive {
		t.Fatal("stale queued approval reappeared")
	}
}

func TestRuntimePromptWaitsForLocalConfirmation(t *testing.T) {
	m, err := NewModel(&statusFakeBot{})
	if err != nil {
		t.Fatal(err)
	}
	m.preConfirmState = stateReady
	m.state = stateConfirming
	m.ConfirmBar.SetDestructive("local deletion", "description", "delete", func(bool) {})
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventApprovalRequested, Payload: controlruntime.ApprovalView{ID: "a", ToolName: "runtime approval"}})
	if !m.ConfirmBar.IsDestructive() {
		t.Fatal("runtime request replaced local deletion confirmation")
	}
	m.handleConfirmKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
	if m.state != stateConfirming || m.ConfirmBar.IsDestructive() || !strings.Contains(m.ConfirmBar.View(80, 24), "runtime approval") {
		t.Fatal("local confirmation did not advance queued runtime request")
	}
	m.transitionTo(stateReady)
	if len(m.runtimePrompts) != 0 || m.runtimePromptActive {
		t.Fatal("completed run retained pending modal state")
	}
}

func TestRunStartedPreservesOpenPrompt(t *testing.T) {
	for _, kind := range []string{"local", "approval", "question"} {
		t.Run(kind, func(t *testing.T) {
			m, err := NewModel(&statusFakeBot{})
			if err != nil {
				t.Fatal(err)
			}
			m.state = stateReady
			switch kind {
			case "local":
				m.preConfirmState = stateReady
				m.state = stateConfirming
				m.ConfirmBar.SetDestructive("delete", "desc", "delete", func(bool) {})
			case "approval":
				m.enqueueRuntimePrompt(runtimePrompt{approval: &controlruntime.ApprovalView{ID: "a", ToolName: "first"}})
			case "question":
				m.enqueueRuntimePrompt(runtimePrompt{question: &controlruntime.QuestionView{ID: "q", Questions: []controlruntime.QuestionItem{{Question: "first"}}}})
			}
			before := m.state
			m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventRunStarted})
			if m.state != before || m.preConfirmState != stateProcessing {
				t.Fatalf("run hid prompt: state=%v return=%v", m.state, m.preConfirmState)
			}
			m.enqueueRuntimePrompt(runtimePrompt{approval: &controlruntime.ApprovalView{ID: "b", ToolName: "second"}})
			if kind == "local" && !m.ConfirmBar.IsDestructive() {
				t.Fatal("run approval replaced local deletion")
			}
			if kind == "question" {
				m.handleQuestionKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
			} else {
				m.handleConfirmKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
			}
			if m.state != stateConfirming || !strings.Contains(m.ConfirmBar.View(80, 24), "second") {
				t.Fatal("queued approval lost after run started")
			}
			m.handleConfirmKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
			if m.state != stateProcessing {
				t.Fatal("completion restored wrong run state")
			}
		})
	}
}

func TestForegroundCompletionKeepsBackgroundApproval(t *testing.T) {
	m, err := NewModel(&statusFakeBot{})
	if err != nil {
		t.Fatal(err)
	}
	m.state = stateProcessing
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventApprovalRequested, Payload: controlruntime.ApprovalView{ID: "background", SubAgentID: "worker", ToolName: "shell"}})
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventRunDone, Payload: controlruntime.RunResult{Output: "dispatched"}})
	if m.state != stateConfirming || len(m.runtimePrompts) != 1 || !strings.Contains(m.ConfirmBar.View(80, 24), "shell") {
		t.Fatal("foreground completion dismissed a background approval")
	}
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventApprovalResolved, Payload: controlruntime.ApprovalView{ID: "background"}})
	if m.state != stateReady {
		t.Fatalf("background approval restored completed foreground run: %v", m.state)
	}
}

func TestForegroundTerminalPreservesWorkerQuestionDraft(t *testing.T) {
	for _, terminal := range []controlruntime.EventType{controlruntime.EventRunDone, controlruntime.EventRunFailed, controlruntime.EventRunCancelled} {
		t.Run(string(terminal), func(t *testing.T) {
			m, err := NewModel(&statusFakeBot{})
			if err != nil {
				t.Fatal(err)
			}
			m.state = stateProcessing
			m.enqueueRuntimePrompt(runtimePrompt{question: &controlruntime.QuestionView{
				ID: "worker-question", SubAgentID: "worker", Questions: []controlruntime.QuestionItem{{Question: "Your answer?", Custom: true}},
			}})
			m.QuestionBar.Type("draft-answer")
			if !strings.Contains(m.QuestionBar.View(100, 40), "draft-answer") {
				t.Fatal("draft setup failed")
			}
			m.handleRuntimeEvent(controlruntime.Event{Type: terminal, Payload: controlruntime.RunResult{}})
			if m.state != stateQuestioning || !m.runtimePromptActive || !strings.Contains(m.QuestionBar.View(100, 40), "draft-answer") {
				t.Fatal("foreground completion erased worker answer")
			}
			m.resolveRuntimePrompt("worker-question", false)
			if m.state != stateReady {
				t.Fatal("worker answer restored finished foreground run")
			}
		})
	}
}
func TestForegroundTerminalPreservesWorkerApprovalSelection(t *testing.T) {
	m, err := NewModel(&statusFakeBot{})
	if err != nil {
		t.Fatal(err)
	}
	m.state = stateProcessing
	m.enqueueRuntimePrompt(runtimePrompt{approval: &controlruntime.ApprovalView{ID: "worker-approval", SubAgentID: "worker", ToolName: "shell"}})
	m.ConfirmBar.Move(1)
	selected := m.ConfirmBar.Selected()
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventRunDone, Payload: controlruntime.RunResult{}})
	if m.state != stateConfirming || !m.runtimePromptActive || m.ConfirmBar.Selected() != selected {
		t.Fatal("foreground completion reset worker approval")
	}
	m.resolveRuntimePrompt("worker-approval", true)
	if m.state != stateReady {
		t.Fatal("approval restored finished foreground run")
	}
}
