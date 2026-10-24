package tui

import (
	tea "charm.land/bubbletea/v2"
	controlruntime "nekocode/runtime"
	"testing"
)

func TestHistoryRebuildPreservesPendingInteraction(t *testing.T) {
	for _, question := range []bool{false, true} {
		t.Run(map[bool]string{false: "approval", true: "question"}[question], func(t *testing.T) {
			m, err := NewModel(&immediateSessionRuntime{})
			if err != nil {
				t.Fatal(err)
			}
			m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
			m.transitionTo(stateProcessing)
			cmd := m.loadSessionMessages()
			loaded := cmd().(sessionLoadedMsg)
			want := stateConfirming
			if question {
				want = stateQuestioning
				m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventQuestionRequested, Payload: controlruntime.QuestionView{ID: "q", Questions: []controlruntime.QuestionItem{{Question: "choose", Options: []controlruntime.QuestionOption{{Label: "yes"}}}}}})
			} else {
				m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventApprovalRequested, Payload: controlruntime.ApprovalView{ID: "a", ToolName: "shell"}})
			}
			m.Update(tea.WindowSizeMsg{Width: 110, Height: 35})
			retry := m.applySessionLoaded(loaded)
			if retry == nil || m.state != want {
				t.Fatalf("history resize dismissed pending interaction: got state %v want %v", m.state, want)
			}
			m.applySessionLoaded(retry().(sessionLoadedMsg))
			if m.state != want {
				t.Fatal("loaded history dismissed pending interaction")
			}
		})
	}
}
