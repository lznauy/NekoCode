package tui

import (
	"context"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	controlruntime "nekocode/runtime"
)

type replayTaskRuntime struct {
	taskFakeRuntime
	filter controlruntime.EventFilter
	output controlruntime.Event
}

func (r *replayTaskRuntime) ReplayEvents(_ context.Context, filter controlruntime.EventFilter) (<-chan controlruntime.Event, error) {
	r.filter = filter
	ch := make(chan controlruntime.Event, 1)
	ch <- r.output
	close(ch)
	return ch, nil
}

func TestTaskReconnectReplaysAfterLastOutput(t *testing.T) {
	m := openedTaskModel(t)
	openTestDetail(m)
	d := m.tasks.detail
	sendTaskOutput(m, 10, controlruntime.SubAgentOutput{ID: d.id, MessageSeq: 5, Kind: "text_delta", Text: "before "})
	rt := &replayTaskRuntime{output: controlruntime.Event{Sequence: 11, Type: controlruntime.EventSubAgentOutput, Payload: controlruntime.SubAgentOutput{ID: d.id, MessageSeq: 5, Kind: "text_delta", Text: "after"}}}
	m.Runtime = rt
	subscribed := m.subscribeTask()()
	_, listen := m.handleTaskMessage(subscribed)
	if listen == nil || rt.filter.After != 10 || rt.filter.SubAgentID != d.id {
		t.Fatal("reconnect did not replay from the last output")
	}
	m.handleTaskMessage(listen())
	if d.liveMessage.text.String() != "before after" {
		t.Fatal("reconnect lost or duplicated live text")
	}
}

// Apply the history/subscription commands returned by an attempt switch. Frame
// timers and the next live receive are left to the normal event loop.
func applyAttemptCommands(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, child := range batch {
			applyAttemptCommands(t, m, child)
		}
		return
	}
	_, next := m.handleTaskMessage(msg)
	if subscribed, ok := msg.(taskSubscribedMsg); ok && subscribed.err == nil && next != nil {
		m.handleTaskMessage(next())
	}
}

func TestAttemptSwitchReplaysPendingLiveContent(t *testing.T) {
	m := openedTaskModel(t)
	openTestDetail(m)
	d := m.tasks.detail
	current := &controlruntime.SubagentAttemptSummary{ID: "current", Seq: 2, Status: "running", TranscriptStart: 1, TranscriptEnd: 4}
	old := &controlruntime.SubagentAttemptSummary{ID: "old", Seq: 1, Status: "completed", TranscriptStart: 1, TranscriptEnd: 1}
	d.attempt = cloneTaskAttempt(current)
	d.info.LatestAttempt = cloneTaskAttempt(current)
	prefix := controlruntime.SubAgentOutput{ID: d.id, AttemptID: current.ID, MessageSeq: 5, Kind: "text_delta", Text: "prefix "}
	m.Runtime = &replayTaskRuntime{output: controlruntime.Event{Sequence: 10, Type: controlruntime.EventSubAgentOutput, Payload: prefix}}
	sendTaskOutput(m, 10, prefix)
	previousRequest := m.tasks.streamRequest
	applyAttemptCommands(t, m, m.selectTaskAttempt(old))
	applyAttemptCommands(t, m, m.selectTaskAttempt(current))
	sendTaskOutput(m, 11, controlruntime.SubAgentOutput{ID: d.id, AttemptID: current.ID, MessageSeq: 5, Kind: "text_delta", Text: "suffix"})
	if d.liveMessage == nil || d.liveMessage.text.String() != "prefix suffix" {
		t.Fatal("switching attempts lost pending live content")
	}
	m.handleTaskMessage(taskEventMsg{generation: m.tasks.generation, request: previousRequest, id: d.id, ok: true, event: controlruntime.Event{Sequence: 12, Type: controlruntime.EventSubAgentOutput, Payload: controlruntime.SubAgentOutput{ID: d.id, AttemptID: current.ID, MessageSeq: 5, Kind: "text_delta", Text: " stale subscription"}}})
	if d.liveMessage.text.String() != "prefix suffix" {
		t.Fatal("old subscription polluted replayed content")
	}
}

func TestPausedFirstHistoryDoesNotDuplicateLivePreview(t *testing.T) {
	for _, nextMessage := range []bool{false, true} {
		m := openedTaskModel(t)
		m.openSelectedTask()
		d := m.tasks.detail
		d.showProcess = true
		sendTaskOutput(m, 1, controlruntime.SubAgentOutput{ID: d.id, MessageSeq: 5, Kind: "message", Text: "complete response"})
		m.handleTaskMessage(taskFrameMsg{generation: m.tasks.generation, request: m.tasks.streamRequest, id: d.id})
		d.follow = false
		if nextMessage {
			sendTaskOutput(m, 2, controlruntime.SubAgentOutput{ID: d.id, MessageSeq: 6, Kind: "text_delta", Text: "pending next message"})
		}
		page := taskTestHistory()
		page.End, page.Total = 5, 5
		page.Records = append(page.Records, controlruntime.SubagentRecord{Sequence: 5, Role: "assistant", Content: "complete response"})
		m.handleTaskMessage(taskHistoryMsg{generation: m.tasks.generation, request: d.request, id: d.id, page: page})
		count := 0
		for _, entry := range d.entries {
			if entry.key == "legacy:record:5" {
				count++
			}
		}
		if count != 1 || d.livePreview != nil {
			t.Fatalf("committed response still has a frozen preview: copies=%d preview=%v", count, d.livePreview != nil)
		}
		if nextMessage && (d.liveMessage == nil || d.liveMessage.sequence != 6) {
			t.Fatal("committing frozen preview discarded the next pending message")
		}
	}
}

func sendTaskOutput(m *Model, sequence uint64, output controlruntime.SubAgentOutput) {
	m.handleTaskMessage(taskEventMsg{generation: m.tasks.generation, request: m.tasks.streamRequest, id: m.tasks.detail.id, ok: true, event: controlruntime.Event{Sequence: sequence, Type: controlruntime.EventSubAgentOutput, Payload: output}})
}

func TestTaskStreamCoalescesAndReconcilesCommittedMessage(t *testing.T) {
	m := openedTaskModel(t)
	openTestDetail(m)
	d := m.tasks.detail
	d.showProcess = true
	m.rebuildTaskDetail()
	oldItem := d.entries[0].item
	for i := 1; i <= 100; i++ {
		sendTaskOutput(m, uint64(i), controlruntime.SubAgentOutput{ID: d.id, MessageSeq: 5, Kind: "text_delta", Text: "x"})
	}
	if !d.framePending || d.liveMessage == nil || d.liveMessage.text.Len() != 100 {
		t.Fatal("stream did not collect deltas")
	}
	if m.scheduleTaskFrame() != nil {
		t.Fatal("a frame was scheduled per token")
	}
	if len(d.entries) != 4 {
		t.Fatal("stream rendered before the coalesced frame")
	}
	sendTaskOutput(m, 101, controlruntime.SubAgentOutput{ID: d.id, MessageSeq: 5, Kind: "message", Text: "finished response", Reasoning: "verified evidence"})
	m.handleTaskMessage(taskFrameMsg{generation: m.tasks.generation, request: m.tasks.streamRequest, id: d.id})
	if d.framePending || d.entries[0].item != oldItem {
		t.Fatal("frame discarded unchanged message items")
	}
	liveKey := "legacy:record:5"
	var liveItem any
	for _, entry := range d.entries {
		if entry.key == liveKey {
			liveItem = entry.item
		}
	}
	if liveItem == nil {
		t.Fatal("live response was not rendered")
	}
	page := taskTestHistory()
	page.End, page.Total = 5, 5
	page.Records = append(page.Records, controlruntime.SubagentRecord{Role: "assistant", Sequence: 5, Content: "finished response", Reasoning: "verified evidence"})
	m.handleTaskMessage(taskHistoryMsg{generation: m.tasks.generation, request: d.request, id: d.id, page: page})
	count := 0
	for _, entry := range d.entries {
		if entry.key == liveKey {
			count++
			if entry.item != liveItem {
				t.Fatal("committed message lost its cached render")
			}
		}
	}
	if count != 1 || d.liveMessage != nil {
		t.Fatal("live response duplicated after commitment")
	}
	sendTaskOutput(m, 102, controlruntime.SubAgentOutput{ID: d.id, MessageSeq: 5, Kind: "text_delta", Text: "stale replay"})
	if d.liveMessage != nil {
		t.Fatal("committed stream was replayed")
	}
}

func TestTaskStreamPreservesPausedViewportAndRejectsOldAttempt(t *testing.T) {
	m := openedTaskModel(t)
	openTestDetail(m)
	d := m.tasks.detail
	d.attempt = &controlruntime.SubagentAttemptSummary{ID: "current", Seq: 2, Status: "running", TranscriptStart: 1, TranscriptEnd: 4}
	d.info.LatestAttempt = cloneTaskAttempt(d.attempt)
	d.showProcess = true
	m.rebuildTaskDetail()
	d.follow = false
	d.list.ScrollToTop()
	index, line := d.list.ViewportAnchor()
	old := d.entries[0].item
	sendTaskOutput(m, 1, controlruntime.SubAgentOutput{ID: d.id, AttemptID: "old", MessageSeq: 5, Kind: "text_delta", Text: "wrong attempt"})
	if d.liveMessage != nil {
		t.Fatal("old attempt entered live text")
	}
	sendTaskOutput(m, 2, controlruntime.SubAgentOutput{ID: d.id, AttemptID: "current", MessageSeq: 5, Kind: "text_delta", Text: "new progress"})
	if d.framePending {
		t.Fatal("paused transcript scheduled an update")
	}
	m.handleTaskMessage(taskTickMsg{generation: m.tasks.generation})
	gotIndex, gotLine := d.list.ViewportAnchor()
	if index != gotIndex || line != gotLine || d.entries[0].item != old {
		t.Fatal("live text disturbed paused reading")
	}
	m.handleTaskKey(tea.KeyPressMsg{Code: tea.KeyEnd})
	m.handleTaskMessage(taskFrameMsg{generation: m.tasks.generation, request: m.tasks.streamRequest, id: d.id})
	if !d.follow || !d.list.AtBottom() {
		t.Fatal("returning to the end did not resume following")
	}
}

func TestTaskStreamResetReplayAndSizeLimit(t *testing.T) {
	d := &taskDetail{id: "child", tools: make(map[string]liveTaskTool)}
	apply := func(seq uint64, kind, text string) {
		d.applyLiveOutput(controlruntime.Event{Sequence: seq}, controlruntime.SubAgentOutput{ID: d.id, MessageSeq: 1, Kind: kind, Text: text})
	}
	apply(1, "text_delta", "failed attempt")
	apply(2, "reset", "")
	apply(3, "reasoning_delta", strings.Repeat("思考", taskLiveTextBytes))
	apply(4, "text_delta", "replacement")
	apply(4, "text_delta", "duplicate")
	if d.liveMessage.text.String() != "replacement" || d.liveMessage.reasoning.Len() > taskLiveTextBytes || !utf8.ValidString(d.liveMessage.reasoning.String()) {
		t.Fatal("retry, replay or bounded UTF-8 text was mishandled")
	}
}

func TestPausedLivePreviewSurvivesHistoryRefresh(t *testing.T) {
	m := openedTaskModel(t)
	openTestDetail(m)
	d := m.tasks.detail
	d.showProcess = true
	sendTaskOutput(m, 1, controlruntime.SubAgentOutput{ID: d.id, MessageSeq: 5, Kind: "text_delta", Text: "visible progress"})
	m.handleTaskMessage(taskFrameMsg{generation: m.tasks.generation, request: m.tasks.streamRequest, id: d.id})
	preview := d.entries[len(d.entries)-1]
	d.follow = false
	d.list.ScrollToTop()
	index, line := d.list.ViewportAnchor()
	sendTaskOutput(m, 2, controlruntime.SubAgentOutput{ID: d.id, MessageSeq: 5, Kind: "text_delta", Text: " buffered update"})
	m.handleTaskMessage(taskHistoryMsg{generation: m.tasks.generation, request: d.request, id: d.id, page: taskTestHistory()})
	last := d.entries[len(d.entries)-1]
	gotIndex, gotLine := d.list.ViewportAnchor()
	if last.key != preview.key || last.item != preview.item || gotIndex != index || gotLine != line {
		t.Fatal("paused history refresh removed or changed the displayed live preview")
	}
}

func TestQueuedTaskStartsFollowingItsFirstAttempt(t *testing.T) {
	for _, viaEvent := range []bool{false, true} {
		m := openedTaskModel(t)
		m.tasks.items = []controlruntime.SubagentSummary{{ID: "queued", Status: "queued", Prompt: "inspect"}}
		m.tasks.selected = "queued"
		m.openSelectedTask()
		d := m.tasks.detail
		if viaEvent {
			m.handleTaskMessage(taskEventMsg{generation: m.tasks.generation, id: d.id, ok: true, event: controlruntime.Event{Type: controlruntime.EventSubAgentStarted, Payload: controlruntime.SubAgentPayload{ID: d.id, AttemptID: "first", AttemptSeq: 1, Status: "running", Prompt: "inspect"}}})
		} else {
			item := controlruntime.SubagentSummary{ID: d.id, Recorded: true, Status: "running", LatestAttempt: &controlruntime.SubagentAttemptSummary{ID: "first", Seq: 1, Status: "running", TranscriptStart: 1, TranscriptEnd: 1}}
			m.handleTaskMessage(taskListMsg{generation: m.tasks.generation, request: m.tasks.listRequest, page: controlruntime.SubagentPage{SessionID: "parent", Items: []controlruntime.SubagentSummary{item}}})
		}
		if d.attempt == nil || d.attempt.ID != "first" || !d.isLatestAttempt() || d.newAttempt {
			t.Fatalf("queued task did not adopt first execution (event=%v)", viaEvent)
		}
		sendTaskOutput(m, 1, controlruntime.SubAgentOutput{ID: d.id, AttemptID: "first", MessageSeq: 2, Kind: "text_delta", Text: "working"})
		if d.liveMessage == nil {
			t.Fatal("first execution's live text was ignored")
		}
	}
}

func TestTaskStreamingFitsTerminalSizes(t *testing.T) {
	m := openedTaskModel(t)
	openTestDetail(m)
	d := m.tasks.detail
	sendTaskOutput(m, 1, controlruntime.SubAgentOutput{ID: d.id, MessageSeq: 5, Kind: "message", Text: strings.Repeat("实时输出，检查接口。\n", 20), Reasoning: "思考：核对测试证据。"})
	for _, size := range [][2]int{{20, 6}, {40, 16}, {80, 24}, {160, 40}} {
		m.Width, m.Height = size[0], size[1]
		m.rebuildTaskDetail()
		view := m.taskPanelView(size[0], size[1])
		lines := strings.Split(view, "\n")
		if len(lines) > size[1] {
			t.Fatalf("stream overflows height at %v", size)
		}
		for _, line := range lines {
			if ansi.StringWidth(line) > size[0] {
				t.Fatalf("stream overflows width at %v", size)
			}
		}
	}
}

func TestTaskReconnectKeepsLifecycleWatermark(t *testing.T) {
	m := openedTaskModel(t)
	openTestDetail(m)
	d := m.tasks.detail
	sendTaskOutput(m, 10, controlruntime.SubAgentOutput{ID: d.id, MessageSeq: 5, Kind: "message", Text: "current"})
	m.handleTaskMessage(taskEventMsg{generation: m.tasks.generation, request: m.tasks.streamRequest, id: d.id, ok: true, event: controlruntime.Event{Sequence: 11, Type: controlruntime.EventSubAgentUpdated, Payload: controlruntime.SubAgentPayload{ID: d.id, Status: "running", Phase: "current phase"}}})
	rt := &replayTaskRuntime{}
	m.Runtime = rt
	m.subscribeTask()()
	if rt.filter.After != 11 {
		t.Fatalf("reconnect replays already seen lifecycle: after=%d", rt.filter.After)
	}
}

func TestTaskReplayDoesNotRewindSnapshotStatus(t *testing.T) {
	m := openedTaskModel(t)
	openTestDetail(m)
	d := m.tasks.detail
	d.info.Status, d.info.Phase, d.info.UpdatedAt = "completed", "latest phase", time.Now()
	m.handleTaskMessage(taskEventMsg{generation: m.tasks.generation, request: m.tasks.streamRequest, id: d.id, ok: true, event: controlruntime.Event{Sequence: 1, Time: d.info.UpdatedAt.Add(-time.Second), Type: controlruntime.EventSubAgentStarted, Payload: controlruntime.SubAgentPayload{ID: d.id, Status: "running", Phase: "old phase"}}})
	if d.info.Status != "completed" || d.info.Phase != "latest phase" {
		t.Fatal("historical replay rewound the snapshot")
	}
}

func TestTaskStreamWaitsForInitialHistory(t *testing.T) {
	m := openedTaskModel(t)
	m.openSelectedTask()
	d := m.tasks.detail
	d.showProcess = true
	sendTaskOutput(m, 1, controlruntime.SubAgentOutput{ID: d.id, MessageSeq: 5, Kind: "message", Text: "pending live text"})
	if d.framePending {
		t.Fatal("stream scheduled an intermediate frame before initial history")
	}
	m.handleTaskMessage(taskHistoryMsg{generation: m.tasks.generation, request: d.request, id: d.id, page: taskTestHistory()})
	if !strings.Contains(m.taskPanelView(80, 24), "pending live text") {
		t.Fatal("loading the history snapshot lost newer live text")
	}
}

func TestTaskReopenKeepsCachedDetailStable(t *testing.T) {
	m := openedTaskModel(t)
	openTestDetail(m)
	w, h := m.taskSize()
	before := m.taskPanelView(w, h)
	m.handleTaskKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	m.openSelectedTask()
	m.tasks.detail.attemptsLoading = true // Background metadata refresh also keeps the cached view stable.
	if after := m.taskPanelView(w, h); after != before {
		t.Fatal("reopening cached detail flashed a loading state or cleared its content")
	}
}
