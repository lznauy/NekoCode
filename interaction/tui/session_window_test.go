package tui

import (
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"fmt"
	"nekocode/interaction/tui/components/message"
	controlruntime "nekocode/runtime"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestHistoryPageSchedulesSpinnerFromReady(t *testing.T) {
	m, err := NewModel(&windowSessionRuntime{})
	if err != nil {
		t.Fatal(err)
	}
	m.applySessionLoaded(m.loadSessionMessages()().(sessionLoadedMsg))
	cmd := m.requestHistoryPage(true)
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatal("history page must schedule both loading and the spinner")
	}
	var tick spinner.TickMsg
	var loaded sessionLoadedMsg
	hasTick, hasLoad := false, false
	for _, command := range batch {
		switch msg := command().(type) {
		case spinner.TickMsg:
			tick, hasTick = msg, true
		case sessionLoadedMsg:
			loaded, hasLoad = msg, true
		}
	}
	if !hasTick || !hasLoad {
		t.Fatalf("missing scheduled work: spinner=%v load=%v", hasTick, hasLoad)
	}
	before := m.Spinner.View()
	_, next := m.Update(tick)
	if m.Spinner.View() == before || next == nil {
		t.Fatal("scheduled tick must advance and continue the loading animation")
	}
	m.Update(loaded)
	if m.sessionLoading || m.state != stateReady {
		t.Fatal("history load did not return to ready")
	}
	_, next = m.Update(tick)
	if next != nil {
		t.Fatal("loading animation must stop after the page is loaded")
	}
}

type windowSessionRuntime struct {
	tickFakeBot
	requests []int
}

func (b *windowSessionRuntime) SessionMessages() []controlruntime.DisplayMessage {
	panic("full history must not be read")
}
func (b *windowSessionRuntime) SessionHistoryWindow(before, limit int) (controlruntime.SessionHistoryWindow, error) {
	b.requests = append(b.requests, before)
	end := before - 1
	if before == 0 {
		end = 10000
	}
	first := end - limit + 1
	if first < 1 {
		first = 1
	}
	var messages []controlruntime.DisplayMessage
	for i := first; i <= end; i++ {
		messages = append(messages, controlruntime.DisplayMessage{Role: "user", Content: fmt.Sprint(i)})
	}
	return controlruntime.SessionHistoryWindow{Messages: messages, Before: first, End: end, Total: 10000, HasMore: first > 1}, nil
}
func TestSessionWindowPagesWithoutRetainingHistory(t *testing.T) {
	b := &windowSessionRuntime{}
	m, err := NewModel(b)
	if err != nil {
		t.Fatal(err)
	}
	cmd := m.loadSessionMessages()
	m.applySessionLoaded(sessionLoadResult(t, cmd))
	for i := 0; i < 20; i++ {
		cmd = m.requestHistoryPage(true)
		if cmd == nil {
			t.Fatal("missing older page command")
		}
		m.applySessionLoaded(sessionLoadResult(t, cmd))
		if m.Messages.Len() > 102 {
			t.Fatalf("retained older page bodies: %d", m.Messages.Len())
		}
	}
	if m.sessionHistoryPage != 20 || b.requests[1] != 9901 {
		t.Fatalf("wrong cursors: %v", b.requests)
	}
	cmd = m.requestHistoryPage(false)
	m.applySessionLoaded(sessionLoadResult(t, cmd))
	if m.sessionHistoryPage != 19 {
		t.Fatal("cannot navigate newer")
	}
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventRunStarted})
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventInputAccepted, Payload: controlruntime.MessagePayload{Content: "new input"}})
	cmd = m.handleDone(doneMsg{})
	if cmd == nil || !m.sessionLoading {
		t.Fatal("completed run must refresh bounded latest window")
	}
}

type telemetryWindowRuntime struct{ tickFakeBot }

func (b *telemetryWindowRuntime) SessionMessages() []controlruntime.DisplayMessage {
	panic("full history must not be read")
}

func (b *telemetryWindowRuntime) SessionHistoryWindow(before, limit int) (controlruntime.SessionHistoryWindow, error) {
	return controlruntime.SessionHistoryWindow{
		Messages: []controlruntime.DisplayMessage{{Role: "user", Content: "question"}, {Role: "assistant", Content: "answer"}},
		Before:   1, End: 2, Total: 2,
	}, nil
}

func TestCompletedHistoryRefreshKeepsTurnTelemetry(t *testing.T) {
	m, err := NewModel(&telemetryWindowRuntime{})
	if err != nil {
		t.Fatal(err)
	}
	m.applySessionLoaded(sessionLoadResult(t, m.loadSessionMessages()))
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventRunStarted})
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventInputAccepted, Payload: controlruntime.MessagePayload{Content: "question"}})
	cmd := m.handleDone(doneMsg{content: "answer", metrics: controlruntime.MetricsSnapshot{
		TurnTotal: 1050, TurnInput: 1000, TurnCached: 900, TurnNew: 100, TurnOutput: 50, TurnCacheReported: true,
	}})
	m.applySessionLoaded(sessionLoadResult(t, cmd))

	items := m.Messages.Items()
	if _, ok := items[len(items)-1].(*message.AssistantMessageItem); !ok {
		t.Fatalf("last item = %T, want assistant", items[len(items)-1])
	}
	rendered := ansi.Strip(m.Messages.Render())
	for _, want := range []string{"缓存 900", "未缓存 100", "本次命中 90.00%"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("refreshed assistant telemetry missing %q: %s", want, rendered)
		}
	}
}

func TestQueuedChatStartsAfterConcurrentControlRunCompletes(t *testing.T) {
	b := &windowSessionRuntime{}
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
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventRunStarted})
	m.handleDone(doneMsg{})
	m.applySessionLoaded(loaded)
	if len(b.submitted) != 1 || b.submitted[0] != "queued input" || m.sessionPendingChat != nil {
		t.Fatal("queued chat was stranded after deferred control completion")
	}
}

// Execute the load command inside a batch without starting an unrelated tick loop.
func sessionLoadResult(t *testing.T, cmd tea.Cmd) sessionLoadedMsg {
	t.Helper()
	switch msg := cmd().(type) {
	case sessionLoadedMsg:
		return msg
	case tea.BatchMsg:
		for _, command := range msg {
			if loaded, ok := command().(sessionLoadedMsg); ok {
				return loaded
			}
		}
	}
	t.Fatal("command did not schedule a history load")
	return sessionLoadedMsg{}
}
