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

func TestLatestTelemetrySurvivesReloadButDoesNotLeakToOlderPage(t *testing.T) {
	m, err := NewModel(&telemetryWindowRuntime{})
	if err != nil {
		t.Fatal(err)
	}
	m.applySessionLoaded(sessionLoadResult(t, m.loadSessionMessages()))
	m.refreshTelemetry = &controlruntime.MetricsSnapshot{TurnInput: 1000, TurnOutput: 50}
	m.applySessionLoaded(sessionLoadResult(t, m.loadSessionWindow(0, 0)))
	m.applySessionLoaded(sessionLoadResult(t, m.loadSessionWindow(0, 0)))
	if !strings.Contains(ansi.Strip(m.Messages.Render()), "总计") {
		t.Fatal("latest telemetry disappeared after reload")
	}
	m.applySessionLoaded(sessionLoadResult(t, m.loadSessionWindow(2, 1)))
	if strings.Contains(ansi.Strip(m.Messages.Render()), "总计") {
		t.Fatal("latest telemetry leaked to an older page")
	}
	m.applySessionLoaded(sessionLoadResult(t, m.loadSessionWindow(0, 0)))
	if !strings.Contains(ansi.Strip(m.Messages.Render()), "总计") {
		t.Fatal("latest telemetry disappeared after paging back")
	}
	m.applySessionLoaded(sessionLoadResult(t, m.loadSessionMessages()))
	if strings.Contains(ansi.Strip(m.Messages.Render()), "总计") {
		t.Fatal("telemetry leaked into a newly loaded session")
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

func TestFailedTurnDoesNotRestorePreviousTurnTelemetry(t *testing.T) {
	m, err := NewModel(&telemetryWindowRuntime{})
	if err != nil {
		t.Fatal(err)
	}
	m.applySessionLoaded(sessionLoadResult(t, m.loadSessionMessages()))
	m.refreshTelemetry = &controlruntime.MetricsSnapshot{TurnInput: 1000, TurnOutput: 50}
	m.applySessionLoaded(sessionLoadResult(t, m.loadSessionWindow(0, 0)))
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventRunStarted})
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventInputAccepted, Payload: controlruntime.MessagePayload{Content: "next question"}})
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventRunFailed, Payload: controlruntime.RunResult{Error: "failed"}})
	m.applySessionLoaded(sessionLoadResult(t, m.loadSessionWindow(0, 0)))
	if strings.Contains(ansi.Strip(m.Messages.Render()), "总计") {
		t.Fatal("previous turn telemetry restored after a failed turn")
	}
}

func TestHistoryWorkerCannotRestoreTelemetryFromPreviousRun(t *testing.T) {
	m, err := NewModel(&telemetryWindowRuntime{})
	if err != nil {
		t.Fatal(err)
	}
	m.applySessionLoaded(sessionLoadResult(t, m.loadSessionMessages()))
	m.refreshTelemetry = &controlruntime.MetricsSnapshot{TurnInput: 1000, TurnOutput: 50}
	load := m.loadSessionWindow(0, 0)
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventRunStarted})
	loaded := sessionLoadResult(t, load)
	retry := m.applySessionLoaded(loaded)
	if retry == nil || !m.sessionLoading {
		t.Fatal("history worker with stale telemetry must be rebuilt")
	}
	m.applySessionLoaded(sessionLoadResult(t, retry))
	if strings.Contains(ansi.Strip(m.Messages.Render()), "总计") {
		t.Fatal("in-flight history restored previous run telemetry")
	}
}

func TestTurnTelemetryDoesNotCrossLatestUserBoundary(t *testing.T) {
	telemetry := &controlruntime.MetricsSnapshot{TurnInput: 1000, TurnOutput: 12}
	history := []message.ChatMessage{{Role: "user", Content: "previous question"}, {Role: "assistant", Content: "previous answer"}, {Role: "user", Content: "latest question"}, {Role: "system", Content: "no answer"}}
	restored := withTurnTelemetry(history, telemetry)
	if restored[1].Telemetry != nil {
		t.Fatal("latest usage attached to the previous answer")
	}
	if len(restored) != len(history)+1 || restored[len(restored)-1].Role != "telemetry" {
		t.Fatal("answerless turn must keep a standalone telemetry footer")
	}
}

type retryWindowRuntime struct {
	windowSessionRuntime
	failNext bool
}

func (b *retryWindowRuntime) SessionHistoryWindow(before, limit int) (controlruntime.SessionHistoryWindow, error) {
	if b.failNext {
		b.failNext = false
		return controlruntime.SessionHistoryWindow{}, fmt.Errorf("temporary history read failure")
	}
	return b.windowSessionRuntime.SessionHistoryWindow(before, limit)
}

func TestFailedHistoryPageCanRetryWithoutSkipping(t *testing.T) {
	b := &retryWindowRuntime{}
	m, err := NewModel(b)
	if err != nil {
		t.Fatal(err)
	}
	m.applySessionLoaded(sessionLoadResult(t, m.loadSessionMessages()))
	before := m.sessionHistoryBefore
	b.failNext = true
	m.applySessionLoaded(sessionLoadResult(t, m.requestHistoryPage(true)))
	if m.sessionLoading || m.state != stateReady || m.sessionHistoryPage != 0 {
		t.Fatal("failed page left the session busy or advanced the page")
	}
	m.applySessionLoaded(sessionLoadResult(t, m.requestHistoryPage(true)))
	if m.sessionLoading || m.state != stateReady || m.sessionHistoryPage != 1 {
		t.Fatal("failed page could not be retried")
	}
	if len(b.requests) != 2 || b.requests[1] != before {
		t.Fatalf("retry skipped a history window: %v", b.requests)
	}
}

func TestSameHistoryWindowRefreshPreservesManualReadingPosition(t *testing.T) {
	m, err := NewModel(&windowSessionRuntime{})
	if err != nil {
		t.Fatal(err)
	}
	m.applySessionLoaded(sessionLoadResult(t, m.loadSessionMessages()))
	m.Messages.ScrollBy(-5)
	m.Messages.SetFollow(false)
	wantIndex, wantLine := m.Messages.ViewportAnchor()
	m.applySessionLoaded(sessionLoadResult(t, m.loadSessionWindow(0, 0)))
	index, line := m.Messages.ViewportAnchor()
	if m.Messages.Follow || index != wantIndex || line != wantLine {
		t.Fatalf("refresh moved manual reading position: %d,%d -> %d,%d, follow=%v", wantIndex, wantLine, index, line, m.Messages.Follow)
	}
}

type completedTurnWindowRuntime struct {
	tickFakeBot
	history []controlruntime.DisplayMessage
}

func (b *completedTurnWindowRuntime) SessionHistoryWindow(before, limit int) (controlruntime.SessionHistoryWindow, error) {
	return controlruntime.SessionHistoryWindow{Messages: b.history, Before: 1, End: len(b.history), Total: len(b.history)}, nil
}
func TestRestoredSessionKeepsCompletionMissingFromHistory(t *testing.T) {
	for _, persisted := range []string{"none", "input", "complete"} {
		for _, metered := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/metered=%v", persisted, metered), func(t *testing.T) {
				b := &completedTurnWindowRuntime{history: []controlruntime.DisplayMessage{{Role: "user", Content: "previous question"}, {Role: "assistant", Content: "previous answer"}}}
				m, err := NewModel(b)
				if err != nil {
					t.Fatal(err)
				}
				m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
				m.applySessionLoaded(sessionLoadResult(t, m.loadSessionMessages()))
				m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventRunStarted})
				m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventInputAccepted, Payload: controlruntime.MessagePayload{Content: "new question", Source: controlruntime.SourceRef{Kind: "tui"}}})
				const reply = "reply must remain visible"
				m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventAssistantDelta, Payload: controlruntime.DeltaPayload{Delta: reply}})
				if persisted != "none" {
					b.history = append(b.history, controlruntime.DisplayMessage{Role: "user", Content: "new question"})
				}
				if persisted == "complete" {
					b.history = append(b.history, controlruntime.DisplayMessage{Role: "assistant", Content: reply})
				}
				var metrics controlruntime.MetricsSnapshot
				if metered {
					metrics = controlruntime.MetricsSnapshot{TurnInput: 1000, TurnOutput: 12}
				}
				cmd := m.handleDone(doneMsg{content: reply, metrics: metrics})
				if m.sessionLoading {
					m.applySessionLoaded(sessionLoadResult(t, cmd))
				}
				clean := ansi.Strip(m.View().Content)
				if !strings.Contains(clean, reply) {
					t.Fatalf("history refresh erased the completion: %q", clean)
				}
				var all strings.Builder
				for _, item := range m.Messages.Items() {
					all.WriteString(ansi.Strip(item.Render(80)))
				}
				if strings.Count(all.String(), reply) != 1 || strings.Count(all.String(), "new question") != 1 {
					t.Fatalf("completed turn was lost or duplicated: %q", all.String())
				}
				m.applySessionLoaded(sessionLoadResult(t, m.loadSessionWindow(0, 0)))
				if !strings.Contains(ansi.Strip(m.View().Content), reply) {
					t.Fatal("reloading history erased the transient completion")
				}
			})
		}
	}
}

func TestUnpersistedRepeatedTurnIsNotMistakenForOldTurn(t *testing.T) {
	turn := &completedTurn{input: "same question", after: 2, assistant: message.ChatMessage{Role: "assistant", Content: "same answer"}}
	history := []controlruntime.DisplayMessage{{Role: "user", Content: "same question"}, {Role: "assistant", Content: "same answer"}}
	got, _ := withCompletedTurns(controlruntime.SessionHistoryWindow{Messages: history, Before: 1, End: 2}, []*completedTurn{turn}, turn, nil)
	if len(got) != 4 {
		t.Fatalf("repeated unpersisted turn was deduplicated: %d messages", len(got))
	}
	persisted := []controlruntime.DisplayMessage{{Role: "user", Content: "same question"}, {Role: "assistant", Content: "same answer"}, {Role: "user", Content: "same question"}, {Role: "assistant", Content: "same answer"}}
	if got, _ := withCompletedTurns(controlruntime.SessionHistoryWindow{Messages: persisted, Before: 1, End: 4}, []*completedTurn{turn}, turn, nil); len(got) != 4 {
		t.Fatalf("persisted repeated turn duplicated: %d messages", len(got))
	}
}

func TestHistoryWorkerRetainsPriorErrorDuringNextRun(t *testing.T) {
	b := &completedTurnWindowRuntime{history: []controlruntime.DisplayMessage{{Role: "user", Content: "previous question"}, {Role: "assistant", Content: "previous answer"}}}
	m, err := NewModel(b)
	if err != nil {
		t.Fatal(err)
	}
	m.applySessionLoaded(sessionLoadResult(t, m.loadSessionMessages()))
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventRunStarted})
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventInputAccepted, Payload: controlruntime.MessagePayload{Content: "failed question"}})
	m.applySessionLoaded(sessionLoadResult(t, m.handleDone(doneMsg{content: "quota error"})))
	load := m.loadSessionWindow(0, 0)
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventRunStarted})
	retry := m.applySessionLoaded(sessionLoadResult(t, load))
	if retry == nil || !m.sessionLoading {
		t.Fatal("worker with a stale active turn was accepted")
	}
	m.applySessionLoaded(sessionLoadResult(t, retry))
	found := false
	for _, item := range m.Messages.Items() {
		if strings.Contains(ansi.Strip(item.Render(80)), "quota error") {
			found = true
		}
	}
	if !found || m.state != stateProcessing {
		t.Fatal("history worker discarded the prior error or ended the new run")
	}
}

func TestUnpersistedTurnSurvivesFollowingRun(t *testing.T) {
	b := &completedTurnWindowRuntime{history: []controlruntime.DisplayMessage{{Role: "user", Content: "old question"}, {Role: "assistant", Content: "old answer"}}}
	m, err := NewModel(b)
	if err != nil {
		t.Fatal(err)
	}
	m.applySessionLoaded(sessionLoadResult(t, m.loadSessionMessages()))
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventRunStarted})
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventInputAccepted, Payload: controlruntime.MessagePayload{Content: "failed question"}})
	m.applySessionLoaded(sessionLoadResult(t, m.handleDone(doneMsg{content: "first quota error"})))
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventRunStarted})
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventInputAccepted, Payload: controlruntime.MessagePayload{Content: "successful question"}})
	b.history = append(b.history, controlruntime.DisplayMessage{Role: "user", Content: "successful question"}, controlruntime.DisplayMessage{Role: "assistant", Content: "successful answer"})
	m.applySessionLoaded(sessionLoadResult(t, m.handleDone(doneMsg{content: "successful answer"})))
	var all strings.Builder
	for _, item := range m.Messages.Items() {
		all.WriteString(ansi.Strip(item.Render(80)))
	}
	text := all.String()
	for _, want := range []string{"failed question", "first quota error", "successful question", "successful answer"} {
		if strings.Count(text, want) != 1 {
			t.Fatalf("turn was erased or duplicated: %q", want)
		}
	}
	if strings.Index(text, "first quota error") > strings.Index(text, "successful question") {
		t.Fatal("transient turn moved after the following persisted turn")
	}
}

func TestTransientTurnsKeepOrderAcrossGroupedHistoryWindow(t *testing.T) {
	failed := &completedTurn{input: "failed question", after: 209, assistant: message.ChatMessage{Role: "assistant", Content: "quota error"}}
	success := &completedTurn{input: "next question", after: 209, assistant: message.ChatMessage{Role: "assistant", Content: "next answer"}}
	page := controlruntime.SessionHistoryWindow{Before: 111, End: 214, Messages: []controlruntime.DisplayMessage{{Role: "assistant", Content: "older grouped tools", Sequence: 209}, {Role: "user", Content: "next question", Sequence: 210}, {Role: "assistant", Content: "next answer", Sequence: 214}}}
	history, pending := withCompletedTurns(page, []*completedTurn{failed, success}, success, nil)
	if len(history) != 5 || history[1].Content != "failed question" || history[2].Content != "quota error" || history[3].Content != "next question" {
		t.Fatalf("transient turn inserted at the wrong raw boundary: %+v", history)
	}
	if len(pending) != 1 || pending[0].input != failed.input || pending[0].after != failed.after || pending[0].before != 210 {
		t.Fatal("confirmed completion was not retired or failed turn was dropped")
	}
	older := controlruntime.SessionHistoryWindow{Before: 1, End: 100, Messages: []controlruntime.DisplayMessage{{Role: "assistant", Content: "old page", Sequence: 100}}}
	history, pending = withCompletedTurns(older, pending, nil, nil)
	if len(history) != 1 || len(pending) != 1 {
		t.Fatal("off-page transient turn leaked into older history or was forgotten")
	}
}

func TestTransientTurnDoesNotReuseFollowingConfirmedAnswer(t *testing.T) {
	for _, answer := range []string{"quota error", "successful answer"} {
		t.Run(answer, func(t *testing.T) {
			failed := &completedTurn{input: "repeat question", after: 2, assistant: message.ChatMessage{Role: "assistant", Content: "quota error"}}
			success := &completedTurn{input: "repeat question", after: 2, assistant: message.ChatMessage{Role: "assistant", Content: answer}}
			page := controlruntime.SessionHistoryWindow{Before: 1, End: 4, Messages: []controlruntime.DisplayMessage{{Role: "user", Content: "old question", Sequence: 1}, {Role: "assistant", Content: "old answer", Sequence: 2}, {Role: "user", Content: "repeat question", Sequence: 3}, {Role: "assistant", Content: answer, Sequence: 4}}}
			history, pending := withCompletedTurns(page, []*completedTurn{failed, success}, success, nil)
			for n := 0; n < 3; n++ {
				if len(history) != 6 || len(pending) != 1 {
					t.Fatalf("merge %d lost a distinct repeated turn", n)
				}
				if history[2].Content != "repeat question" || history[3].Content != "quota error" || history[4].Content != "repeat question" || history[5].Content != answer {
					t.Fatalf("merge %d reordered repeated turns", n)
				}
				history, pending = withCompletedTurns(page, pending, success, nil)
			}
		})
	}
}

func TestTransientAnswerAtStartOfPageKeepsPersistedInput(t *testing.T) {
	turn := &completedTurn{input: "failed question", after: 2, before: 4, assistant: message.ChatMessage{Role: "assistant", Content: "quota error"}}
	page := controlruntime.SessionHistoryWindow{Before: 3, End: 4, Messages: []controlruntime.DisplayMessage{{Role: "user", Content: "failed question", Sequence: 3}, {Role: "user", Content: "next question", Sequence: 4}}}
	history, pending := withCompletedTurns(page, []*completedTurn{turn}, turn, nil)
	if len(history) != 3 || history[0].Content != "failed question" || history[1].Content != "quota error" || history[2].Content != "next question" || len(pending) != 1 {
		t.Fatalf("page-start input lost its transient answer: %+v", history)
	}
}

// Coordinator notifications occupy a raw transcript sequence but are hidden
// from visible session history. This matches the standard runtime projection.
type coordinatorHistoryRuntime struct {
	completedTurnWindowRuntime
	rawTotal int
}

func (b *coordinatorHistoryRuntime) SessionHistoryWindow(before, limit int) (controlruntime.SessionHistoryWindow, error) {
	return controlruntime.SessionHistoryWindow{Messages: b.history, Before: 1, End: b.rawTotal, Total: b.rawTotal}, nil
}

func TestCoordinatorCompletionReconcilesHiddenInputWithoutDuplicate(t *testing.T) {
	for _, persisted := range []bool{false, true} {
		t.Run(fmt.Sprintf("persisted=%v", persisted), func(t *testing.T) {
			b := &coordinatorHistoryRuntime{completedTurnWindowRuntime: completedTurnWindowRuntime{history: []controlruntime.DisplayMessage{
				{Role: "user", Content: "check background work", Sequence: 1},
				{Role: "assistant", Content: "previous response", Sequence: 2},
			}}, rawTotal: 2}
			m, err := NewModel(b)
			if err != nil {
				t.Fatal(err)
			}
			m.Update(tea.WindowSizeMsg{Width: 100, Height: 32})
			m.applySessionLoaded(sessionLoadResult(t, m.loadSessionMessages()))
			m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventRunStarted})
			m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventInputAccepted, Payload: controlruntime.MessagePayload{Content: "后台任务已返回结果，正在验收。", Source: controlruntime.SourceRef{Kind: "task_completion"}}})
			const reply = "前端回归检查已返回。核心结论：验收结果仅显示一次。"
			m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventAssistantDelta, Payload: controlruntime.DeltaPayload{Delta: reply}})
			b.rawTotal = 3 // the hidden runtime notification
			if persisted {
				b.history = append(b.history, controlruntime.DisplayMessage{Role: "assistant", Content: reply, Sequence: 4})
				b.rawTotal = 4
			}
			cmd := m.handleDone(doneMsg{content: reply, metrics: controlruntime.MetricsSnapshot{TurnInput: 185200, TurnOutput: 366}})
			m.applySessionLoaded(sessionLoadResult(t, cmd))
			assertOnce := func() {
				t.Helper()
				var text strings.Builder
				for _, item := range m.Messages.Items() {
					text.WriteString(ansi.Strip(item.Render(100)))
				}
				if got := strings.Count(text.String(), reply); got != 1 {
					t.Fatalf("coordinator reply rendered %d times: %s", got, text.String())
				}
				if got := strings.Count(text.String(), "185.6k tok"); got != 1 {
					t.Fatalf("coordinator usage footer rendered %d times: %s", got, text.String())
				}
			}
			assertOnce()
			if !persisted {
				b.history = append(b.history, controlruntime.DisplayMessage{Role: "assistant", Content: reply, Sequence: 4})
				b.rawTotal = 4
			}
			m.applySessionLoaded(sessionLoadResult(t, m.loadSessionWindow(0, 0)))
			assertOnce()
			if len(m.transientTurns) != 0 {
				t.Fatal("committed coordinator reply remained in transient history")
			}
		})
	}
}

func TestRepeatedCoordinatorRepliesRemainSeparateTurns(t *testing.T) {
	const reply = "Still reviewing background work."
	b := &coordinatorHistoryRuntime{completedTurnWindowRuntime: completedTurnWindowRuntime{history: []controlruntime.DisplayMessage{
		{Role: "user", Content: "review", Sequence: 1}, {Role: "assistant", Content: reply, Sequence: 2},
	}}, rawTotal: 2}
	m, err := NewModel(b)
	if err != nil {
		t.Fatal(err)
	}
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 32})
	m.applySessionLoaded(sessionLoadResult(t, m.loadSessionMessages()))
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventRunStarted})
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventInputAccepted, Payload: controlruntime.MessagePayload{Content: "后台任务已返回结果，正在验收。", Source: controlruntime.SourceRef{Kind: "task_completion"}}})
	b.rawTotal = 3
	cmd := m.handleDone(doneMsg{content: reply, metrics: controlruntime.MetricsSnapshot{TurnInput: 1000, TurnOutput: 12}})
	m.applySessionLoaded(sessionLoadResult(t, cmd))
	var text strings.Builder
	for _, item := range m.Messages.Items() {
		text.WriteString(ansi.Strip(item.Render(100)))
	}
	if got := strings.Count(text.String(), reply); got != 2 {
		t.Fatalf("separate identical replies collapsed: count=%d %s", got, text.String())
	}
	b.history = append(b.history, controlruntime.DisplayMessage{Role: "assistant", Content: reply, Sequence: 4})
	b.rawTotal = 4
	m.applySessionLoaded(sessionLoadResult(t, m.loadSessionWindow(0, 0)))
	text.Reset()
	for _, item := range m.Messages.Items() {
		text.WriteString(ansi.Strip(item.Render(100)))
	}
	if got := strings.Count(text.String(), reply); got != 2 || len(m.transientTurns) != 0 {
		t.Fatalf("separate committed turns were duplicated or lost: count=%d transient=%d %s", got, len(m.transientTurns), text.String())
	}
}
