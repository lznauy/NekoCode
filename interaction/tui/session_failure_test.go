package tui

import (
	tea "charm.land/bubbletea/v2"
	"errors"
	controlruntime "nekocode/runtime"
	"strings"
	"testing"
)

func TestFailedHistoryLoadDoesNotFinishLiveRun(t *testing.T) {
	m, err := NewModel(&immediateSessionRuntime{})
	if err != nil {
		t.Fatal(err)
	}
	m.transitionTo(stateProcessing)
	m.loadSessionMessages()
	m.Messages.ProcessStreamText("still running")
	m.applySessionLoaded(sessionLoadedMsg{generation: m.sessionLoadGeneration, err: errors.New("disk unavailable")})
	if m.state != stateProcessing || !m.Messages.Processing || !strings.Contains(m.Messages.AccumulatedText(), "still running") {
		t.Fatal("history read failure finished live run and discarded its output")
	}
	m.handleDone(doneMsg{content: "finished"})
	if m.state != stateReady {
		t.Fatal("real completion did not release input")
	}
}

func TestHistoryLoadRestoresLiveRunPhase(t *testing.T) {
	m, err := NewModel(&immediateSessionRuntime{})
	if err != nil {
		t.Fatal(err)
	}
	m.transitionTo(stateProcessing)
	m.setPhase("Generating answer")
	cmd := m.loadSessionMessages()
	m.applySessionLoaded(cmd().(sessionLoadedMsg))
	if m.processingPhase != "Generating answer" {
		t.Fatalf("live run stuck in history phase: %q", m.processingPhase)
	}
}

func TestControlRunCompletionKeepsSystemResponse(t *testing.T) {
	m, err := NewModel(&windowSessionRuntime{})
	if err != nil {
		t.Fatal(err)
	}
	cmd := m.loadSessionMessages()
	m.applySessionLoaded(cmd().(sessionLoadedMsg))
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventRunStarted})
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventSystemMessage, Payload: controlruntime.MessagePayload{Content: "command response must stay visible"}})
	m.handleDone(doneMsg{})
	if m.sessionLoading {
		t.Fatal("control-only completion refreshes history and discards its non-persistent response")
	}
}

func TestHistoryLoadKeepsBackgroundSystemNotice(t *testing.T) {
	m, err := NewModel(&immediateSessionRuntime{})
	if err != nil {
		t.Fatal(err)
	}
	cmd := m.loadSessionMessages()
	loaded := cmd().(sessionLoadedMsg)
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventSystemMessage, Payload: controlruntime.MessagePayload{Content: "background notification"}})
	m.applySessionLoaded(loaded)
	found := false
	for _, item := range m.Messages.Items() {
		if strings.Contains(item.Render(100), "background notification") {
			found = true
		}
	}
	if !found {
		t.Fatal("asynchronous history replacement discarded a newly delivered background notification")
	}
}

func TestHistoryNoticeSurvivesDeferredCompletionRefresh(t *testing.T) {
	m, err := NewModel(&windowSessionRuntime{})
	if err != nil {
		t.Fatal(err)
	}
	cmd := m.loadSessionMessages()
	m.applySessionLoaded(cmd().(sessionLoadedMsg))
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventRunStarted})
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventInputAccepted, Payload: controlruntime.MessagePayload{Content: "input"}})
	cmd = m.loadSessionWindow(0, 0)
	loaded := cmd().(sessionLoadedMsg)
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventSystemMessage, Payload: controlruntime.MessagePayload{Content: "keep this notice"}})
	m.handleDone(doneMsg{})
	followup := m.applySessionLoaded(loaded)
	if !m.sessionLoading || followup == nil {
		t.Fatal("test did not exercise deferred completion refresh")
	}
	for _, command := range followup().(tea.BatchMsg) {
		if next, ok := command().(sessionLoadedMsg); ok {
			m.applySessionLoaded(next)
		}
	}
	found := false
	for _, item := range m.Messages.Items() {
		if strings.Contains(item.Render(100), "keep this notice") {
			found = true
		}
	}
	if !found {
		t.Fatal("deferred completion refresh discarded the newly delivered notice")
	}
}
