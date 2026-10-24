package tui

import (
	"charm.land/bubbles/v2/spinner"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"nekocode/interaction/tui/components/block"
	controlruntime "nekocode/runtime"
)

type delayedSessionRuntime struct {
	tickFakeBot
	entered chan struct{}
	release chan struct{}
}

func (b *delayedSessionRuntime) SessionMessages() []controlruntime.DisplayMessage {
	close(b.entered)
	<-b.release
	return []controlruntime.DisplayMessage{{Role: "user", Content: "restored question"}, {Role: "assistant", Content: "restored answer"}}
}

func TestSessionChangedDoesNotBlockEventLoop(t *testing.T) {
	b := &delayedSessionRuntime{entered: make(chan struct{}), release: make(chan struct{})}
	defer close(b.release)
	m, err := NewModel(b)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan tea.Cmd, 1)
	go func() { done <- m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventSessionChanged}) }()
	select {
	case cmd := <-done:
		if cmd == nil {
			t.Fatal("session load must schedule background work")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("session change blocked event loop while reading history")
	}
}

func TestSessionLoadKeepsSpinnerAliveAndDefersCompletion(t *testing.T) {
	b := &delayedSessionRuntime{entered: make(chan struct{}), release: make(chan struct{})}
	m, err := NewModel(b)
	if err != nil {
		t.Fatal(err)
	}
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	cmd := m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventSessionChanged})
	result := make(chan tea.Msg, 1)
	go func() { result <- cmd() }()
	select {
	case <-b.entered:
	case <-time.After(time.Second):
		close(b.release)
		t.Fatal("background load did not start")
	}
	before := m.Spinner.View()
	m.handleSpinnerTick(spinner.TickMsg{})
	if before == m.Spinner.View() {
		close(b.release)
		t.Fatal("spinner did not advance while history blocked")
	}
	m.handleDone(doneMsg{})
	if !m.sessionLoading || m.sessionLoadDone == nil || m.state != stateProcessing {
		close(b.release)
		t.Fatal("completion discarded loading state")
	}
	close(b.release)
	var loaded tea.Msg
	select {
	case loaded = <-result:
	case <-time.After(time.Second):
		t.Fatal("history never finished")
	}
	m.Update(loaded)
	if m.sessionLoading || m.sessionLoadDone != nil || m.state != stateReady {
		t.Fatal("loading/completion state not settled")
	}
	if m.Messages.Len() != 2 || !strings.Contains(ansi.Strip(m.Messages.Render()), "restored answer") {
		t.Fatalf("restored history lost: items=%d render=%q", m.Messages.Len(), ansi.Strip(m.Messages.Render()))
	}
}

type immediateSessionRuntime struct{ tickFakeBot }

func (*immediateSessionRuntime) SessionMessages() []controlruntime.DisplayMessage {
	return []controlruntime.DisplayMessage{{Role: "user", Content: "latest history"}}
}

func TestStaleSessionLoadCannotReplaceNewLoad(t *testing.T) {
	m, err := NewModel(&immediateSessionRuntime{})
	if err != nil {
		t.Fatal(err)
	}
	first := m.loadSessionMessages()
	second := m.loadSessionMessages()
	firstResult := first().(sessionLoadedMsg)
	m.applySessionLoaded(firstResult)
	if !m.sessionLoading {
		t.Fatal("stale result ended current load")
	}
	secondResult := second().(sessionLoadedMsg)
	m.applySessionLoaded(secondResult)
	if m.sessionLoading || m.Messages.Len() != 1 || m.state != stateReady {
		t.Fatal("newest load not applied")
	}
}

func TestResizedSessionLoadRebuildsInBackground(t *testing.T) {
	m, err := NewModel(&immediateSessionRuntime{})
	if err != nil {
		t.Fatal(err)
	}
	cmd := m.loadSessionMessages()
	result := cmd().(sessionLoadedMsg)
	m.Update(tea.WindowSizeMsg{Width: 110, Height: 35})
	rebuild := m.applySessionLoaded(result)
	if rebuild == nil || !m.sessionLoading {
		t.Fatal("resize must rebuild off event loop")
	}
	m.applySessionLoaded(rebuild().(sessionLoadedMsg))
	if m.sessionLoading || m.Messages.Width() != 110 || m.Messages.Height() <= 0 || m.state != stateReady {
		t.Fatal("history restored at wrong size")
	}
}

func TestSessionLoadWaitsForActualRunCompletion(t *testing.T) {
	m, err := NewModel(&immediateSessionRuntime{})
	if err != nil {
		t.Fatal(err)
	}
	m.transitionTo(stateProcessing)
	cmd := m.loadSessionMessages()
	m.applySessionLoaded(cmd().(sessionLoadedMsg))
	if m.sessionLoading || m.state != stateProcessing {
		t.Fatal("load completion finished a live run")
	}
	m.handleDone(doneMsg{})
	if m.state != stateReady {
		t.Fatal("actual run completion did not release input")
	}
}

func TestSessionLoadPreservesInFlightStreamAndTools(t *testing.T) {
	m, err := NewModel(&immediateSessionRuntime{})
	if err != nil {
		t.Fatal(err)
	}
	m.transitionTo(stateProcessing)
	cmd := m.loadSessionMessages()
	loaded := cmd().(sessionLoadedMsg)
	m.Messages.ProcessToolBlock(block.ContentBlock{Type: block.BlockTool, ToolName: "read", Content: "pending tool"})
	m.Messages.ProcessStreamText("before refresh")
	m.applySessionLoaded(loaded)
	if !m.Messages.Processing {
		t.Fatal("restoring history removed the live processing item")
	}
	m.Messages.ProcessStreamText(" after refresh")
	if got := m.Messages.AccumulatedText(); !strings.Contains(got, "before refresh") || !strings.Contains(got, "after refresh") {
		t.Fatalf("lost live deltas: %q", got)
	}
	blocks := m.Messages.ProcessingBlocks()
	found := false
	for _, b := range blocks {
		if b.ToolName == "read" {
			found = true
		}
	}
	if !found {
		t.Fatal("restoring history lost the pending tool")
	}
}
