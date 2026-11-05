package tui

import (
	"fmt"
	"nekocode/interaction/tui/components"
	"strings"
	"testing"

	controlruntime "nekocode/runtime"

	tea "charm.land/bubbletea/v2"
)

func TestTaskListCrossPageSelection(t *testing.T) {
	for _, tt := range []struct {
		name   string
		input  tea.Msg
		upward bool
	}{
		{"up", tea.KeyPressMsg{Code: tea.KeyUp}, true},
		{"pgup", tea.KeyPressMsg{Code: tea.KeyPgUp}, true},
		{"wheel-up", tea.MouseWheelMsg{Button: tea.MouseWheelUp}, true},
		{"down", tea.KeyPressMsg{Code: tea.KeyDown}, false},
		{"pgdown", tea.KeyPressMsg{Code: tea.KeyPgDown}, false},
		{"wheel-down", tea.MouseWheelMsg{Button: tea.MouseWheelDown}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m := openedTaskModel(t)
			s := m.tasks
			s.listOffset, s.appliedOffset = 1000, 1000
			s.listPage.HasMore, s.listPage.NextOffset = true, 2000
			s.items = []controlruntime.SubagentSummary{{ID: "current"}}
			s.selected = "current"
			_, cmd := m.Update(tt.input)
			if cmd == nil {
				t.Fatal("adjacent page was not requested")
			}
			wantOffset, wantSelected := 2000, "first"
			if tt.upward {
				wantOffset, wantSelected = 0, "last"
			}
			if s.listOffset != wantOffset {
				t.Fatalf("requested offset=%d, want %d", s.listOffset, wantOffset)
			}
			page := controlruntime.SubagentPage{SessionID: "parent", Total: 3000, HasMore: true, NextOffset: wantOffset + 1000, Items: []controlruntime.SubagentSummary{{ID: "first"}, {ID: "last"}}}
			m.handleTaskMessage(taskListMsg{generation: s.generation, request: s.listRequest, page: page})
			if s.selected != wantSelected {
				t.Fatalf("selected=%s, want %s", s.selected, wantSelected)
			}
		})
	}
}

func TestTaskListNewQuerySelectsFirstItem(t *testing.T) {
	m := openedTaskModel(t)
	s := m.tasks
	s.appliedOffset = 1000
	s.appliedQuery = "old query"
	s.query = "new query"
	s.listOffset = 0
	s.selected = "old-result"
	s.items = []controlruntime.SubagentSummary{{ID: "old-result"}}
	m.loadTasks()
	page := controlruntime.SubagentPage{SessionID: "parent", Items: []controlruntime.SubagentSummary{{ID: "first"}, {ID: "last"}}}
	m.handleTaskMessage(taskListMsg{generation: s.generation, request: s.listRequest, page: page})
	if s.selected != "first" {
		t.Fatalf("new query selected=%s, want first", s.selected)
	}
}

func TestTaskReopenSelectsLatestAttempt(t *testing.T) {
	m := openedTaskModel(t)
	openTestDetail(m)
	d := m.tasks.detail
	first := &controlruntime.SubagentAttemptSummary{ID: "first", Seq: 1, Status: "completed", TranscriptStart: 1, TranscriptEnd: 4}
	d.attempt = cloneTaskAttempt(first)
	d.info.LatestAttempt = cloneTaskAttempt(first)
	d.showProcess = true
	d.eventSeq = 100
	m.handleTaskKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	latest := &controlruntime.SubagentAttemptSummary{ID: "second", Seq: 2, Status: "running", Prompt: "continue", TranscriptStart: 5, TranscriptEnd: 5}
	for i := range m.tasks.items {
		if m.tasks.items[i].ID == d.id {
			m.tasks.items[i].LatestAttempt = latest
			m.tasks.items[i].TranscriptSeq = 5
			m.tasks.items[i].Status = "running"
		}
	}
	cmd := m.openSelectedTask()
	if cmd == nil || d.attempt.ID != latest.ID || d.showProcess || d.eventSeq != 0 || len(d.page.Records) != 0 || !d.follow {
		t.Fatalf("reopening reused old attempt state: attempt=%+v", d.attempt)
	}
}

func TestTaskDelayedLatestSummaryRespectsPausedReading(t *testing.T) {
	for _, paused := range []bool{false, true} {
		m := openedTaskModel(t)
		openTestDetail(m)
		d := m.tasks.detail
		first := &controlruntime.SubagentAttemptSummary{ID: "first", Seq: 1, Status: "completed", TranscriptStart: 1, TranscriptEnd: 4}
		d.attempt, d.info.LatestAttempt = cloneTaskAttempt(first), cloneTaskAttempt(first)
		d.follow = !paused
		page := taskTestPage()
		page.Items[0].LatestAttempt = &controlruntime.SubagentAttemptSummary{ID: "second", Seq: 2, Status: "running", TranscriptStart: 5, TranscriptEnd: 5}
		page.Items[0].TranscriptSeq = 5
		m.handleTaskMessage(taskListMsg{generation: m.tasks.generation, request: m.tasks.listRequest, page: page})
		want := "second"
		if paused {
			want = "first"
		}
		if d.attempt.ID != want {
			t.Fatalf("paused=%v: selected %s, want %s", paused, d.attempt.ID, want)
		}
		if paused && !d.newAttempt {
			t.Fatal("paused reader lost new attempt notification")
		}
	}
}

type taskScrollFixture struct{}

func (taskScrollFixture) Render(int) string {
	return strings.Repeat("scroll row\n", 199) + "scroll row"
}
func (taskScrollFixture) Height(int) int { return 200 }

func TestTaskDetailScrollInteractionMatchesMain(t *testing.T) {
	m := openedTaskModel(t)
	openTestDetail(m)
	d := m.tasks.detail
	d.list.SetItems(taskScrollFixture{})
	d.list.SetSize(70, 12)
	d.list.ScrollToBottom()
	m.Messages.SetItems(taskScrollFixture{})
	m.Messages.SetSize(70, 12)
	m.Messages.Processing = true
	m.Messages.SetRenderingSuspended(false)
	m.Messages.GotoBottom()
	m.Messages.Render()
	inputs := []tea.Msg{
		tea.MouseWheelMsg{Button: tea.MouseWheelUp},
		tea.KeyPressMsg{Code: tea.KeyPgUp},
		tea.KeyPressMsg{Code: tea.KeyUp},
		tea.KeyPressMsg{Code: tea.KeyDown},
		tea.KeyPressMsg{Code: tea.KeyPgDown},
		tea.MouseWheelMsg{Button: tea.MouseWheelDown},
		tea.KeyPressMsg{Code: tea.KeyPgDown},
	}
	for _, input := range inputs {
		m.Messages.Update(input)
		m.Update(input)
		if got, want := d.list.ScrollY(), m.Messages.ScrollY(); got != want {
			t.Fatalf("%T %v: task scroll=%d, main=%d", input, input, got, want)
		}
		if d.follow != m.Messages.Follow {
			t.Fatalf("%v: task follow=%v, main=%v", input, d.follow, m.Messages.Follow)
		}
	}
}

func TestTaskEndReturnsImmediatelyWithoutWaitingForHistory(t *testing.T) {
	m := openedTaskModel(t)
	openTestDetail(m)
	d := m.tasks.detail
	d.list.SetItems(taskScrollFixture{})
	d.list.SetSize(70, 12)
	d.list.ScrollToTop()
	d.follow = false
	m.handleTaskKey(tea.KeyPressMsg{Code: tea.KeyEnd})
	if !d.list.AtBottom() || !d.follow {
		t.Fatal("End waited for history I/O before returning to bottom")
	}
}

func seedTaskOlderPage(m *Model) {
	d := m.tasks.detail
	d.showProcess = true
	d.cursors = []int{0, 3}
	d.loadedCursor = 3
	d.page = controlruntime.SubagentHistoryPage{Before: 1, End: 2, Total: 4, Records: []controlruntime.SubagentRecord{
		{Sequence: 1, Role: "user", Content: strings.Repeat("old page content ", 200)},
		{Sequence: 2, Role: "assistant", Content: "older response"},
	}}
	d.follow = false
	m.rebuildTaskDetail()
}

func TestTaskEndThenScrollUpKeepsHistoryCursor(t *testing.T) {
	m := openedTaskModel(t)
	openTestDetail(m)
	seedTaskOlderPage(m)
	d := m.tasks.detail
	cmd := m.handleTaskKey(tea.KeyPressMsg{Code: tea.KeyEnd})
	m.handleTaskKey(tea.KeyPressMsg{Code: tea.KeyUp})
	index, line := d.list.ViewportAnchor()
	applyAttemptCommands(t, m, cmd)
	gotIndex, gotLine := d.list.ViewportAnchor()
	if d.follow || d.page.End != 2 || len(d.cursors) != 2 || d.loadedCursor != 3 || index != gotIndex || line != gotLine {
		t.Fatalf("late End reply disturbed paused reader: follow=%v end=%d cursors=%v loaded=%d", d.follow, d.page.End, d.cursors, d.loadedCursor)
	}
}

func TestTaskPagingOlderStartsAtTop(t *testing.T) {
	m := openedTaskModel(t)
	openTestDetail(m)
	d := m.tasks.detail
	d.showProcess = true
	d.follow = false
	older := controlruntime.SubagentHistoryPage{Before: 1, End: 2, Total: 4, Records: []controlruntime.SubagentRecord{
		{Sequence: 1, Role: "user", Content: strings.Repeat("older page ", 300)},
		{Sequence: 2, Role: "assistant", Content: "older response"},
	}}
	m.handleTaskMessage(taskHistoryMsg{generation: m.tasks.generation, request: d.request, id: d.id, before: 3, page: older})
	if d.list.ScrollY() != 0 || d.follow {
		t.Fatalf("older page opened away from top: scroll=%d follow=%v", d.list.ScrollY(), d.follow)
	}
}

func TestTaskPagingBackToNewestResumesFollow(t *testing.T) {
	m := openedTaskModel(t)
	openTestDetail(m)
	seedTaskOlderPage(m)
	d := m.tasks.detail
	d.list.ScrollToBottom()
	cmd := m.handleTaskKey(tea.KeyPressMsg{Code: tea.KeyDown})
	if cmd == nil {
		t.Fatal("newer page was not requested")
	}
	m.handleTaskMessage(cmd())
	if !d.follow || !d.list.AtBottom() || len(d.cursors) != 1 || d.loadedCursor != 0 {
		t.Fatalf("newest page did not resume follow: follow=%v bottom=%v cursors=%v loaded=%d", d.follow, d.list.AtBottom(), d.cursors, d.loadedCursor)
	}
}

func TestTaskNewerPageReplyRespectsContinuedReading(t *testing.T) {
	m := openedTaskModel(t)
	openTestDetail(m)
	seedTaskOlderPage(m)
	d := m.tasks.detail
	d.list.ScrollToBottom()
	cmd := m.handleTaskKey(tea.KeyPressMsg{Code: tea.KeyDown})
	m.handleTaskKey(tea.KeyPressMsg{Code: tea.KeyUp})
	index, line := d.list.ViewportAnchor()
	m.handleTaskMessage(cmd())
	gotIndex, gotLine := d.list.ViewportAnchor()
	if d.follow || d.loadedCursor != 3 || d.page.End != 2 || len(d.cursors) != 2 || index != gotIndex || line != gotLine {
		t.Fatalf("newer page reply interrupted reading: follow=%v loaded=%d end=%d", d.follow, d.loadedCursor, d.page.End)
	}
}

func TestTaskOlderPageReplyRespectsContinuedReading(t *testing.T) {
	m := openedTaskModel(t)
	openTestDetail(m)
	d := m.tasks.detail
	d.showProcess = true
	d.follow = false
	d.page = controlruntime.SubagentHistoryPage{Before: 3, End: 4, Total: 4, HasMore: true, Records: []controlruntime.SubagentRecord{
		{Sequence: 3, Role: "assistant", Content: strings.Repeat("current page ", 300)},
		{Sequence: 4, Role: "assistant", Content: "current response"},
	}}
	m.rebuildTaskDetail()
	d.list.ScrollToTop()
	cmd := m.handleTaskKey(tea.KeyPressMsg{Code: tea.KeyUp})
	m.handleTaskKey(tea.KeyPressMsg{Code: tea.KeyDown})
	index, line := d.list.ViewportAnchor()
	reply := cmd().(taskHistoryMsg)
	reply.page = controlruntime.SubagentHistoryPage{Before: 1, End: 2, Total: 4, Records: []controlruntime.SubagentRecord{{Sequence: 1, Role: "user", Content: "older prompt"}, {Sequence: 2, Role: "assistant", Content: "older response"}}}
	m.handleTaskMessage(reply)
	gotIndex, gotLine := d.list.ViewportAnchor()
	if d.loadedCursor != 0 || d.page.End != 4 || index != gotIndex || line != gotLine {
		t.Fatalf("older page reply interrupted reading: loaded=%d end=%d", d.loadedCursor, d.page.End)
	}
}

func TestTaskCompactSwitchThenScrollKeepsHistoryCursor(t *testing.T) {
	m := openedTaskModel(t)
	openTestDetail(m)
	seedTaskOlderPage(m)
	d := m.tasks.detail
	d.fullPrompt, d.fullPromptLoaded = strings.Repeat("delegated prompt ", 300), true
	cmd := m.handleTaskKey(tea.KeyPressMsg{Code: 'p', Text: "p"})
	m.handleTaskKey(tea.KeyPressMsg{Code: tea.KeyUp})
	index, line := d.list.ViewportAnchor()
	applyAttemptCommands(t, m, cmd)
	gotIndex, gotLine := d.list.ViewportAnchor()
	if d.follow || d.showProcess || d.loadedCursor != 3 || len(d.cursors) != 2 || d.page.End != 2 || index != gotIndex || line != gotLine {
		t.Fatalf("compact switch lost paused history cursor: follow=%v cursor=%d cursors=%v end=%d", d.follow, d.loadedCursor, d.cursors, d.page.End)
	}
}

func TestTaskPausedResizeClampsViewportAtContentEnd(t *testing.T) {
	for _, inspector := range []bool{false, true} {
		t.Run(fmt.Sprint(inspector), func(t *testing.T) {
			m := openedTaskModel(t)
			openTestDetail(m)
			d := m.tasks.detail
			d.follow = false
			d.list.SetItems(taskScrollFixture{})
			d.list.SetSize(70, 12)
			d.list.ScrollToBottom()
			list := d.list
			if inspector {
				d.inspector = components.NewList(taskScrollFixture{})
				list = d.inspector
				list.SetSize(70, 12)
				list.ScrollToBottom()
			}
			m.Update(tea.WindowSizeMsg{Width: 100, Height: 60})
			maxOffset := max(0, list.TotalContentHeight()-list.Height())
			if list.ScrollY() > maxOffset {
				t.Fatalf("resize left blank rows: offset=%d max=%d height=%d", list.ScrollY(), maxOffset, list.Height())
			}
			if d.follow {
				t.Fatal("resize unexpectedly enabled follow")
			}
		})
	}
}
