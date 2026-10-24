package tui

import (
	tea "charm.land/bubbletea/v2"
	"errors"
	"github.com/charmbracelet/x/ansi"
	controlruntime "nekocode/runtime"
	"strings"
	"testing"
)

func TestDeferredOldCompletionCannotFinishNewRun(t *testing.T) {
	m, err := NewModel(&windowSessionRuntime{})
	if err != nil {
		t.Fatal(err)
	}
	cmd := m.loadSessionMessages()
	m.applySessionLoaded(cmd().(sessionLoadedMsg))
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventRunStarted})
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventInputAccepted, Payload: controlruntime.MessagePayload{Content: "first input"}})
	cmd = m.loadSessionWindow(0, 0)
	stale := cmd().(sessionLoadedMsg)
	m.handleDone(doneMsg{content: "first answer"})
	reload := m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventRunStarted})
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventInputAccepted, Payload: controlruntime.MessagePayload{Content: "second input"}})
	m.Messages.ProcessStreamText("second run is live")
	m.applySessionLoaded(stale)
	if m.state != stateProcessing || !strings.Contains(m.Messages.AccumulatedText(), "second run is live") || !m.sessionLoadWaitForRun {
		t.Fatal("deferred completion of old run ended or cleared the new run")
	}
	for _, cmd := range reload().(tea.BatchMsg) {
		if loaded, ok := cmd().(sessionLoadedMsg); ok {
			m.applySessionLoaded(loaded)
		}
	}
	if m.state != stateProcessing || !strings.Contains(m.Messages.AccumulatedText(), "second run is live") {
		t.Fatal("replacement history did not preserve the new run")
	}
}

func TestDeferredFailureRemainsVisibleWhenNewRunStarts(t *testing.T) {
	m, err := NewModel(&windowSessionRuntime{})
	if err != nil {
		t.Fatal(err)
	}
	m.transitionTo(stateProcessing)
	m.loadSessionWindow(0, 0)
	m.handleDone(doneMsg{err: errors.New("first run failed")})
	reload := m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventRunStarted})
	for _, cmd := range reload().(tea.BatchMsg) {
		if loaded, ok := cmd().(sessionLoadedMsg); ok {
			m.applySessionLoaded(loaded)
		}
	}
	found := false
	for _, item := range m.Messages.Items() {
		if strings.Contains(ansi.Strip(item.Render(100)), "first run failed") {
			found = true
		}
	}
	if !found {
		t.Fatal("new run discarded the previous deferred failure notification")
	}
}
