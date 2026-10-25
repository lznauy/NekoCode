package tui

import (
	"errors"
	"fmt"
	"testing"

	controlruntime "nekocode/runtime"

	tea "charm.land/bubbletea/v2"
)

func TestLiveTaskInspectorLoadsCommittedFullOutput(t *testing.T) {
	m := openedTaskModel(t)
	m.Runtime = &taskOutputFakeRuntime{}
	openTestDetail(m)
	d := m.tasks.detail
	m.inspectVisibleTaskTool()
	if d.inspectedEntry == nil || d.inspectedEntry.call == nil {
		t.Fatal("no pending inspector")
	}
	id := d.inspectedEntry.call.ID
	page := taskTestHistory()
	page.Records = append(page.Records, controlruntime.SubagentRecord{Sequence: 5, Role: "tool", ToolCallID: id, Content: "truncated output", OutputPath: "stored-output"})
	page.End, page.Total = 5, 5
	m.handleTaskMessage(taskHistoryMsg{generation: m.tasks.generation, request: d.request, id: d.id, page: page})
	if d.inspectedEntry.result.OutputPath != "stored-output" || d.output == nil || !d.output.loading {
		t.Fatal("pending inspector did not switch to stored full output")
	}
}

func TestTaskHistoryEvictsResultsOutsideCurrentPage(t *testing.T) {
	m := openedTaskModel(t)
	openTestDetail(m)
	d := m.tasks.detail
	d.attempt = nil
	d.info.LatestAttempt = nil
	d.follow = true
	for page := 1; page <= 30; page++ {
		id := fmt.Sprintf("call-%d", page)
		end := page * 2
		m.handleTaskMessage(taskHistoryMsg{
			generation: m.tasks.generation, request: d.request, id: d.id,
			page: controlruntime.SubagentHistoryPage{Before: end - 1, End: end, Total: end, Records: []controlruntime.SubagentRecord{
				{Sequence: end - 1, Role: "assistant", ToolCalls: []controlruntime.SubagentToolCall{{ID: id, Name: "read", Arguments: "{}"}}},
				{Sequence: end, Role: "tool", ToolCallID: id, Content: "page output"},
			}},
		})
		if len(d.committedResults) != 1 || d.committedResults[id].Content != "page output" {
			t.Fatalf("page %d retained results outside its window: %v", page, d.committedResults)
		}
	}
}

func TestPausedRetryClearsError(t *testing.T) {
	m := openedTaskModel(t)
	openTestDetail(m)
	d := m.tasks.detail
	d.follow = false
	m.handleTaskMessage(taskHistoryMsg{generation: m.tasks.generation, request: d.request, id: d.id, before: 0, err: errors.New("temporary")})
	previousEnd := d.page.End
	page := taskTestHistory()
	page.End++
	page.Total++
	m.handleTaskMessage(taskHistoryMsg{generation: m.tasks.generation, request: d.request, id: d.id, before: 0, page: page})
	if d.err != "" {
		t.Fatalf("successful retry retained error: %s", d.err)
	}
	if d.page.End != previousEnd || d.newRecords != 1 {
		t.Fatal("successful retry changed paused reading position or lost new record count")
	}
}

func TestTaskOutputRetryUsesFailedOffset(t *testing.T) {
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
	cmd := m.inspectVisibleTaskTool()
	if cmd == nil {
		blocks := 0
		if d.processing != nil {
			blocks = len(d.processing.Blocks())
		}
		t.Fatalf("no processing tool inspector: status=%s entries=%d processing=%t blocks=%d", d.detailStatus(), len(d.entries), d.processing != nil, blocks)
	}
	m.handleTaskMessage(cmd())
	const failedOffset int64 = 65536
	m.loadTaskOutput(failedOffset)
	m.handleTaskMessage(taskOutputMsg{generation: m.tasks.generation, request: d.output.request, id: d.id, key: d.inspectedKey, err: errors.New("temporary page failure"), page: controlruntime.SubagentOutputPage{}})
	retry := m.handleTaskKey(tea.KeyPressMsg{Code: 'r', Text: "r"})
	if retry == nil {
		t.Fatal("retry command missing")
	}
	if d.output.requestedOffset != failedOffset {
		t.Fatalf("retry offset = %d, want failed page %d", d.output.requestedOffset, failedOffset)
	}
}
