package tui

import (
	"fmt"
	"nekocode/interaction/tui/components"
	"nekocode/interaction/tui/styles"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	controlruntime "nekocode/runtime"
)

func newTaskBenchmarkModel(b *testing.B) *Model {
	b.Helper()
	m, err := NewModel(&taskFakeRuntime{})
	if err != nil {
		b.Fatal(err)
	}
	m.Ready = true
	m.Width, m.Height = 80, 24
	m.toggleTasks()
	m.handleTaskMessage(taskListMsg{generation: m.tasks.generation, request: m.tasks.listRequest, page: taskTestPage()})
	m.tasks.selected = "test-agent"
	m.openSelectedTask()
	m.handleTaskMessage(taskHistoryMsg{generation: m.tasks.generation, request: m.tasks.detail.request, id: m.tasks.detail.id, page: taskTestHistory()})
	d := m.tasks.detail
	d.attempt = &controlruntime.SubagentAttemptSummary{ID: "attempt", Seq: 1, Status: "running", Prompt: "检查当前实现", TranscriptStart: 1, TranscriptEnd: 40}
	d.info.LatestAttempt = cloneTaskAttempt(d.attempt)
	d.info.Status = "running"
	d.info.Phase = "Running go test"
	d.info.TranscriptSeq = 40
	d.page.Records = nil
	for i := 1; i <= 20; i++ {
		id := fmt.Sprintf("tool-%d", i)
		d.page.Records = append(d.page.Records,
			controlruntime.SubagentRecord{Sequence: i*2 - 1, Role: "assistant", ToolCalls: []controlruntime.SubagentToolCall{{ID: id, Name: "shell", Arguments: `{"command":"go test"}`}}},
			controlruntime.SubagentRecord{Sequence: i * 2, Role: "tool", ToolCallID: id, Content: "ok"},
		)
	}
	m.rebuildTaskDetail()
	return m
}

func BenchmarkTaskPanelProcessing(b *testing.B) {
	m := newTaskBenchmarkModel(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.taskPanelView(80, 24)
	}
}

func BenchmarkTaskRebuildProcessing(b *testing.B) {
	m := newTaskBenchmarkModel(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.rebuildTaskDetail()
		_ = m.taskPanelView(80, 24)
	}
}

func BenchmarkTaskEscapeAndReopen(b *testing.B) {
	m := newTaskBenchmarkModel(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.handleTaskKey(tea.KeyPressMsg{Code: tea.KeyEscape})
		m.openSelectedTask()
	}
}

func BenchmarkTaskStreamFrame(b *testing.B) {
	m := newTaskBenchmarkModel(b)
	d := m.tasks.detail
	d.showProcess = true
	d.page.Total = 40
	m.rebuildTaskDetail()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d.applyLiveOutput(controlruntime.Event{Sequence: uint64(i + 1)}, controlruntime.SubAgentOutput{ID: d.id, AttemptID: "attempt", MessageSeq: 41, Kind: "message", Text: fmt.Sprintf("检查结果 %d", i%2), Reasoning: "核对测试输出"})
		m.handleTaskMessage(taskFrameMsg{generation: m.tasks.generation, request: m.tasks.streamRequest, id: d.id})
		_ = m.taskPanelView(80, 24)
	}
}

func BenchmarkTaskConcurrentStreams(b *testing.B) {
	m := newTaskBenchmarkModel(b)
	m.Messages.SetProcessing(true)
	m.Messages.ProcessStreamText(strings.Repeat("Streaming **main output** with evidence.\n", 200))
	_ = m.View()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.Messages.ProcessStreamText(" delta")
		d := m.tasks.detail
		d.applyLiveOutput(controlruntime.Event{Sequence: uint64(i + 1)}, controlruntime.SubAgentOutput{ID: d.id, AttemptID: "attempt", MessageSeq: 41, Kind: "message", Text: fmt.Sprint(i)})
		m.handleTaskMessage(taskFrameMsg{generation: m.tasks.generation, request: m.tasks.streamRequest, id: d.id})
		_ = m.View()
	}
}

type taskBackgroundRenderCounter struct {
	components.Item
	renders int
}

func (c *taskBackgroundRenderCounter) Render(width int) string {
	c.renders++
	return c.Item.Render(width)
}

func TestTaskOverlayDefersHiddenMainRendering(t *testing.T) {
	m := openedTaskModel(t)
	openTestDetail(m)
	m.Messages.SetProcessing(true)
	items := m.Messages.Items()
	counter := &taskBackgroundRenderCounter{Item: items[len(items)-1]}
	m.Messages.ReplaceItem(len(items)-1, counter)
	_ = m.View()
	before := counter.renders
	for range 20 {
		m.Messages.ProcessStreamText(" retained")
		_ = m.View()
	}
	m.handleTaskKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	_ = m.View()
	if counter.renders != before {
		t.Fatalf("hidden main card rendered %d extra times before returning from detail", counter.renders-before)
	}
	m.closeTasks()
	_ = m.View()
	if counter.renders != before+1 || m.Messages.AccumulatedText() != strings.Repeat(" retained", 20) || !m.Messages.AtBottom() {
		t.Fatal("closing overlay did not render all accumulated main output once at the bottom")
	}
}

func TestTaskBackgroundRefreshesForResizeAndFocusedControls(t *testing.T) {
	m := openedTaskModel(t)
	openTestDetail(m)
	m.Messages.SetProcessing(true)
	_ = m.View()
	m.Messages.ProcessStreamText("new main text")
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	_ = m.View()
	if m.tasks.backgroundWidth != 100 || m.tasks.backgroundHeight != 30 {
		t.Fatal("resized overlay retained old background dimensions")
	}
	m.Messages.ProcessStreamText(" after resize")
	for _, state := range []chatState{stateQuestioning, stateConfirming} {
		m.state = state
		_ = m.View()
		if m.tasks.background != "" {
			t.Fatal("focused controls reused a stale task background")
		}
		m.state = stateProcessing
		_ = m.View()
	}
	m.closeTasks()
	view := m.View()
	if !strings.Contains(view.Content, "after resize") {
		t.Fatal("main streaming output disappeared after closing overlay")
	}
}

func TestMainStreamReusesTaskViewUntilTaskFrame(t *testing.T) {
	m := openedTaskModel(t)
	openTestDetail(m)
	m.Messages.SetProcessing(true)
	before := m.View().Content
	m.Update(runtimeEventMsg{event: controlruntime.Event{Type: controlruntime.EventAssistantDelta, Payload: controlruntime.DeltaPayload{Delta: "main-only delta"}}})
	if !m.tasks.reuseView || m.View().Content != before || m.Messages.AccumulatedText() != "main-only delta" {
		t.Fatal("main delta failed to preserve the visible task frame and accumulate data")
	}
	d := m.tasks.detail
	d.applyLiveOutput(controlruntime.Event{Sequence: 1}, controlruntime.SubAgentOutput{ID: d.id, MessageSeq: d.page.Total + 1, Kind: "message", Text: "NEW_TASK_FRAME"})
	m.Update(taskFrameMsg{generation: m.tasks.generation, request: m.tasks.streamRequest, id: d.id})
	if m.tasks.reuseView || !strings.Contains(m.View().Content, "NEW_TASK_FRAME") {
		t.Fatal("main stream cache hid a newer task frame")
	}
}

func BenchmarkTaskMainStreamWhileDetailOpen(b *testing.B) {
	m := newTaskBenchmarkModel(b)
	m.Messages.SetProcessing(true)
	m.Messages.ProcessStreamText(strings.Repeat("Streaming **main output**.\n", 200))
	_ = m.View()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		m.Update(runtimeEventMsg{event: controlruntime.Event{Type: controlruntime.EventAssistantDelta, Payload: controlruntime.DeltaPayload{Delta: " delta"}}})
		_ = m.View()
	}
}

func TestTaskOverlaySuspendsReplacementMessages(t *testing.T) {
	m := openedTaskModel(t)
	openTestDetail(m)
	_ = m.View()
	// Session history can finish loading after the overlay background was cached.
	sty := styles.DefaultStyles()
	m.Messages = components.NewMessages(80, 12, &sty)
	m.Messages.SetProcessing(true)
	items := m.Messages.Items()
	counter := &taskBackgroundRenderCounter{Item: items[len(items)-1]}
	m.Messages.ReplaceItem(len(items)-1, counter)
	_ = m.View()
	before := counter.renders
	m.Messages.ProcessStreamText("replacement output")
	_ = m.View()
	if counter.renders != before {
		t.Fatal("replacement messages rendered underneath cached overlay")
	}
	m.closeTasks()
	_ = m.View()
	if m.Messages.AccumulatedText() != "replacement output" {
		t.Fatal("replacement stream was lost")
	}
}
