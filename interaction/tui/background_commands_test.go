package tui

import (
	"context"
	controlruntime "nekocode/runtime"
	"testing"
)

type backgroundFakeRuntime struct {
	localFakeBot
	session, id, text, stopped string
	allStopped                 bool
}

func (*backgroundFakeRuntime) CurrentSessionID() string { return "parent" }
func (*backgroundFakeRuntime) BackgroundTasks(string) ([]controlruntime.BackgroundTask, error) {
	return nil, nil
}
func (b *backgroundFakeRuntime) AppendBackgroundTask(_ context.Context, session, id, text string) error {
	b.session, b.id, b.text = session, id, text
	return nil
}
func (b *backgroundFakeRuntime) CancelBackgroundTask(_ context.Context, session, id string) error {
	b.session, b.stopped = session, id
	return nil
}
func TestBackgroundCommandsAddressExplicitTaskWithoutForegroundRun(t *testing.T) {
	rt := &backgroundFakeRuntime{}
	m, err := NewModel(rt)
	if err != nil {
		t.Fatal(err)
	}
	m.state = stateProcessing
	if handled, _ := m.tryLocalCommand("/task-add task-A 保留  两个空格\n下一行"); !handled {
		t.Fatal("command not handled")
	}
	if rt.session != "parent" || rt.id != "task-A" || rt.text != "保留  两个空格\n下一行" || rt.runs != 0 {
		t.Fatalf("wrong routing: %+v", rt)
	}
	if handled, _ := m.tryLocalCommand("/task-stop task-B"); !handled || rt.stopped != "task-B" || rt.runs != 0 {
		t.Fatal("cancel routed to foreground")
	}
}

func (b *backgroundFakeRuntime) StopBackgroundTasks(_ context.Context, session string) error {
	b.session = session
	b.allStopped = true
	return nil
}
func TestStopAllCommandUsesSessionController(t *testing.T) {
	rt := &backgroundFakeRuntime{}
	m, err := NewModel(rt)
	if err != nil {
		t.Fatal(err)
	}
	if handled, _ := m.tryLocalCommand("/tasks-stop"); !handled || !rt.allStopped || rt.session != "parent" || rt.runs != 0 {
		t.Fatalf("wrong stop target: %+v", rt)
	}
}

type taskSearchFake struct {
	backgroundFakeRuntime
	after int
	query string
}

func (f *taskSearchFake) SearchSubagentOutputs(_ context.Context, parent, id, query string, after, limit int) (controlruntime.TaskSearchPage, error) {
	f.session = parent
	f.id = id
	f.query = query
	f.after = after
	return controlruntime.TaskSearchPage{Hits: []controlruntime.TaskSearchHit{{Sequence: 9, Preview: "match"}}, Next: 9, HasMore: true}, nil
}
func TestTaskSearchCommandUsesServerCursorWithoutForegroundRun(t *testing.T) {
	rt := &taskSearchFake{}
	m, err := NewModel(rt)
	if err != nil {
		t.Fatal(err)
	}
	if handled, _ := m.tryLocalCommand("/task-find task-A --after=7 needle text"); !handled {
		t.Fatal("search not handled")
	}
	if rt.after != 7 || rt.query != "needle text" || rt.id != "task-A" || rt.session != "parent" || rt.runs != 0 {
		t.Fatalf("wrong search routing: %+v", rt)
	}
}
