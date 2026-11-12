package tui

import (
	"fmt"
	"nekocode/protocol"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	controlruntime "nekocode/runtime"
)

func processKey(m *Model, key string) tea.Cmd {
	code := map[string]rune{"up": tea.KeyUp, "down": tea.KeyDown, "pgup": tea.KeyPgUp, "pgdown": tea.KeyPgDown, "home": tea.KeyHome, "end": tea.KeyEnd, "enter": tea.KeyEnter, "esc": tea.KeyEscape}[key]
	if code != 0 {
		return m.handleTaskKey(tea.KeyPressMsg{Code: code})
	}
	return m.handleTaskKey(tea.KeyPressMsg{Code: []rune(key)[0], Text: key})
}
func processEvent(m *Model, kind controlruntime.EventType, payload controlruntime.ToolPayload) {
	s := m.tasks
	d := s.detail
	m.handleTaskMessage(taskEventMsg{generation: s.generation, request: s.streamRequest, id: d.id, ok: true, event: controlruntime.Event{Type: kind, Payload: payload}})
	m.handleTaskMessage(taskFrameMsg{generation: s.generation, request: s.streamRequest, id: d.id})
}
func TestTaskProcessIsSeparateSelectablePage(t *testing.T) {
	m := openedTaskModel(t)
	openTestDetail(m)
	d := m.tasks.detail
	d.list.ScrollToTop()
	original := d.list
	index, line := d.list.ViewportAnchor()
	entries := len(d.entries)
	processKey(m, "p")
	p := d.process
	if p == nil || p.list == original || len(d.entries) != entries {
		t.Fatal("process replaced the overview")
	}
	if !p.follow || p.selected != "shell-1" {
		t.Fatalf("initial selection=%q follow=%v", p.selected, p.follow)
	}
	view := ansi.Strip(m.taskPanelView(90, 28))
	if !strings.Contains(view, "执行过程") || !strings.Contains(view, "▸") || !strings.Contains(view, "↑↓ 选择") {
		t.Fatalf("missing process navigation: %s", view)
	}
	processKey(m, "up")
	if p.follow || p.selected != "read-1" {
		t.Fatal("Up did not select previous call and pause")
	}
	selected := p.selected
	anchor, offset := p.list.ViewportAnchor()
	processKey(m, "enter")
	if d.inspector == nil || d.inspectedEntry.call.ID != selected {
		t.Fatal("Enter opened a different call")
	}
	processKey(m, "esc")
	gotAnchor, gotOffset := p.list.ViewportAnchor()
	if d.inspector != nil || d.process != p || p.selected != selected || anchor != gotAnchor || offset != gotOffset {
		t.Fatal("Esc lost process selection/position")
	}
	processKey(m, "esc")
	gotIndex, gotLine := d.list.ViewportAnchor()
	if d.process != nil || d.showProcess || d.list != original || index != gotIndex || line != gotLine {
		t.Fatal("Esc lost overview position")
	}
}
func TestPausedTaskProcessKeepsSelectionAndUpdatesLiveOutput(t *testing.T) {
	m := openedTaskModel(t)
	openTestDetail(m)
	d := m.tasks.detail
	processKey(m, "p")
	p := d.process
	processKey(m, "up")
	selected := p.selected
	processEvent(m, controlruntime.EventToolStarted, controlruntime.ToolPayload{CallID: "new-call", ToolName: "shell", Args: `{"command":"git diff"}`})
	if p.selected != selected || p.follow || p.unread != 1 {
		t.Fatalf("new event stole selection: selected=%q unread=%d", p.selected, p.unread)
	}
	processKey(m, "end")
	if !p.follow || p.selected != "new-call" || p.unread != 0 {
		t.Fatal("End did not resume latest")
	}
	processKey(m, "enter")
	if p.follow || d.inspectedEntry.call.ID != "new-call" {
		t.Fatal("inspection failed to pin selected call")
	}
	processEvent(m, controlruntime.EventToolPreview, controlruntime.ToolPayload{CallID: "new-call", ToolName: "shell", Preview: "LIVE_PROGRESS_SENTINEL"})
	if !strings.Contains(ansi.Strip(d.inspector.Render()), "LIVE_PROGRESS_SENTINEL") {
		t.Fatal("selected inspector stopped updating")
	}
	processEvent(m, controlruntime.EventToolStarted, controlruntime.ToolPayload{CallID: "newer", ToolName: "read", Args: `{"path":"new.go"}`})
	if d.inspectedEntry.call.ID != "new-call" || p.selected != "new-call" {
		t.Fatal("new call stole inspector focus")
	}
	processKey(m, "esc")
	if d.process != p || p.selected != "new-call" || p.unread != 1 {
		t.Fatal("inspector return lost selection or unread count")
	}
}
func TestTaskProcessPagingSelectionAndStableIDs(t *testing.T) {
	m := openedTaskModel(t)
	openTestDetail(m)
	d := m.tasks.detail
	d.page.Records = nil
	d.tools = map[string]liveTaskTool{}
	d.liveOrder = nil
	for i := 0; i < 80; i++ {
		d.page.Records = append(d.page.Records, controlruntime.SubagentRecord{Sequence: i + 1, Role: "assistant", ToolCalls: []controlruntime.SubagentToolCall{{ID: fmt.Sprintf("call-%02d", i), Name: "read", Arguments: `{"path":"fixture.go"}`}}})
	}
	processKey(m, "p")
	p := d.process
	processKey(m, "pgup")
	if p.selected == "call-79" || p.follow {
		t.Fatal("PageUp did not move selection")
	}
	processKey(m, "home")
	if p.selected != "call-00" || p.list.ScrollY() != 0 {
		t.Fatal("Home did not select oldest")
	}
	processKey(m, "pgdown")
	if p.selected == "call-00" || p.follow {
		t.Fatal("PageDown did not move selection")
	}
	selected := p.selected
	processKey(m, "enter")
	processKey(m, "esc")
	if p.selected != selected {
		t.Fatal("selection was tracked by changing index")
	}
	processKey(m, "end")
	if p.selected != "call-79" || !p.follow {
		t.Fatal("End lost newest call")
	}
	for _, size := range [][2]int{{40, 16}, {100, 35}} {
		m.taskPanelView(size[0], size[1])
		if p.selected != "call-79" {
			t.Fatal("resize lost selected ID")
		}
	}
}
func TestProcessInspectorPausesScrollingButKeepsReceivingOutput(t *testing.T) {
	m := openedTaskModel(t)
	openTestDetail(m)
	d := m.tasks.detail
	processKey(m, "p")
	processEvent(m, controlruntime.EventToolPreview, controlruntime.ToolPayload{CallID: "shell-1", ToolName: "shell", Preview: strings.Repeat("line of output\n", 100)})
	processKey(m, "enter")
	processKey(m, "end")
	processEvent(m, controlruntime.EventToolPreview, controlruntime.ToolPayload{CallID: "shell-1", ToolName: "shell", Preview: strings.Repeat("line of output\n", 100) + "LATEST_OUTPUT_SENTINEL"})
	if !strings.Contains(ansi.Strip(d.inspector.Render()), "LATEST_OUTPUT_SENTINEL") {
		t.Fatal("End did not follow live output")
	}
	processKey(m, "home")
	index, line := d.inspector.ViewportAnchor()
	processEvent(m, controlruntime.EventToolPreview, controlruntime.ToolPayload{CallID: "shell-1", ToolName: "shell", Preview: strings.Repeat("line of output\n", 150) + "NEW_TAIL"})
	nextIndex, nextLine := d.inspector.ViewportAnchor()
	if index != nextIndex || line != nextLine || d.inspectorFollow {
		t.Fatal("live output interrupted history reading")
	}
	if !strings.Contains(d.inspectedEntry.result.Content, "NEW_TAIL") {
		t.Fatal("paused inspector did not receive new output")
	}
}
func TestTaskProcessHomeLoadsOlderCallsWithoutLosingNewerCalls(t *testing.T) {
	m := openedTaskModel(t)
	openTestDetail(m)
	d := m.tasks.detail
	d.page = controlruntime.SubagentHistoryPage{Before: 100, End: 200, Total: 200, HasMore: true, Records: []controlruntime.SubagentRecord{{Sequence: 150, Role: "assistant", ToolCalls: []controlruntime.SubagentToolCall{{ID: "newer", Name: "read"}}}}}
	d.loadedCursor = 200
	d.cursors = []int{200}
	d.tools = map[string]liveTaskTool{}
	d.liveOrder = nil
	processKey(m, "p")
	p := d.process
	cmd := processKey(m, "home")
	if cmd == nil {
		t.Fatal("Home did not request earlier history")
	}
	reply := cmd().(taskHistoryMsg)
	reply.page = controlruntime.SubagentHistoryPage{Before: 1, End: 99, Total: 200, Records: []controlruntime.SubagentRecord{{Sequence: 1, Role: "assistant", ToolCalls: []controlruntime.SubagentToolCall{{ID: "oldest", Name: "grep"}}}}}
	m.handleTaskMessage(reply)
	if len(p.calls) != 2 || p.selected != "oldest" || p.unread != 0 || p.seekOldest {
		t.Fatalf("incorrect historical merge: selected=%q calls=%d unread=%d", p.selected, len(p.calls), p.unread)
	}
	processKey(m, "end")
	if p.selected != "newer" {
		t.Fatal("historical read discarded newer calls")
	}
}

func TestTaskProcessHomeAtOldestStillCountsNewCalls(t *testing.T) {
	m := openedTaskModel(t)
	openTestDetail(m)
	processKey(m, "p")
	d := m.tasks.detail
	d.page.HasMore = false
	processKey(m, "home")
	selected := d.process.selected
	processEvent(m, controlruntime.EventToolStarted, controlruntime.ToolPayload{CallID: "after-home", ToolName: "shell", Args: `{}`})
	if d.process.seekOldest || d.process.unread != 1 || d.process.selected != selected {
		t.Fatalf("Home suppressed live notifications: seek=%v unread=%d selected=%q", d.process.seekOldest, d.process.unread, d.process.selected)
	}
}

func TestTaskProcessPreservesLiveExecutionEvidence(t *testing.T) {
	m := openedTaskModel(t)
	openTestDetail(m)
	processKey(m, "p")
	processEvent(m, controlruntime.EventToolStarted, controlruntime.ToolPayload{CallID: "duration-call", ToolName: "shell", Args: `{}`})
	evidence := protocol.ToolEvidence{Schema: 1, Executions: []protocol.ExecutionReceipt{{StartedAt: 1000000000, EndedAt: 2500000000}}}
	processEvent(m, controlruntime.EventToolCompleted, controlruntime.ToolPayload{CallID: "duration-call", ToolName: "shell", Output: "done", Evidence: evidence})
	// Later payloads may omit receipts; they must not erase the final evidence.
	evidence.Executions[0].EndedAt = 99000000000
	processEvent(m, controlruntime.EventToolPreview, controlruntime.ToolPayload{CallID: "duration-call", ToolName: "shell", Preview: "done"})
	for _, c := range m.tasks.detail.process.calls {
		if c.entry.call.ID == "duration-call" {
			if c.duration != "1.5s" {
				t.Fatalf("duration evidence dropped or aliased: %q", c.duration)
			}
			return
		}
	}
	t.Fatal("call absent")
}
