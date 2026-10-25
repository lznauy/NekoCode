package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nekocode/interaction/tui/components/message"
	controlruntime "nekocode/runtime"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestLiveToolInspectorReceivesPreviewUpdates(t *testing.T) {
	m := openedTaskModel(t)
	openTestDetail(m)
	d := m.tasks.detail
	for i, entry := range d.entries {
		if entry.call != nil && entry.call.ID == "shell-1" {
			d.list.RestoreViewportAnchor(i, 0)
			break
		}
	}
	m.inspectVisibleTaskTool()
	if d.inspector == nil {
		t.Fatal("missing pending tool inspector")
	}
	m.handleTaskMessage(taskEventMsg{generation: m.tasks.generation, id: d.id, ok: true, event: controlruntime.Event{Type: controlruntime.EventToolPreview, Payload: controlruntime.ToolPayload{CallID: "shell-1", ToolName: "shell", Preview: "new live progress"}}})
	m.handleTaskMessage(taskTickMsg{generation: m.tasks.generation})
	if !strings.Contains(ansi.Strip(d.inspector.Render()), "new live progress") {
		t.Fatal("open tool inspector stopped receiving live previews")
	}
}

func TestTaskPanelUsesEdgeAlignedFrameAndContinuousBackground(t *testing.T) {
	m := openedTaskModel(t)
	canvas := lipgloss.NewCanvas(80, 24).Compose(lipgloss.NewLayer(m.taskPanelView(80, 24)))
	for y := 0; y < 24; y++ {
		for x := 0; x < 80; x++ {
			cell := canvas.CellAt(x, y)
			if cell == nil || cell.Width == 0 {
				continue
			}
			if cell.Style.Bg == nil {
				t.Fatalf("panel interior lost background at %d,%d", x, y)
			}
			if y == 0 && x > 0 && x < 79 && cell.Content != "▀" {
				t.Fatal("top stroke must sit at the cell's top edge")
			}
			if y == 23 && x > 0 && x < 79 && cell.Content != "▄" {
				t.Fatal("bottom stroke must sit at the cell's bottom edge")
			}
			if y > 0 && y < 23 && x == 0 && cell.Content != "▌" {
				t.Fatal("left stroke must sit at the cell's left edge")
			}
			if y > 0 && y < 23 && x == 79 && cell.Content != "▐" {
				t.Fatal("right stroke must sit at the cell's right edge")
			}
		}
	}
	for _, corner := range []struct {
		x, y  int
		glyph string
	}{{0, 0, "▛"}, {79, 0, "▜"}, {0, 23, "▙"}, {79, 23, "▟"}} {
		if canvas.CellAt(corner.x, corner.y).Content != corner.glyph {
			t.Fatal("frame corner must connect its horizontal and vertical strokes")
		}
	}
}

func TestTaskFrameDoesNotChangeUnderlayOutsidePanel(t *testing.T) {
	m := openedTaskModel(t)
	background := lipgloss.NewStyle().Background(lipgloss.Color("#354657")).Width(80).Height(24).Render("主窗口")
	underlay := lipgloss.NewCanvas(80, 24).Compose(lipgloss.NewLayer(background))
	canvas := lipgloss.NewCanvas(80, 24).Compose(lipgloss.NewLayer(m.taskOverlayView(background)))
	w, h := m.taskSize()
	ox, oy := (80-w)/2, (24-h)/2
	for y := 0; y < 24; y++ {
		for x := 0; x < 80; x++ {
			inside := x >= ox && x < ox+w && y >= oy && y < oy+h
			got := canvas.CellAt(x, y)
			if got.Width == 0 {
				continue
			}
			if !inside {
				want := underlay.CellAt(x, y).Style.Bg
				if got.Style.Bg == nil || want == nil {
					t.Fatal("lost explicit parent background")
				}
				r, g, b, a := got.Style.Bg.RGBA()
				wr, wg, wb, wa := want.RGBA()
				if r != wr || g != wg || b != wb || a != wa {
					t.Fatalf("background escaped frame at %d,%d", x, y)
				}
			} else if got.Style.Bg == nil {
				t.Fatalf("straight border/interior has a background hole at %d,%d", x, y)
			}
		}
	}
	for _, line := range strings.Split(m.taskPanelView(80, 24), "\n") {
		row := lipgloss.NewCanvas(81, 1).Compose(lipgloss.NewLayer(line + "X"))
		if last := row.CellAt(80, 0); last.Content != "X" || last.Style.Bg != nil {
			t.Fatal("panel color leaks past the end of a line")
		}
	}
}

func TestTaskListContentAlignsAcrossStatuses(t *testing.T) {
	m := openedTaskModel(t)
	m.tasks.items = []controlruntime.SubagentSummary{
		{ID: "agent-A", Status: "running", Prompt: "检查接口"},
		{ID: "agent-B", Status: "waiting_question", Prompt: "检查配置"},
		{ID: "agent-C", Status: "completed", Prompt: "检查迁移"},
	}
	lines := strings.Split(ansi.Strip(m.taskPanelView(80, 24)), "\n")
	column := -1
	for _, id := range []string{"agent-A", "agent-B", "agent-C"} {
		found := false
		for _, line := range lines {
			if index := strings.Index(line, id); index >= 0 {
				current := ansi.StringWidth(line[:index])
				if column >= 0 && current != column {
					t.Fatal("status length shifted the content column")
				}
				column, found = current, true
			}
		}
		if !found {
			t.Fatalf("task %s was lost in layout", id)
		}
	}
}

func TestTaskDetailBackgroundCoversEveryCell(t *testing.T) {
	m := openedTaskModel(t)
	openTestDetail(m)
	panel := m.taskPanelView(80, 24)
	canvas := lipgloss.NewCanvas(80, 24).Compose(lipgloss.NewLayer(panel))
	for y := 1; y < 23; y++ {
		for x := 1; x < 79; x++ {
			cell := canvas.CellAt(x, y)
			if cell != nil && cell.Width > 0 && cell.Style.Bg == nil {
				t.Fatalf("detail background missing at %d,%d (%q)", x, y, cell.Content)
			}
		}
	}
}

func TestTaskBackgroundPreservesExplicitColorsAndWideText(t *testing.T) {
	selected := lipgloss.Color("#303a3a")
	content := lipgloss.NewStyle().Background(selected).Render("代码") + "\x1b[0m tail"
	filled := fillTaskBackground(content, 20, 3)
	canvas := lipgloss.NewCanvas(20, 3).Compose(lipgloss.NewLayer(filled))
	if !strings.Contains(ansi.Strip(filled), "代码 tail") {
		t.Fatal("background fill changed wide text")
	}
	r, g, b, a := canvas.CellAt(0, 0).Style.Bg.RGBA()
	wantR, wantG, wantB, wantA := selected.RGBA()
	if r != wantR || g != wantG || b != wantB || a != wantA {
		t.Fatal("background fill replaced an explicit background")
	}
	for y := 0; y < 3; y++ {
		for x := 0; x < 20; x++ {
			cell := canvas.CellAt(x, y)
			if cell.Width > 0 && cell.Style.Bg == nil {
				t.Fatalf("unfilled background at %d,%d", x, y)
			}
		}
	}
}

func TestEmptyTaskListDoesNotFlashLoadingDuringPolling(t *testing.T) {
	m := openedTaskModel(t)
	m.tasks.items = nil
	m.tasks.selected = ""
	m.tasks.listPage = controlruntime.SubagentPage{SessionID: m.tasks.sessionID}
	m.tasks.listLoading = true
	m.handleTaskMessage(taskListMsg{generation: m.tasks.generation, request: m.tasks.listRequest, page: m.tasks.listPage})
	before := m.taskPanelView(80, 24)
	if !strings.Contains(before, "当前会话还没有子任务") {
		t.Fatal("expected confirmed empty state")
	}
	for i := 0; i < 3; i++ {
		m.handleTaskMessage(taskTickMsg{generation: m.tasks.generation})
		if during := m.taskPanelView(80, 24); during != before {
			t.Fatal("background polling flashes the empty task list")
		}
		m.handleTaskMessage(taskListMsg{generation: m.tasks.generation, request: m.tasks.listRequest, page: m.tasks.listPage})
		if after := m.taskPanelView(80, 24); after != before {
			t.Fatal("unchanged empty result repaints different content")
		}
	}
	// Polling still discovers tasks; stabilizing the empty state must not freeze it.
	m.handleTaskMessage(taskTickMsg{generation: m.tasks.generation})
	m.handleTaskMessage(taskListMsg{generation: m.tasks.generation, request: m.tasks.listRequest, page: taskTestPage()})
	if len(m.tasks.items) != 3 || !strings.Contains(m.taskPanelView(80, 24), "3 个任务") {
		t.Fatal("refresh stopped discovering new tasks")
	}
}

type taskFakeRuntime struct{ statusFakeBot }

type taskAttemptFakeRuntime struct{ taskFakeRuntime }

func (*taskAttemptFakeRuntime) SubagentAttempts(string, string, int, int) (controlruntime.SubagentAttemptPage, error) {
	return controlruntime.SubagentAttemptPage{}, nil
}

type hundredTaskRuntime struct{ taskFakeRuntime }

func (*hundredTaskRuntime) Subagents(controlruntime.SubagentFilter) (controlruntime.SubagentPage, error) {
	return hundredTaskTestPage(), nil
}

type taskOutputFakeRuntime struct{ taskFakeRuntime }

func (*taskOutputFakeRuntime) SubagentToolOutput(_, _ string, _ int, offset int64) (controlruntime.SubagentOutputPage, error) {
	return controlruntime.SubagentOutputPage{Content: strings.Repeat("完整输出\n", 30), Offset: offset, NextOffset: offset + 65536, Total: 131072, HasMore: offset == 0}, nil
}

func (*taskFakeRuntime) CurrentSessionID() string { return "parent" }
func (*taskFakeRuntime) Subagents(controlruntime.SubagentFilter) (controlruntime.SubagentPage, error) {
	return taskTestPage(), nil
}
func (*taskFakeRuntime) SubagentHistory(string, string, int, int) (controlruntime.SubagentHistoryPage, error) {
	return taskTestHistory(), nil
}
func (*taskFakeRuntime) Events(ctx context.Context, filter controlruntime.EventFilter) (<-chan controlruntime.Event, error) {
	ch := make(chan controlruntime.Event)
	if filter.SubAgentID != "" {
		go func() { <-ctx.Done(); close(ch) }()
	}
	return ch, nil
}

func taskTestPage() controlruntime.SubagentPage {
	return controlruntime.SubagentPage{SessionID: "parent", Total: 3, Items: []controlruntime.SubagentSummary{
		{ID: "test-agent", SessionID: "parent", Profile: "coder", Prompt: "迁移认证接口，保留旧客户端兼容入口", Status: "running", Phase: "Running go test", Recorded: true, TranscriptSeq: 4},
		{ID: "review-agent", SessionID: "parent", Profile: "reviewer", Prompt: "审查认证模块的安全问题", Status: "completed", Recorded: true},
		{ID: "verify-agent", SessionID: "parent", Profile: "explore", Prompt: "验证数据库配置", Status: "waiting_question", Recorded: true},
	}}
}

func taskTestHistory() controlruntime.SubagentHistoryPage {
	return controlruntime.SubagentHistoryPage{SessionID: "parent", SubagentID: "test-agent", Before: 1, End: 4, Total: 4, Records: []controlruntime.SubagentRecord{
		{Sequence: 1, Role: "user", Content: "迁移认证接口，保留旧客户端兼容入口。"},
		{Sequence: 2, Role: "assistant", Content: "已找到三个调用点，正在验证接口兼容性。", ToolCalls: []controlruntime.SubagentToolCall{{ID: "read-1", Name: "read", Arguments: `{"path":"auth/login.go"}`}}},
		{Sequence: 3, Role: "tool", ToolCallID: "read-1", Content: "[auth/login.go#abc]\n1:package auth\n2:func Login() {}"},
		{Sequence: 4, Role: "assistant", ToolCalls: []controlruntime.SubagentToolCall{{ID: "shell-1", Name: "shell", Arguments: `{"command":"go test ./auth/..."}`}}},
	}}
}

func openedTaskModel(t *testing.T) *Model {
	t.Helper()
	m, err := NewModel(&taskFakeRuntime{})
	if err != nil {
		t.Fatal(err)
	}
	m.Ready = true
	m.Width = 80
	m.Height = 24
	m.resizeMessages()
	m.Input.SetValue("尚未发送的主会话草稿")
	m.Messages.AddMessage(message.ChatMessage{Role: "assistant", Content: "主 agent 正在处理认证模块。"})
	m.toggleTasks()
	s := m.tasks
	m.handleTaskMessage(taskListMsg{generation: s.generation, request: s.listRequest, page: taskTestPage()})
	m.tasks.selected = "test-agent"
	t.Cleanup(m.closeTasks)
	return m
}

func openTestDetail(m *Model) {
	m.openSelectedTask()
	s, d := m.tasks, m.tasks.detail
	m.handleTaskMessage(taskHistoryMsg{generation: s.generation, request: d.request, id: d.id, page: taskTestHistory()})
}

func TestTaskOverlayPreservesMainDraftAndScroll(t *testing.T) {
	m := openedTaskModel(t)
	index, line := m.Messages.ViewportAnchor()
	draft := m.Input.Value()
	openTestDetail(m)
	m.Update(tea.PasteMsg{Content: "不能进入主输入框"})
	m.handleTaskKey(tea.KeyPressMsg{Code: tea.KeyUp})
	m.handleTaskKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.tasks.detail != nil || !m.tasks.open {
		t.Fatal("Esc did not return to the task list")
	}
	m.handleTaskKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.tasks.open || m.Input.Value() != draft {
		t.Fatal("overlay damaged main draft")
	}
	gotIndex, gotLine := m.Messages.ViewportAnchor()
	if gotIndex != index || gotLine != line {
		t.Fatal("overlay moved main viewport")
	}
}

func TestTaskOverlayIgnoresClosedAndReplacedRequests(t *testing.T) {
	m := openedTaskModel(t)
	oldGeneration, oldRequest := m.tasks.generation, m.tasks.listRequest
	m.closeTasks()
	m.toggleTasks()
	m.handleTaskMessage(taskListMsg{generation: oldGeneration, request: oldRequest, page: controlruntime.SubagentPage{SessionID: "other"}})
	if m.tasks.sessionID != "parent" {
		t.Fatal("stale list switched the parent session")
	}
	openTestDetail(m)
	d := m.tasks.detail
	oldHistory := taskHistoryMsg{generation: m.tasks.generation, request: d.request, id: d.id, page: taskTestHistory()}
	m.handleTaskKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	m.handleTaskMessage(oldHistory)
	if m.tasks.detail != nil {
		t.Fatal("late history reopened a closed detail")
	}
}

func TestTaskDetailReusesToolsAndRetainsReadingPosition(t *testing.T) {
	m := openedTaskModel(t)
	openTestDetail(m)
	d := m.tasks.detail
	d.showProcess = true
	m.rebuildTaskDetail()
	d.follow = false
	d.list.RestoreViewportAnchor(1, 0)
	key := d.entries[1].key
	m.handleTaskKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	m.openSelectedTask()
	if m.tasks.detail != d {
		t.Fatal("returning to the same task lost its reading window")
	}
	m.rebuildTaskDetail()
	index, _ := d.list.ViewportAnchor()
	if d.entries[index].key != key {
		t.Fatal("live refresh moved the reading anchor")
	}
	found := false
	for _, entry := range d.entries {
		if entry.call != nil && entry.call.Name == "read" {
			found = true
			view := ansi.Strip(entry.item.Render(70))
			if !strings.Contains(view, "package auth") {
				t.Fatalf("missing shared read rendering: %s", view)
			}
		}
	}
	if !found {
		t.Fatal("read tool disappeared from task history")
	}
}

func TestTaskOverlayFitsTerminalAndPreservesBackground(t *testing.T) {
	m := openedTaskModel(t)
	for _, size := range [][2]int{{120, 36}, {80, 24}, {40, 12}, {20, 10}, {12, 6}, {6, 3}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		view := m.taskOverlayView("main agent background")
		if lipgloss.Width(view) > size[0] || lipgloss.Height(view) > size[1] {
			t.Fatalf("overlay overflows %v: %dx%d", size, lipgloss.Width(view), lipgloss.Height(view))
		}
		if size[0] >= 60 && !strings.Contains(ansi.Strip(view), "main agent background") {
			t.Fatal("floating panel replaced the background")
		}
	}
}

func TestTaskOverlayApprovalTemporarilyOwnsFocus(t *testing.T) {
	m := openedTaskModel(t)
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventApprovalRequested, Payload: controlruntime.ApprovalView{ID: "approval", ToolName: "shell", SubAgentID: "test-agent"}})
	if m.state != stateConfirming || !m.tasks.open {
		t.Fatal("approval did not suspend observation")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.state == stateConfirming || !m.tasks.open {
		t.Fatal("approval did not restore observer")
	}
}

func TestTaskDetailDefaultsToResultAndExpandsProcess(t *testing.T) {
	m := openedTaskModel(t)
	openTestDetail(m)
	d := m.tasks.detail
	d.attempt = &controlruntime.SubagentAttemptSummary{ID: "attempt-1", Seq: 1, Status: "completed", Result: "最终结论", TranscriptStart: 1, TranscriptEnd: 4}
	d.info.LatestAttempt = cloneTaskAttempt(d.attempt)
	d.info.Status = "completed"
	d.showProcess = false
	m.rebuildTaskDetail()
	if len(d.entries) != 1 || !strings.Contains(ansi.Strip(d.entries[0].item.Render(70)), "最终结论") {
		t.Fatalf("completed detail did not show only final result: %+v", d.entries)
	}
	m.handleTaskKey(tea.KeyPressMsg{Code: 'p', Text: "p"})
	if !d.showProcess || len(d.entries) < 2 {
		t.Fatal("process toggle did not expose committed records")
	}
	foundRead := false
	for _, entry := range d.entries {
		foundRead = foundRead || entry.call != nil && entry.call.ID == "read-1"
	}
	if !foundRead {
		t.Fatal("expanded process lost tool records")
	}
	m.handleTaskKey(tea.KeyPressMsg{Code: 'p', Text: "p"})
	if d.showProcess || len(d.entries) != 1 {
		t.Fatal("result view did not collapse the process")
	}
}

func TestTaskAttemptNavigationKeepsHistoryIsolated(t *testing.T) {
	m := openedTaskModel(t)
	openTestDetail(m)
	d := m.tasks.detail
	first := controlruntime.SubagentAttemptSummary{ID: "first", Seq: 1, Status: "completed", Result: "first result", TranscriptStart: 1, TranscriptEnd: 2}
	second := controlruntime.SubagentAttemptSummary{ID: "second", Seq: 2, Status: "running", TranscriptStart: 3, TranscriptEnd: 4}
	d.attempts.Attempts = []controlruntime.SubagentAttemptSummary{first, second}
	d.info.LatestAttempt = cloneTaskAttempt(&second)
	d.attempt = cloneTaskAttempt(&second)
	if records := d.visibleRecords(); len(records) != 2 || records[0].Sequence != 3 || records[1].Sequence != 4 {
		t.Fatalf("attempt transcript range leaked other runs: %+v", records)
	}
	m.stepTaskAttempt(-1)
	if d.attempt.ID != "first" || d.showProcess || d.currentCursor() != 3 {
		t.Fatalf("older attempt selection failed: %+v cursor=%d", d.attempt, d.currentCursor())
	}
	m.stepTaskAttempt(1)
	if d.attempt.ID != "second" || d.currentCursor() != 5 {
		t.Fatalf("newer attempt selection failed: %+v cursor=%d", d.attempt, d.currentCursor())
	}
	before := len(d.tools)
	m.handleTaskMessage(taskEventMsg{generation: m.tasks.generation, id: d.id, ok: true, event: controlruntime.Event{Type: controlruntime.EventToolStarted, Payload: controlruntime.ToolPayload{CallID: "old-tool", AttemptID: "first", AttemptSeq: 1}}})
	if len(d.tools) != before {
		t.Fatal("stale tool event entered the selected attempt")
	}
	m.handleTaskMessage(taskEventMsg{generation: m.tasks.generation, id: d.id, ok: true, event: controlruntime.Event{Type: controlruntime.EventToolStarted, Payload: controlruntime.ToolPayload{CallID: "legacy-tool"}}})
	m.handleTaskMessage(taskEventMsg{generation: m.tasks.generation, id: d.id, ok: true, event: controlruntime.Event{Type: controlruntime.EventSubAgentEnded, Payload: controlruntime.SubAgentPayload{Status: "failed"}}})
	if len(d.tools) != before || d.attempt.Status != "running" {
		t.Fatal("legacy event polluted an attempt-aware detail")
	}
}

func TestTaskRunningViewKeepsIntermediateTextInsideStatus(t *testing.T) {
	m := openedTaskModel(t)
	openTestDetail(m)
	d := m.tasks.detail
	d.attempt = &controlruntime.SubagentAttemptSummary{ID: "running", Seq: 1, Status: "running", Prompt: "检查当前实现", TranscriptStart: 1, TranscriptEnd: 4}
	d.info.LatestAttempt = cloneTaskAttempt(d.attempt)
	d.showProcess = false
	m.rebuildTaskDetail()
	if len(d.entries) != 1 {
		t.Fatalf("running view should keep intermediate text in the status card: %d entries", len(d.entries))
	}
	if d.processing == nil {
		t.Fatal("running detail is missing the main-style processing card")
	}
	var rendered string
	for _, entry := range d.entries {
		rendered += ansi.Strip(entry.item.Render(70))
	}
	if strings.Contains(rendered, "当前执行") || !strings.Contains(rendered, "检查当前实现") || strings.Contains(rendered, "已找到三个调用点") {
		t.Fatalf("unexpected compact running projection: %s", rendered)
	}
}

func TestProcessingCardStaysVisibleBelowManyToolsAndClearsOnCompletion(t *testing.T) {
	m := openedTaskModel(t)
	openTestDetail(m)
	d := m.tasks.detail
	d.attempt = &controlruntime.SubagentAttemptSummary{ID: "running", Seq: 1, Status: "running", Prompt: "检查当前实现", TranscriptStart: 1, TranscriptEnd: 40}
	d.info.LatestAttempt = cloneTaskAttempt(d.attempt)
	d.info.Status = "running"
	d.info.Phase = "Running go test"
	d.info.TranscriptSeq = 40
	d.showProcess = false
	d.page.Records = nil
	for i := 1; i <= 20; i++ {
		id := fmt.Sprintf("tool-%d", i)
		d.page.Records = append(d.page.Records,
			controlruntime.SubagentRecord{Sequence: i*2 - 1, Role: "assistant", ToolCalls: []controlruntime.SubagentToolCall{{ID: id, Name: "shell", Arguments: `{"command":"go test"}`}}},
			controlruntime.SubagentRecord{Sequence: i * 2, Role: "tool", ToolCallID: id, Content: "ok"},
		)
	}
	m.rebuildTaskDetail()
	view := ansi.Strip(m.taskPanelView(80, 24))
	section := "activity"
	if strings.Contains(view, "changes") {
		section = "changes"
	}
	if d.processing == nil || !strings.Contains(view, section) || !strings.Contains(view, "Running go test") {
		t.Fatalf("main-style processing card is not visible: %s", view)
	}
	if strings.LastIndex(view, section) < strings.LastIndex(view, "检查当前实现") {
		t.Fatal("processing card was placed above the prompt")
	}
	for _, size := range [][2]int{{80, 24}, {40, 12}, {20, 10}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		small := m.taskOverlayView("background")
		if lipgloss.Width(small) > size[0] || lipgloss.Height(small) > size[1] {
			t.Fatalf("processing status overflows at %v", size)
		}
		plain := ansi.Strip(small)
		if size[0] >= 40 && !strings.Contains(plain, "Running go test") || size[0] < 40 && !strings.Contains(plain, "运行中") {
			t.Fatalf("processing status is not visible at %v", size)
		}
	}
	d.attempt.Status = "completed"
	d.attempt.Result = "全部完成"
	d.info.LatestAttempt = cloneTaskAttempt(d.attempt)
	d.info.Status = "completed"
	m.rebuildTaskDetail()
	if d.processing != nil || strings.Contains(ansi.Strip(m.taskPanelView(80, 24)), "activity") || strings.Contains(ansi.Strip(m.taskPanelView(80, 24)), "changes") {
		t.Fatal("processing card remained after completion")
	}
}

func TestLatestAttemptCursorAdvancesWithoutMovingPausedReader(t *testing.T) {
	m := openedTaskModel(t)
	openTestDetail(m)
	d := m.tasks.detail
	d.attempt = &controlruntime.SubagentAttemptSummary{ID: "attempt", Seq: 1, Status: "running", TranscriptStart: 1, TranscriptEnd: 4}
	d.info.LatestAttempt = cloneTaskAttempt(d.attempt)
	d.cursors = []int{5}
	d.loadedCursor = 5
	if got := d.currentCursor(); got != 5 {
		t.Fatalf("initial cursor = %d", got)
	}
	d.attempt.TranscriptEnd = 6
	d.info.LatestAttempt = cloneTaskAttempt(d.attempt)
	d.info.TranscriptSeq = 6
	if got := d.currentCursor(); got != 7 {
		t.Fatalf("advanced cursor = %d, want 7", got)
	}
	page := controlruntime.SubagentHistoryPage{SessionID: "parent", SubagentID: d.id, Before: 5, End: 6, Total: 6, Records: []controlruntime.SubagentRecord{{Sequence: 5, Role: "assistant", Content: "five"}, {Sequence: 6, Role: "assistant", Content: "six"}}}
	d.follow = false
	m.handleTaskMessage(taskHistoryMsg{generation: m.tasks.generation, request: d.request, id: d.id, before: 7, page: page})
	if d.page.End != 4 || d.newRecords != 2 {
		t.Fatalf("in-flight tail refresh moved paused reader: end=%d new=%d", d.page.End, d.newRecords)
	}
	d.follow = true
	m.handleTaskMessage(taskHistoryMsg{generation: m.tasks.generation, request: d.request, id: d.id, before: 7, page: page})
	if d.page.End != 6 || len(d.cursors) != 1 || d.currentCursor() != 7 {
		t.Fatalf("latest attempt did not accept advanced tail: %+v cursors=%v", d.page, d.cursors)
	}
}

func TestAttemptLiveDirtyRefreshAndFollowUseLatestTail(t *testing.T) {
	m := openedTaskModel(t)
	openTestDetail(m)
	d := m.tasks.detail
	d.attempt = &controlruntime.SubagentAttemptSummary{ID: "attempt", Seq: 1, Status: "running", TranscriptStart: 1, TranscriptEnd: 4}
	d.info.LatestAttempt = cloneTaskAttempt(d.attempt)
	d.cursors = []int{5}
	d.follow = true
	m.handleTaskMessage(taskEventMsg{generation: m.tasks.generation, id: d.id, ok: true, event: controlruntime.Event{Type: controlruntime.EventToolPreview, Payload: controlruntime.ToolPayload{CallID: "shell-1", ToolName: "shell", Preview: "live update", AttemptID: "attempt", AttemptSeq: 1}}})
	m.handleTaskMessage(taskTickMsg{generation: m.tasks.generation})
	if d.dirty {
		t.Fatal("attempt live event remained dirty after tick")
	}
	d.follow = false
	d.list.ScrollToBottom()
	m.handleTaskKey(tea.KeyPressMsg{Code: tea.KeyDown})
	if !d.follow {
		t.Fatal("returning to the latest tail did not restore follow")
	}
}

func TestSavedDetailRefreshesSameAttemptOnReopen(t *testing.T) {
	m := openedTaskModel(t)
	openTestDetail(m)
	d := m.tasks.detail
	d.attempt = &controlruntime.SubagentAttemptSummary{ID: "attempt", Seq: 1, Status: "running", TranscriptStart: 1, TranscriptEnd: 4}
	d.info.LatestAttempt = cloneTaskAttempt(d.attempt)
	d.seenTranscriptSeq = 4
	m.handleTaskKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	latest := controlruntime.SubagentAttemptSummary{ID: "attempt", Seq: 1, Status: "completed", Result: "completed while closed", TranscriptStart: 1, TranscriptEnd: 5}
	for i := range m.tasks.items {
		if m.tasks.items[i].ID == d.id {
			m.tasks.items[i].LatestAttempt = &latest
			m.tasks.items[i].Status = "completed"
			m.tasks.items[i].Result = latest.Result
			m.tasks.items[i].TranscriptSeq = 5
		}
	}
	m.openSelectedTask()
	if m.tasks.detail != d || d.attempt.Status != "completed" || d.attempt.Result != latest.Result || d.attempt.TranscriptEnd != 5 || d.currentCursor() != 6 {
		t.Fatalf("saved attempt was not refreshed: %+v", d.attempt)
	}
}

func TestLegacyDetailCanEnterFirstAttempt(t *testing.T) {
	m := openedTaskModel(t)
	openTestDetail(m)
	d := m.tasks.detail
	d.attempt = nil
	d.info.LatestAttempt = &controlruntime.SubagentAttemptSummary{ID: "first", Seq: 1, Status: "completed", TranscriptStart: 1, TranscriptEnd: 4}
	d.newAttempt = true
	m.stepTaskAttempt(1)
	if d.attempt == nil || d.attempt.ID != "first" || d.newAttempt {
		t.Fatalf("legacy detail could not enter first attempt: %+v", d.attempt)
	}
}

func TestHistoryPaginationDoesNotCountSeenPagesAsNew(t *testing.T) {
	m := openedTaskModel(t)
	openTestDetail(m)
	d := m.tasks.detail
	d.attempt = &controlruntime.SubagentAttemptSummary{ID: "attempt", Seq: 1, Status: "completed", TranscriptStart: 1, TranscriptEnd: 150}
	d.info.LatestAttempt = cloneTaskAttempt(d.attempt)
	d.info.TranscriptSeq = 150
	d.seenTranscriptSeq = 150
	d.page.End, d.page.Total = 150, 150
	d.cursors = []int{151}
	d.loadedCursor = 151
	d.follow = false
	page := controlruntime.SubagentHistoryPage{SessionID: "parent", SubagentID: d.id, Before: 1, End: 50, Total: 150, HasMore: true, Records: []controlruntime.SubagentRecord{{Sequence: 50, Role: "assistant", Content: "older"}}}
	m.handleTaskMessage(taskHistoryMsg{generation: m.tasks.generation, request: d.request, id: d.id, before: 51, page: page})
	if d.newRecords != 0 {
		t.Fatalf("older history was reported as %d new records", d.newRecords)
	}
}

func TestTaskSubscriptionRetriesAfterChannelClose(t *testing.T) {
	m := openedTaskModel(t)
	openTestDetail(m)
	s := m.tasks
	m.handleTaskMessage(taskEventMsg{generation: s.generation, id: s.detail.id, ok: false})
	if s.stream != nil || s.subscribeRetryAt.IsZero() {
		t.Fatal("closed subscription did not schedule recovery")
	}
	s.subscribeRetryAt = time.Now().Add(-time.Second)
	_, cmd := m.handleTaskMessage(taskTickMsg{generation: s.generation})
	if cmd == nil || !s.subscribeRetryAt.After(time.Now()) {
		t.Fatal("tick did not resubscribe after the retry deadline")
	}
	stale := make(chan controlruntime.Event)
	defer close(stale)
	m.handleTaskMessage(taskSubscribedMsg{generation: s.generation, request: s.streamRequest - 1, id: s.detail.id, events: stale})
	if s.stream != nil {
		t.Fatal("stale subscription replaced the newer request")
	}
}

func TestCommittedToolsAreRemovedFromLiveOrder(t *testing.T) {
	m := openedTaskModel(t)
	openTestDetail(m)
	d := m.tasks.detail
	d.tools["old-a"] = liveTaskTool{done: true}
	d.tools["old-b"] = liveTaskTool{done: true}
	d.liveOrder = []string{"old-a", "old-b"}
	m.handleTaskMessage(taskHistoryMsg{generation: m.tasks.generation, request: d.request, id: d.id, page: taskTestHistory()})
	if len(d.liveOrder) != 0 {
		t.Fatalf("committed tools remained in live order: %v", d.liveOrder)
	}
}

func TestSlashOpensAttemptListAndEnterSelects(t *testing.T) {
	m := openedTaskModel(t)
	openTestDetail(m)
	d := m.tasks.detail
	first := controlruntime.SubagentAttemptSummary{ID: "first", Seq: 1, Status: "completed", Result: "first result", TranscriptStart: 1, TranscriptEnd: 2}
	second := controlruntime.SubagentAttemptSummary{ID: "second", Seq: 2, Status: "completed", Result: "second result", TranscriptStart: 3, TranscriptEnd: 4}
	d.attempts.Attempts = []controlruntime.SubagentAttemptSummary{first, second}
	d.info.LatestAttempt = cloneTaskAttempt(&second)
	d.attempt = cloneTaskAttempt(&second)
	m.handleTaskKey(tea.KeyPressMsg{Code: '/', Text: "/"})
	if !d.showAttempts {
		t.Fatal("slash did not open attempt list")
	}
	view := ansi.Strip(m.taskPanelView(80, 24))
	if !strings.Contains(view, "第 1 轮") || !strings.Contains(view, "第 2 轮") || !strings.Contains(view, "first result") {
		t.Fatalf("attempt list is incomplete: %s", view)
	}
	m.handleTaskKey(tea.KeyPressMsg{Code: tea.KeyDown})
	m.handleTaskKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if d.showAttempts || d.attempt.ID != "first" {
		t.Fatalf("attempt selection failed: show=%v attempt=%+v", d.showAttempts, d.attempt)
	}
	m.handleTaskKey(tea.KeyPressMsg{Code: '/', Text: "/"})
	m.handleTaskKey(tea.KeyPressMsg{Code: '/', Text: "/"})
	if d.showAttempts {
		t.Fatal("second slash did not close attempt list")
	}
}

func TestOpenTaskPreservesInflightListRefresh(t *testing.T) {
	m := openedTaskModel(t)
	s := m.tasks
	s.listLoading = true
	s.listDirty = false
	generation := s.generation
	m.openSelectedTask()
	if s.listLoading || !s.listDirty {
		t.Fatalf("opening detail lost refresh state: loading=%v dirty=%v", s.listLoading, s.listDirty)
	}
	before := s.items
	m.handleTaskMessage(taskListMsg{generation: generation, request: s.listRequest, page: controlruntime.SubagentPage{Items: []controlruntime.SubagentSummary{{ID: "stale"}}}})
	if len(s.items) != len(before) || s.items[0].ID != before[0].ID || !s.listDirty {
		t.Fatal("stale list response was applied or cleared the dirty refresh")
	}
	if !m.shouldRefreshTaskList(time.Now().Add(time.Second)) {
		t.Fatal("invalidated list refresh was not rescheduled")
	}
}

func TestTaskAttemptsFailureIsVisibleAndRetriesSamePage(t *testing.T) {
	for _, before := range []int{0, 7} {
		m, err := NewModel(&taskAttemptFakeRuntime{})
		if err != nil {
			t.Fatal(err)
		}
		m.toggleTasks()
		m.handleTaskMessage(taskListMsg{generation: m.tasks.generation, request: m.tasks.listRequest, page: taskTestPage()})
		m.tasks.selected = "test-agent"
		openTestDetail(m)
		d := m.tasks.detail
		d.showAttempts = true
		if before > 0 {
			d.attempts.Attempts = []controlruntime.SubagentAttemptSummary{{ID: "existing", Seq: 2}}
			d.attempts.Before = before
			d.attempts.HasMore = true
		}
		d.attemptRequest++
		d.attemptsLoading = true
		m.handleTaskMessage(taskAttemptsMsg{generation: m.tasks.generation, request: d.attemptRequest, id: d.id, before: before, err: errors.New("boom")})
		view := ansi.Strip(m.taskAttemptsView(100))
		if d.attemptsErr != "boom" || !strings.Contains(view, "boom") || strings.Contains(view, "旧版记录") {
			t.Fatalf("attempt error before=%d view=%q err=%q", before, view, d.attemptsErr)
		}
		if cmd := m.handleTaskKey(tea.KeyPressMsg{Code: 'r', Text: "r"}); cmd == nil {
			t.Fatalf("attempt retry before=%d was not scheduled", before)
		}
		if d.attemptsRetryBefore != before || !d.attemptsLoading {
			t.Fatalf("attempt retry before=%d state=%+v", before, d)
		}
	}
}

func TestSlashAttemptListHandlesLegacyAndFirstAttempt(t *testing.T) {
	m := openedTaskModel(t)
	openTestDetail(m)
	d := m.tasks.detail
	d.attempt = nil
	d.info.LatestAttempt = nil
	d.attempts.Attempts = nil
	m.handleTaskKey(tea.KeyPressMsg{Code: '/', Text: "/"})
	if !strings.Contains(ansi.Strip(m.taskPanelView(80, 24)), "旧版记录，没有独立执行轮次") {
		t.Fatal("legacy attempt list did not explain missing rounds")
	}
	d.info.LatestAttempt = &controlruntime.SubagentAttemptSummary{ID: "first", Seq: 1, Status: "completed", TranscriptStart: 1, TranscriptEnd: 4}
	m.handleTaskKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if d.showAttempts || d.attempt == nil || d.attempt.ID != "first" {
		t.Fatalf("legacy detail could not select first attempt: %+v", d.attempt)
	}
}

func TestToolResultFromNewerPageMatchesOlderCall(t *testing.T) {
	m := openedTaskModel(t)
	openTestDetail(m)
	d := m.tasks.detail
	d.attempt = nil
	d.info.LatestAttempt = nil
	d.info.Status = "completed"
	d.info.TranscriptSeq = 2
	d.showProcess = true
	d.follow = true
	m.handleTaskMessage(taskHistoryMsg{generation: m.tasks.generation, request: d.request, id: d.id, page: controlruntime.SubagentHistoryPage{SessionID: "parent", SubagentID: d.id, Before: 2, End: 2, Total: 2, Records: []controlruntime.SubagentRecord{{Sequence: 2, Role: "tool", ToolCallID: "call-1", Content: "complete output"}}}})
	d.follow = false
	m.handleTaskMessage(taskHistoryMsg{generation: m.tasks.generation, request: d.request, id: d.id, before: 2, page: controlruntime.SubagentHistoryPage{SessionID: "parent", SubagentID: d.id, Before: 1, End: 1, Total: 2, Records: []controlruntime.SubagentRecord{{Sequence: 1, Role: "assistant", ToolCalls: []controlruntime.SubagentToolCall{{ID: "call-1", Name: "read", Arguments: "{}"}}}}}})
	found := false
	for _, entry := range d.entries {
		if entry.call != nil && entry.call.ID == "call-1" {
			found = true
			if entry.result.Content != "complete output" || strings.Contains(ansi.Strip(entry.item.Render(70)), "结果在后续记录中") {
				t.Fatalf("cross-page result was not matched: %+v", entry.result)
			}
		}
	}
	if !found {
		t.Fatal("older tool call disappeared")
	}
}

func TestMissingToolResultOnlyWarnsAtCommittedEnd(t *testing.T) {
	m := openedTaskModel(t)
	openTestDetail(m)
	d := m.tasks.detail
	d.attempt = nil
	d.info.Status = "completed"
	d.info.TranscriptSeq = 1
	d.showProcess = true
	d.committedResults = make(map[string]controlruntime.SubagentRecord)
	d.page.Records = []controlruntime.SubagentRecord{{Sequence: 1, Role: "assistant", ToolCalls: []controlruntime.SubagentToolCall{{ID: "missing", Name: "shell", Arguments: "{}"}}}}
	m.rebuildTaskDetail()
	if !strings.Contains(ansi.Strip(d.entries[0].item.Render(70)), "未记录到工具结果") {
		t.Fatal("genuinely missing final tool result was not reported")
	}
}

func TestCompletedDetailShowsMainAgentPromptAndResult(t *testing.T) {
	m := openedTaskModel(t)
	openTestDetail(m)
	d := m.tasks.detail
	d.attempt = &controlruntime.SubagentAttemptSummary{ID: "attempt", Seq: 1, Status: "completed", Prompt: "修复认证过期校验", Result: "修复已完成", TranscriptStart: 1, TranscriptEnd: 1}
	d.info.LatestAttempt = cloneTaskAttempt(d.attempt)
	d.showProcess = false
	d.loading = false
	d.page.Records = nil
	m.rebuildTaskDetail()
	if len(d.entries) != 2 {
		t.Fatalf("completed detail should contain prompt and result: %d", len(d.entries))
	}
	prompt := ansi.Strip(d.entries[0].item.Render(70))
	result := ansi.Strip(d.entries[1].item.Render(70))
	if !strings.Contains(prompt, "修复认证过期校验") || !strings.Contains(result, "修复已完成") {
		t.Fatalf("prompt or result missing: prompt=%q result=%q", prompt, result)
	}
}

func TestFinalResultDoesNotFlashTruncatedPreviewWhileLoading(t *testing.T) {
	m := openedTaskModel(t)
	openTestDetail(m)
	d := m.tasks.detail
	d.attempt = &controlruntime.SubagentAttemptSummary{ID: "attempt", Seq: 1, Status: "completed", Result: "truncated preview", TranscriptStart: 1, TranscriptEnd: 4}
	d.info.LatestAttempt = cloneTaskAttempt(d.attempt)
	d.showProcess = false
	d.historyLoaded = false
	d.page.Records = nil
	cmd := m.loadTaskHistory(d.currentCursor())
	if cmd == nil || !d.loading {
		t.Fatal("result history was not requested")
	}
	if len(d.entries) != 1 {
		t.Fatalf("unexpected loading entries: %d", len(d.entries))
	}
	rendered := ansi.Strip(d.entries[0].item.Render(70))
	if !strings.Contains(rendered, "正在加载完整结果") || strings.Contains(rendered, "truncated preview") {
		t.Fatalf("truncated preview flashed while loading: %s", rendered)
	}
}

func TestFinalResultUsesCompleteSubmittedContent(t *testing.T) {
	m := openedTaskModel(t)
	openTestDetail(m)
	d := m.tasks.detail
	full := strings.Repeat("a", 5000) + "_END"
	arguments, err := json.Marshal(map[string]any{"summary": full})
	if err != nil {
		t.Fatal(err)
	}
	d.attempt = &controlruntime.SubagentAttemptSummary{ID: "attempt", Seq: 1, Status: "completed", Result: "truncated preview", TranscriptStart: 1, TranscriptEnd: 1}
	d.info.LatestAttempt = cloneTaskAttempt(d.attempt)
	d.info.Status = "completed"
	d.showProcess = false
	d.loading = false
	d.page.Records = []controlruntime.SubagentRecord{{Sequence: 1, Role: "assistant", ToolCalls: []controlruntime.SubagentToolCall{{ID: "submit", Name: "nekocode_submit_result", Arguments: string(arguments)}}}}
	m.rebuildTaskDetail()
	if len(d.entries) != 1 {
		t.Fatalf("unexpected result entries: %d", len(d.entries))
	}
	rendered := ansi.Strip(d.entries[0].item.Render(70))
	if !strings.Contains(rendered, "_END") || len(rendered) < 5000 {
		t.Fatal("final result was rendered from the truncated preview")
	}
}

func TestTaskNameDoesNotUsePrompt(t *testing.T) {
	info := controlruntime.SubagentSummary{ID: "12345678-abcd", Title: "修复认证", Profile: "coder", Prompt: "这是一段不应该作为任务名的详细提示词"}
	if label := taskLabel(info); !strings.Contains(label, "修复认证") || strings.Contains(label, "详细提示词") {
		t.Fatalf("task label uses prompt instead of title: %s", label)
	}
	info.Title = ""
	if label := taskLabel(info); !strings.Contains(label, "coder") || strings.Contains(label, "详细提示词") {
		t.Fatalf("legacy fallback uses prompt: %s", label)
	}
}

func TestTaskMetricsAndTokenFormatting(t *testing.T) {
	info := controlruntime.SubagentSummary{RunCount: 2, LatestAttempt: &controlruntime.SubagentAttemptSummary{CacheHitTokens: 80, CacheMissTokens: 20, ContextTokens: 1500, ContextWindow: 200000, ToolUseCount: 7}}
	if got := taskMetrics(info); got != "轮次 2 · 缓存 80% · 上下文 1.5K/200K · 工具 7" {
		t.Fatalf("metrics = %q", got)
	}
	if got := taskMetrics(controlruntime.SubagentSummary{RunCount: 2}); got != "轮次 2 · 旧版无明细" {
		t.Fatalf("legacy metrics = %q", got)
	}
	if formatTaskTokens(1000) != "1K" || formatTaskTokens(1500) != "1.5K" || formatTaskTokens(2000000) != "2M" {
		t.Fatal("token formatting is incorrect")
	}
}

func TestTaskListDisplaysAttemptMetrics(t *testing.T) {
	m := openedTaskModel(t)
	m.tasks.items[0].RunCount = 2
	m.tasks.items[0].LatestAttempt = &controlruntime.SubagentAttemptSummary{CacheHitTokens: 80, CacheMissTokens: 20, ContextTokens: 1500, ContextWindow: 200000, ToolUseCount: 7}
	view := ansi.Strip(m.taskPanelView(80, 24))
	if !strings.Contains(view, "轮次 2 · 缓存 80% · 工具 7") || strings.Contains(view, "上下文 1.5K/200K") {
		t.Fatalf("compact task list metrics are incorrect: %s", view)
	}
	wide := ansi.Strip(m.taskPanelView(120, 24))
	if !strings.Contains(wide, "轮次 2 · 缓存 80% · 上下文 1.5K/200K · 工具 7") {
		t.Fatalf("wide task list is missing context metrics: %s", wide)
	}
	m.tasks.items[0].Phase = ""
	narrow := ansi.Strip(m.taskPanelView(40, 12))
	if strings.Contains(narrow, "缓存 80%") {
		t.Fatalf("narrow list lost compact layout: %s", narrow)
	}
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "verify-a") {
			if !strings.Contains(line, "轮次 2") {
				t.Fatalf("metrics moved onto a separate row: %q", line)
			}
		}
	}
}

func TestTaskPausedDetailIgnoresInflightRefresh(t *testing.T) {
	m := openedTaskModel(t)
	openTestDetail(m)
	d := m.tasks.detail
	d.follow = false
	page := taskTestHistory()
	page.End = 5
	page.Total = 5
	page.Records = append(page.Records, controlruntime.SubagentRecord{Sequence: 5, Role: "assistant", Content: "new text"})
	m.handleTaskMessage(taskHistoryMsg{generation: m.tasks.generation, request: d.request, id: d.id, page: page})
	if d.page.End != 4 || d.newRecords != 1 {
		t.Fatal("in-flight refresh changed a paused history window")
	}
}

func TestTaskOutputRequestsCannotReopenOrOverwriteInspector(t *testing.T) {
	m := openedTaskModel(t)
	m.Runtime = &taskOutputFakeRuntime{}
	openTestDetail(m)
	d := m.tasks.detail
	d.page.Records[2].OutputPath = "stored-output"
	m.rebuildTaskDetail()
	for i, entry := range d.entries {
		if entry.call != nil && entry.call.Name == "read" {
			d.list.RestoreViewportAnchor(i, 0)
			break
		}
	}
	oldCmd := m.inspectVisibleTaskTool()
	oldResponse := oldCmd().(taskOutputMsg)
	m.handleTaskKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	newCmd := m.inspectVisibleTaskTool()
	m.handleTaskMessage(oldResponse)
	if d.output.loaded {
		t.Fatal("stale output response overwrote a reopened inspector")
	}
	m.handleTaskMessage(newCmd())
	if !d.output.loaded {
		t.Fatal("current output response was lost")
	}
	d.inspector.ScrollToBottom()
	cmd := m.handleTaskKey(tea.KeyPressMsg{Code: tea.KeyPgDown})
	if cmd == nil {
		t.Fatal("cannot page original output")
	}
	m.handleTaskMessage(cmd())
	if d.output.page.Offset != 65536 || d.output.page.HasMore {
		t.Fatal("wrong output page")
	}
}

func TestTaskOutputLoadingResumesAfterClosingOverlay(t *testing.T) {
	m := openedTaskModel(t)
	m.Runtime = &taskOutputFakeRuntime{}
	openTestDetail(m)
	d := m.tasks.detail
	d.page.Records[2].OutputPath = "stored-output"
	m.rebuildTaskDetail()
	for i, entry := range d.entries {
		if entry.call != nil && entry.call.Name == "read" {
			d.list.RestoreViewportAnchor(i, 0)
			break
		}
	}
	m.inspectVisibleTaskTool()
	if !d.output.loading {
		t.Fatal("output load did not start")
	}
	m.closeTasks()
	if d.output.loading || !d.output.resume {
		t.Fatal("close left an orphan loading request")
	}
	m.toggleTasks()
	if !d.output.loading || d.output.resume {
		t.Fatal("reopen did not resume the interrupted output request")
	}
}

func TestTaskOverlayTerminalPreview(t *testing.T) {
	if os.Getenv("NEKOCODE_TASK_INTERACTIVE") == "" {
		t.Skip("interactive preview not requested")
	}
	m := openedTaskModel(t)
	if os.Getenv("NEKOCODE_TASK_PREVIEW_HUNDRED") == "1" {
		m.Runtime = &hundredTaskRuntime{}
		page := hundredTaskTestPage()
		m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventRunStarted})
		for _, item := range page.Items {
			kind := controlruntime.EventSubAgentStarted
			if item.Status == "completed" {
				kind = controlruntime.EventSubAgentEnded
			}
			m.handleRuntimeEvent(controlruntime.Event{Type: kind, Payload: controlruntime.SubAgentPayload{ID: item.ID, Status: item.Status}})
		}
		m.handleTaskMessage(taskListMsg{generation: m.tasks.generation, request: m.tasks.listRequest, page: page})
		// Keep the manual preview fixture stable; automated tests exercise refresh.
		m.tasks.listDirty = false
		m.tasks.listQueriedAt = time.Now().Add(time.Hour)
	}
	m.Input.Clear()
	if _, err := tea.NewProgram(m).Run(); err != nil {
		t.Fatal(err)
	}
}

func hundredTaskTestPage() controlruntime.SubagentPage {
	page := controlruntime.SubagentPage{SessionID: "parent", Total: 100}
	for i := 0; i < 100; i++ {
		id := fmt.Sprintf("agent-%03d", i)
		if i == 0 {
			id = "test-agent"
		}
		status := "completed"
		if i < 8 {
			status = "running"
		} else if i < 40 {
			status = "queued"
		}
		page.Items = append(page.Items, controlruntime.SubagentSummary{ID: id, SessionID: "parent", Profile: "explore", Prompt: fmt.Sprintf("检查样例文件 %d 的接口兼容性", i), Status: status, Recorded: status != "queued", TranscriptSeq: 4})
	}
	return page
}

func TestHundredTaskOverlayResizesAndPreservesMainDraft(t *testing.T) {
	m := openedTaskModel(t)
	m.handleTaskMessage(taskListMsg{generation: m.tasks.generation, request: m.tasks.listRequest, page: hundredTaskTestPage()})
	draft := m.Input.Value()
	for _, size := range [][2]int{{120, 36}, {80, 24}, {40, 12}, {20, 10}, {80, 24}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m.handleTaskKey(tea.KeyPressMsg{Code: tea.KeyEnd})
		if m.tasks.selected != "agent-099" {
			t.Fatal("cannot navigate to last of 100 tasks")
		}
		m.handleTaskKey(tea.KeyPressMsg{Code: tea.KeyHome})
		openTestDetail(m)
		view := m.taskOverlayView("main conversation")
		if lipgloss.Width(view) > size[0] || lipgloss.Height(view) > size[1] {
			t.Fatal("large task detail overflowed resized terminal")
		}
		m.handleTaskKey(tea.KeyPressMsg{Code: tea.KeyEscape})
		m.toggleTasks()
		m.toggleTasks()
		if m.Input.Value() != draft || len(m.tasks.items) != 100 {
			t.Fatal("overlay lost main draft or task rows")
		}
	}
}

func TestTaskOverlaySnapshots(t *testing.T) {
	dir := os.Getenv("NEKOCODE_TASK_SNAPSHOTS")
	if dir == "" {
		t.Skip("snapshot export not requested")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	m := openedTaskModel(t)
	for _, detail := range []bool{false, true} {
		if detail {
			openTestDetail(m)
		}
		for _, size := range [][2]int{{100, 30}, {40, 12}} {
			m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			name := fmt.Sprintf("tasks-%t-%dx%d.txt", detail, size[0], size[1])
			if err := os.WriteFile(filepath.Join(dir, name), []byte(ansi.Strip(m.taskOverlayView("主 agent 继续执行…"))), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestToolInspectorReceivesErrorStatusWithUnchangedOutput(t *testing.T) {
	m := openedTaskModel(t)
	openTestDetail(m)
	d := m.tasks.detail
	d.showProcess = true
	m.rebuildTaskDetail()
	for i, entry := range d.entries {
		if entry.call == nil {
			continue
		}
		d.list.RestoreViewportAnchor(i, 0)
		m.inspectVisibleTaskTool()
		if d.inspectedEntry == nil {
			t.Fatal("missing inspector")
		}
		d.entries[i].result.IsError = true
		m.refreshTaskInspector()
		if !d.inspectedEntry.result.IsError {
			t.Fatal("tool failure status ignored when output was unchanged")
		}
		return
	}
	t.Fatal("missing tool entry")
}
