package tui

import (
	tea "charm.land/bubbletea/v2"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"charm.land/bubbles/v2/spinner"

	"github.com/charmbracelet/x/ansi"
	"nekocode/interaction/tui/components/block"
	"nekocode/interaction/tui/components/processing"
	controlruntime "nekocode/runtime"
)

func TestTaskStatusShowsMainProcessingSections(t *testing.T) {
	m := openedTaskModel(t)
	openTestDetail(m)
	d := m.tasks.detail
	calls := []controlruntime.SubagentToolCall{{ID: "edit", Name: "edit", Arguments: `{"path":"main.go","old_string":"a","new_string":"b"}`}}
	for i := 0; i < 5; i++ {
		calls = append(calls, controlruntime.SubagentToolCall{ID: fmt.Sprint(i), Name: "read", Arguments: `{"path":"main.go"}`})
	}
	d.page.Records = []controlruntime.SubagentRecord{{Sequence: 1, Role: "user", Content: "inspect"}, {Sequence: 2, Role: "assistant", Content: ".", ToolCalls: calls}}
	for i, call := range calls {
		d.page.Records = append(d.page.Records, controlruntime.SubagentRecord{Sequence: i + 3, Role: "tool", ToolCallID: call.ID, Content: "ok"})
	}
	d.page.End, d.page.Total = 8, 8
	sendTaskOutput(m, 1, controlruntime.SubAgentOutput{ID: d.id, MessageSeq: 9, Kind: "message", Text: "正在核对结果", Reasoning: "检查修改证据"})
	m.handleTaskMessage(taskFrameMsg{generation: m.tasks.generation, request: m.tasks.streamRequest, id: d.id})
	if d.processing == nil || d.processing.OutputText() != "正在核对结果" || d.processing.ThinkingText() != "检查修改证据" {
		t.Fatal("task text and reasoning bypass the shared processing card")
	}
	m.taskPanelView(80, 24)
	view := taskScrolledContent(d)
	for _, section := range []string{"activity", "changes", "output", "thinking"} {
		if !strings.Contains(view, section) {
			t.Fatalf("missing %s section: %s", section, view)
		}
	}
	w, h := m.taskSize()
	m.taskPanelView(w, h)
	compact := taskScrolledContent(d)
	for _, section := range []string{"activity", "changes", "output", "thinking"} {
		if !strings.Contains(compact, section) {
			t.Fatalf("real overlay lost %s: %s", section, compact)
		}
	}
	lines := strings.Split(ansi.Strip(d.processing.Render(d.list.Width())), "\n")
	for i, line := range lines {
		if strings.Contains(line, d.info.Phase) && strings.Contains(line, "...") && (i+1 >= len(lines) || !strings.Contains(lines[i+1], "└")) {
			t.Fatal("processing card bottom border was clipped")
		}
	}
	if len(d.entries) != 2 {
		t.Fatalf("intermediate text became independent messages: %d", len(d.entries))
	}
}

func TestTaskProcessOmitsDotOnlyAssistantMessages(t *testing.T) {
	m := openedTaskModel(t)
	openTestDetail(m)
	d := m.tasks.detail
	d.showProcess = true
	d.page.Records[1].Content = ".\n...\n   "
	d.page.Records[1].Reasoning = ""
	d.page.Records[2].Content = "."
	m.rebuildTaskDetail()
	for _, entry := range d.entries {
		if entry.key == "legacy:record:2" {
			t.Fatal("dot-only intermediate assistant message was rendered")
		}
		if entry.call != nil && entry.call.ID == "read-1" && entry.result.Content != "." {
			t.Fatal("real tool output was filtered")
		}
	}
	sendTaskOutput(m, 1, controlruntime.SubAgentOutput{ID: d.id, MessageSeq: 5, Kind: "text_delta", Text: "."})
	m.handleTaskMessage(taskFrameMsg{generation: m.tasks.generation, request: m.tasks.streamRequest, id: d.id})
	for _, entry := range d.entries {
		if entry.key == "legacy:record:5" {
			t.Fatal("dot-only live assistant message was rendered")
		}
	}
}

func TestTaskStatusShowsOnlySuccessfulTaskListUpdates(t *testing.T) {
	records := []controlruntime.SubagentRecord{{Role: "assistant", ToolCalls: []controlruntime.SubagentToolCall{{ID: "todo", Name: "todo_write", Arguments: `{"todos":[{"content":"验证接口","status":"in_progress"}]}`}}}}
	results := map[string]controlruntime.SubagentRecord{"todo": {Role: "tool", ToolCallID: "todo"}}
	if text := taskProcessingTodos(records, results); !strings.Contains(text, "▸ 验证接口") {
		t.Fatal("missing tasks section data")
	}
	results["todo"] = controlruntime.SubagentRecord{IsError: true}
	if taskProcessingTodos(records, results) != "" {
		t.Fatal("failed todo update was shown as applied")
	}
}

func TestTaskAndMainStatusRenderIdentically(t *testing.T) {
	m := openedTaskModel(t)
	openTestDetail(m)
	d := m.tasks.detail
	calls := []controlruntime.SubagentToolCall{
		{ID: "read", Name: "read", Arguments: `{"path":"main.go"}`},
		{ID: "process", Name: "process", Arguments: `{"action":"list"}`},
		{ID: "edit", Name: "edit", Arguments: `{"path":"main.go","old_string":"a","new_string":"b"}`},
	}
	d.page.Records = []controlruntime.SubagentRecord{{Sequence: 1, Role: "assistant", Content: ".", ToolCalls: calls}}
	m.Messages.SetProcessing(true)
	for i, call := range calls {
		payload := controlruntime.ToolPayload{CallID: call.ID, ToolName: call.Name, Args: call.Arguments, Output: "ok"}
		m.applyRuntimeToolEvent(controlruntime.Event{Type: controlruntime.EventToolStarted, Payload: payload})
		m.applyRuntimeToolEvent(controlruntime.Event{Type: controlruntime.EventToolCompleted, Payload: payload})
		d.page.Records = append(d.page.Records, controlruntime.SubagentRecord{Sequence: i + 2, Role: "tool", ToolCallID: call.ID, Content: payload.Output})
	}
	d.page.End, d.page.Total = 4, 4
	sendTaskOutput(m, 1, controlruntime.SubAgentOutput{ID: d.id, MessageSeq: 5, Kind: "message", Text: "核对结果", Reasoning: "检查证据"})
	m.rebuildTaskDetail()
	m.Messages.ProcessStreamText("核对结果")
	m.Messages.ProcessThinkingText("检查证据")
	m.Messages.UpdateProcessing(func(p *processing.ProcessingItem) {
		p.SetStatusText(d.info.Phase)
		p.SetSubagentSummary("当前执行")
		for _, width := range []int{40, 80, 120} {
			if got, want := d.processing.Render(width), p.Render(width); got != want {
				t.Fatalf("status differs at width %d: task=%q main=%q", width, got, want)
			}
			for _, height := range []int{6, 12, 40} {
				if d.processing.RenderWithin(width, height) != p.RenderWithin(width, height) {
					t.Fatalf("bounded status differs at %dx%d", width, height)
				}
			}
		}
	})
}

func TestTaskInspectorPreservesSavedToolArguments(t *testing.T) {
	m := openedTaskModel(t)
	openTestDetail(m)
	d := m.tasks.detail
	call := controlruntime.SubagentToolCall{ID: "process", Name: "process", Arguments: `{"action":"wait","task_id":"job"}`}
	d.page.Records = []controlruntime.SubagentRecord{{Role: "assistant", ToolCalls: []controlruntime.SubagentToolCall{call}}}
	d.tools[call.ID] = liveTaskTool{done: true, payload: controlruntime.ToolPayload{}}
	got, _ := taskToolSnapshot(d, call)
	if got.Arguments != call.Arguments {
		t.Fatalf("inspector discarded saved parameters: %q", got.Arguments)
	}
}

func TestTaskStatusFollowsShortPrompt(t *testing.T) {
	m := openedTaskModel(t)
	openTestDetail(m)
	d := m.tasks.detail
	d.info.Prompt = "SHORT_PROMPT"
	m.rebuildTaskDetail()
	for _, height := range []int{24, 40} {
		m.taskPanelView(80, height)
		d.follow = false
		d.list.ScrollToTop()
		lines := strings.Split(ansi.Strip(m.taskPanelView(80, height)), "\n")
		prompt, card := -1, -1
		for i, line := range lines {
			if strings.Contains(line, "SHORT_PROMPT") {
				prompt = i
			}
			if strings.Contains(line, "┌") && card < 0 {
				card = i
			}
		}
		if prompt < 0 || card <= prompt || card-prompt > 3 {
			t.Fatalf("status detached from prompt at height %d: prompt=%d card=%d", height, prompt, card)
		}
	}
}

func TestTaskStatusSpinnerRefreshesCachedListWithoutMovingReader(t *testing.T) {
	for _, follow := range []bool{true, false} {
		t.Run(fmt.Sprintf("follow=%v", follow), func(t *testing.T) {
			m := openedTaskModel(t)
			openTestDetail(m)
			d := m.tasks.detail
			d.follow = follow
			d.processing.SetSpinnerView(m.Spinner.View())
			// Warm the history cache before the animation advances.
			m.taskPanelView(80, 24)
			// Keep the animated row visible even when automatic following is paused.
			d.list.ScrollToBottom()
			before := d.list.Render()
			index, line := d.list.ViewportAnchor()
			previousSpinner := m.Spinner.View()
			m.handleSpinnerTick(spinner.TickMsg{})
			if m.Spinner.View() == previousSpinner {
				t.Fatal("test tick did not advance the spinner")
			}
			after := d.list.Render()
			if after == before || !strings.Contains(after, m.Spinner.View()) {
				t.Fatal("status animation stayed frozen in the history render cache")
			}
			gotIndex, gotLine := d.list.ViewportAnchor()
			if gotIndex != index || gotLine != line {
				t.Fatal("status animation moved the reader")
			}
		})
	}
}

func TestTaskStatusKeepsAllSectionsInScrollableContent(t *testing.T) {
	m := openedTaskModel(t)
	openTestDetail(m)
	d := m.tasks.detail
	d.processing.SetBlocks([]block.ContentBlock{
		{Type: block.BlockTool, ToolName: "read", ToolArgs: "main.go", Done: true},
		{Type: block.BlockTool, ToolName: "edit", ToolArgs: "main.go", Content: "updated", Done: true},
	})
	d.processing.SetTodos("Tasks 0/1\n▸ 检查结果")
	d.processing.SetStreamText("输出内容", "推理内容")
	for _, height := range []int{10, 16, 24} {
		m.taskPanelView(80, height)
		card := taskScrolledContent(d)
		for _, section := range []string{"activity", "changes", "tasks", "output", "thinking", "输出内容", "推理内容"} {
			if !strings.Contains(card, section) {
				t.Fatalf("scrollable card lost %s at height %d: %s", section, height, card)
			}
		}
	}
}

// Collect content as a reader scrolls, including sections outside the viewport.
func taskScrolledContent(d *taskDetail) string {
	index, line := d.list.ViewportAnchor()
	defer d.list.RestoreViewportAnchor(index, line)
	d.list.ScrollToTop()
	var out strings.Builder
	for i := 0; i <= d.list.TotalContentHeight(); i++ {
		out.WriteString(d.list.Render())
		out.WriteByte('\n')
		if d.list.AtBottom() {
			break
		}
		d.list.ScrollBy(1)
	}
	return ansi.Strip(out.String())
}

func TestTaskStatusRetainsOutputAcrossToolCallsAndEmptyStreamReset(t *testing.T) {
	m := openedTaskModel(t)
	openTestDetail(m)
	d := m.tasks.detail
	d.showProcess = false
	d.page.Records = []controlruntime.SubagentRecord{
		{Sequence: 1, Role: "assistant", Content: "已找到配置入口，继续检查调用链", ToolCalls: []controlruntime.SubagentToolCall{{ID: "read", Name: "read", Arguments: `{"path":"main.go"}`}}},
		{Sequence: 2, Role: "tool", ToolCallID: "read", Content: "file contents"},
		{Sequence: 3, Role: "assistant", Content: ".", ToolCalls: []controlruntime.SubagentToolCall{{ID: "grep", Name: "grep", Arguments: `{"pattern":"config"}`}}},
	}
	d.page.End, d.page.Total = 3, 3
	m.rebuildTaskDetail()
	for _, kind := range []string{"reset", "reasoning_delta", "text_delta"} {
		text := ""
		if kind == "reasoning_delta" {
			text = "检查边界条件"
		}
		if kind == "text_delta" {
			text = "调用链检查完成"
		}
		sendTaskOutput(m, uint64(d.eventSeq+1), controlruntime.SubAgentOutput{ID: d.id, MessageSeq: 4, Kind: kind, Text: text})
		m.handleTaskMessage(taskFrameMsg{generation: m.tasks.generation, request: m.tasks.streamRequest, id: d.id})
		want := "已找到配置入口，继续检查调用链"
		if kind == "text_delta" {
			want = text
		}
		if got := d.processing.OutputText(); got != want {
			t.Fatalf("output lost at %s: got %q want %q", kind, got, want)
		}
		m.taskPanelView(80, 24)
		visible := taskScrolledContent(d)
		if !strings.Contains(visible, "output") || !strings.Contains(visible, want) {
			t.Fatalf("compact task hid intermediate output at %s: %s", kind, visible)
		}
	}
}

func TestTaskDetailOwnsAnimationClockWhenForegroundIsIdle(t *testing.T) {
	m := openedTaskModel(t)
	openTestDetail(m)
	m.state = stateReady
	d := m.tasks.detail
	m.closeTasks()
	command := m.toggleTasks()
	batch, ok := command().(tea.BatchMsg)
	if !ok {
		t.Fatal("missing task commands")
	}
	var animation tea.Msg
	for _, cmd := range batch {
		msg := cmd()
		if msg != nil && reflect.TypeOf(msg).Name() == "taskAnimationMsg" {
			animation = msg
			break
		}
	}
	if animation == nil {
		t.Fatal("task overlay has no independent animation clock")
	}
	d.processing.SetSpinnerView(m.Spinner.View())
	m.taskPanelView(80, 24)
	before := m.Spinner.View()
	_, next := m.Update(animation)
	if next == nil || m.Spinner.View() == before {
		t.Fatal("idle foreground froze task animation")
	}
	after := d.list.Render()
	if !strings.Contains(after, m.Spinner.View()) {
		t.Fatal("task animation frame stayed in render cache")
	}
	m.closeTasks()
	_, next = m.Update(animation)
	if next != nil {
		t.Fatal("closed overlay kept animation alive")
	}
}

func TestTaskAnimationIgnoresOldGenerationAndAvoidsDoubleForegroundClock(t *testing.T) {
	m := openedTaskModel(t)
	openTestDetail(m)
	generation := m.tasks.generation
	m.state = stateProcessing
	before := m.Spinner.View()
	_, next := m.Update(taskAnimationMsg{generation: generation})
	if next == nil || m.Spinner.View() != before {
		t.Fatal("task clock duplicated foreground animation")
	}
	m.state = stateReady
	_, next = m.Update(taskAnimationMsg{generation: generation - 1})
	if next != nil || m.Spinner.View() != before {
		t.Fatal("stale overlay animated current detail")
	}
}

func TestEnteringTaskDetailRestartsAnimationAfterGenerationChange(t *testing.T) {
	m := openedTaskModel(t)
	generation := m.tasks.generation
	command := m.openSelectedTask()
	if m.tasks.generation == generation {
		t.Fatal("test did not navigate to a new detail generation")
	}
	batch, ok := command().(tea.BatchMsg)
	if !ok {
		t.Fatal("missing detail commands")
	}
	found := false
	for _, cmd := range batch {
		message := cmd()
		if message != nil && reflect.TypeOf(message).Name() == "taskAnimationMsg" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("entering detail invalidated animation without restarting it")
	}
}
