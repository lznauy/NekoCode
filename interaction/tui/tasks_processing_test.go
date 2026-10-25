package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
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
	view := ansi.Strip(m.taskPanelView(80, 24))
	for _, section := range []string{"activity", "changes", "output", "thinking"} {
		if !strings.Contains(view, section) {
			t.Fatalf("missing %s section: %s", section, view)
		}
	}
	w, h := m.taskSize()
	compact := ansi.Strip(m.taskPanelView(w, h))
	for _, section := range []string{"activity", "changes", "output", "thinking"} {
		if !strings.Contains(compact, section) {
			t.Fatalf("real overlay lost %s: %s", section, compact)
		}
	}
	lines := strings.Split(compact, "\n")
	for i, line := range lines {
		if strings.Contains(line, d.info.Phase) && strings.Contains(line, "...") && (i+1 >= len(lines) || !strings.Contains(lines[i+1], "└")) {
			t.Fatal("processing card bottom border was clipped")
		}
	}
	if len(d.entries) != 1 {
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
