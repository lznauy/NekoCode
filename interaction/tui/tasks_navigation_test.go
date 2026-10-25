package tui

import (
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
