package tui

import (
	"github.com/charmbracelet/x/ansi"
	"nekocode/interaction/tui/components/message"
	controlruntime "nekocode/runtime"
	"strings"
	"testing"
)

func TestRunTerminalPreservesLocalDeletionConfirmation(t *testing.T) {
	for _, eventType := range []controlruntime.EventType{controlruntime.EventRunDone, controlruntime.EventRunFailed, controlruntime.EventRunCancelled} {
		t.Run(string(eventType), func(t *testing.T) {
			m, err := NewModel(&statusFakeBot{})
			if err != nil {
				t.Fatal(err)
			}
			m.preConfirmState = stateReady
			m.state = stateConfirming
			m.ConfirmBar.SetDestructive("delete", "description", "delete", func(bool) {})
			m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventRunStarted})
			m.enqueueRuntimePrompt(runtimePrompt{approval: &controlruntime.ApprovalView{ID: "queued", ToolName: "shell"}})
			m.handleRuntimeEvent(controlruntime.Event{Type: eventType, Payload: controlruntime.RunResult{}})
			if m.state != stateConfirming || !m.ConfirmBar.IsDestructive() || m.preConfirmState != stateReady {
				t.Fatal("run completion erased local deletion confirmation or restored wrong return state")
			}
			if len(m.runtimePrompts) != 0 || m.runtimePromptActive {
				t.Fatal("run completion retained runtime prompts")
			}
		})
	}
}

func TestUnrelatedSessionDeletionKeepsCurrentHistory(t *testing.T) {
	b := &completedTurnWindowRuntime{history: []controlruntime.DisplayMessage{{Role: "user", Content: "old"}, {Role: "assistant", Content: "answer"}}}
	m, err := NewModel(b)
	if err != nil {
		t.Fatal(err)
	}
	m.applySessionLoaded(sessionLoadResult(t, m.loadSessionMessages()))
	m.retainCompletedTurn(message.ChatMessage{Role: "assistant", Content: "unsaved current reply"})
	m.refreshTelemetry = &controlruntime.MetricsSnapshot{TurnTotal: 42}
	m.sessionHistoryPage = 2
	generation := m.sessionLoadGeneration
	cmd := m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventSessionChanged, Payload: controlruntime.SessionPayload{ID: "current", Reason: "delete", ChangedID: "other", CurrentChanged: false}})
	if cmd != nil || len(m.transientTurns) != 1 || m.refreshTelemetry == nil || m.sessionHistoryPage != 2 || generation != m.sessionLoadGeneration {
		t.Fatal("deleting unrelated session reset current history")
	}
}

func TestSessionSwitchDiscardsDeferredOldRunState(t *testing.T) {
	b := &completedTurnWindowRuntime{history: []controlruntime.DisplayMessage{{Role: "user", Content: "old session question"}}}
	m, err := NewModel(b)
	if err != nil {
		t.Fatal(err)
	}
	m.applySessionLoaded(sessionLoadResult(t, m.loadSessionMessages()))
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventRunStarted})
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventInputAccepted, Payload: controlruntime.MessagePayload{Content: "old session question"}})
	stale := m.loadSessionWindow(0, 0)
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventRunDone, Payload: controlruntime.RunResult{Output: "PRIVATE OLD SESSION ANSWER"}})
	m.sessionLoadNotices = []message.ChatMessage{{Role: "system", Content: "old session notice"}}
	m.sessionPendingChat = &pendingSessionChat{text: "old session queued input"}
	b.history = []controlruntime.DisplayMessage{{Role: "user", Content: "new session question"}}
	cmd := m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventSessionChanged, Payload: controlruntime.SessionPayload{ID: "new", Reason: "resume", CurrentChanged: true}})
	next := m.applySessionLoaded(sessionLoadResult(t, cmd))
	if m.sessionLoading {
		m.applySessionLoaded(sessionLoadResult(t, next))
	}
	m.applySessionLoaded(sessionLoadResult(t, stale))
	if m.sessionLoadDone != nil || m.sessionPendingChat != nil || len(b.submitted) != 0 || m.state != stateReady {
		t.Fatal("old run state or queued input crossed session boundary")
	}
	for _, item := range m.Messages.Items() {
		rendered := ansi.Strip(item.Render(80))
		for _, old := range []string{"PRIVATE OLD SESSION ANSWER", "old session notice"} {
			if strings.Contains(rendered, old) {
				t.Fatalf("old session content leaked: %s", old)
			}
		}
	}
}

func TestSessionChangeLegacyAndCurrentDeletionStillReload(t *testing.T) {
	for _, payload := range []controlruntime.SessionPayload{{ID: "legacy"}, {ID: "new", Reason: "delete", ChangedID: "old", CurrentChanged: true}, {Reason: "delete"}} {
		m, err := NewModel(&completedTurnWindowRuntime{})
		if err != nil {
			t.Fatal(err)
		}
		cmd := m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventSessionChanged, Payload: payload})
		if cmd == nil || !m.sessionLoading {
			t.Fatalf("session change was ignored: %+v", payload)
		}
	}
}

func TestSessionSwitchDuringLiveCommandStillWaitsForCompletion(t *testing.T) {
	m, err := NewModel(&completedTurnWindowRuntime{})
	if err != nil {
		t.Fatal(err)
	}
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventRunStarted})
	cmd := m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventSessionChanged, Payload: controlruntime.SessionPayload{ID: "new", Reason: "resume", CurrentChanged: true}})
	m.applySessionLoaded(sessionLoadResult(t, cmd))
	if m.state != stateProcessing || !m.sessionLoadWaitForRun {
		t.Fatal("session switch finished a live command")
	}
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventRunDone, Payload: controlruntime.RunResult{}})
	if m.state != stateReady {
		t.Fatal("command completion did not restore ready state")
	}
}

func TestRunTerminalStillClearsRuntimePrompt(t *testing.T) {
	m, err := NewModel(&statusFakeBot{})
	if err != nil {
		t.Fatal(err)
	}
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventRunStarted})
	m.enqueueRuntimePrompt(runtimePrompt{approval: &controlruntime.ApprovalView{ID: "old", ToolName: "shell"}})
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventRunDone, Payload: controlruntime.RunResult{}})
	if m.state != stateReady || m.runtimePromptActive || len(m.runtimePrompts) != 0 {
		t.Fatal("terminal preserved runtime prompt instead of just local confirmation")
	}
}
