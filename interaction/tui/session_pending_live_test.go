package tui

import (
	"context"
	"errors"
	controlruntime "nekocode/runtime"
	"testing"
)

type busyWindowRuntime struct {
	windowSessionRuntime
	busy     bool
	attempts int
}

func (b *busyWindowRuntime) StartRun(ctx context.Context, input controlruntime.Input) (controlruntime.RunID, error) {
	b.attempts++
	if b.busy {
		return "", errors.New("another run is still active")
	}
	return b.tickFakeBot.StartRun(ctx, input)
}

func TestPendingChatWaitsForRunThatStartsDuringHistoryLoad(t *testing.T) {
	b := &busyWindowRuntime{}
	m, err := NewModel(b)
	if err != nil {
		t.Fatal(err)
	}
	cmd := m.loadSessionMessages()
	m.applySessionLoaded(sessionLoadResult(t, cmd))
	cmd = m.requestHistoryPage(true)
	m.applySessionLoaded(sessionLoadResult(t, cmd))
	cmd = m.startChat("queued input", nil)
	loaded := cmd().(sessionLoadedMsg)
	b.busy = true
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventRunStarted})
	m.applySessionLoaded(loaded)
	if b.attempts != 0 || m.sessionPendingChat == nil || m.state != stateProcessing || !m.Messages.Processing {
		t.Fatal("history completion submitted queued input into a live run and cleared its display")
	}
	b.busy = false
	m.handleDone(doneMsg{})
	if b.attempts != 1 || len(b.submitted) != 1 || m.sessionPendingChat != nil {
		t.Fatal("queued input was not resumed after the real completion")
	}
}

func TestFailedHistoryLoadDoesNotSubmitDeferredChat(t *testing.T) {
	b := &busyWindowRuntime{}
	m, err := NewModel(b)
	if err != nil {
		t.Fatal(err)
	}
	cmd := m.loadSessionMessages()
	m.applySessionLoaded(sessionLoadResult(t, cmd))
	cmd = m.requestHistoryPage(true)
	m.applySessionLoaded(sessionLoadResult(t, cmd))
	m.startChat("queued input", nil)
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventRunStarted})
	m.handleDone(doneMsg{})
	m.applySessionLoaded(sessionLoadedMsg{generation: m.sessionLoadGeneration, err: errors.New("history unavailable")})
	if b.attempts != 0 || m.sessionPendingChat != nil {
		t.Fatal("failed history load submitted deferred input or retained it for a later run")
	}
}
