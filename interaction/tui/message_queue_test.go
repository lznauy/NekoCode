package tui

import (
	"context"
	"errors"
	"io"
	"nekocode/util/attachment"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	controlruntime "nekocode/runtime"
)

func queueKey(m *Model, code rune, mod tea.KeyMod) {
	m.Update(tea.KeyPressMsg{Code: code, Mod: mod})
}

func enqueueText(m *Model, text string) {
	m.Input.SetValue(text)
	queueKey(m, tea.KeyTab, 0)
}

func TestQueueManagementPreservesTurnsDraftAndUndoOrder(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	b := &tickFakeBot{}
	m, err := NewModel(b)
	if err != nil {
		t.Fatal(err)
	}
	m.Update(tea.WindowSizeMsg{Width: 46, Height: 22})
	m.transitionTo(stateProcessing)
	for _, text := range []string{"B", "C", "D"} {
		enqueueText(m, text)
	}
	m.Input.SetValue("unfinished draft")
	queueKey(m, tea.KeyTab, tea.ModShift)
	queueKey(m, tea.KeyUp, 0)
	queueKey(m, tea.KeyDelete, 0)
	queueKey(m, tea.KeyDelete, 0)
	m.Update(doneMsg{})
	if len(b.submitted) > 0 || len(m.queue.items) != 1 {
		t.Fatal("management did not pause dispatch")
	}
	queueKey(m, 'z', tea.ModCtrl)
	queueKey(m, 'z', tea.ModCtrl)
	queueKey(m, tea.KeyEnter, 0)
	m.Update(tea.PasteMsg{Content: " updated"})
	for _, width := range []int{24, 80} {
		m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
		panel, cursor := m.queueView()
		if cursor == nil || len(strings.Split(panel, "\n")) != m.queueHeight() {
			t.Fatal("editor has no cursor or incorrect height")
		}
		for _, line := range strings.Split(panel, "\n") {
			if ansi.StringWidth(line) > width {
				t.Fatalf("queue row overflow: %q", line)
			}
		}
	}
	queueKey(m, 's', tea.ModCtrl)
	queueKey(m, tea.KeyEsc, 0)
	if !reflect.DeepEqual(b.submitted, []string{"B"}) || m.Input.Value() != "unfinished draft" {
		t.Fatalf("first dispatch: %v, draft %q", b.submitted, m.Input.Value())
	}
	m.Update(runtimeEventMsg{event: controlruntime.Event{Type: controlruntime.EventInputAccepted, Payload: controlruntime.MessagePayload{Content: "B", Source: controlruntime.SourceRef{Kind: "tui", ID: queuedSourceID}}}})
	m.Update(doneMsg{})
	m.Update(doneMsg{})
	if !reflect.DeepEqual(b.submitted, []string{"B", "C updated", "D"}) || m.Input.Value() != "unfinished draft" {
		t.Fatalf("turns %v draft %q", b.submitted, m.Input.Value())
	}
}

func TestQueueEditorSavesKeyboardNewlines(t *testing.T) {
	for _, tc := range []struct {
		name string
		code rune
		mod  tea.KeyMod
	}{{"Enter", tea.KeyEnter, 0}, {"AltEnter", tea.KeyEnter, tea.ModAlt}, {"CtrlJ", 'j', tea.ModCtrl}} {
		t.Run(tc.name, func(t *testing.T) {
			m, err := NewModel(&tickFakeBot{})
			if err != nil {
				t.Fatal(err)
			}
			m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
			m.transitionTo(stateProcessing)
			enqueueText(m, "第一行")
			queueKey(m, tea.KeyTab, tea.ModShift)
			queueKey(m, tea.KeyEnter, 0)
			queueKey(m, tc.code, tc.mod)
			m.Update(tea.PasteMsg{Content: "第二行"})
			queueKey(m, 's', tea.ModCtrl)
			if got := m.queue.items[0].Value(); got != "第一行\n第二行" {
				t.Fatalf("saved draft = %q", got)
			}
		})
	}
}

func TestQueueCancelledEditAndFailedAdmissionKeepOriginal(t *testing.T) {
	b := &busyWindowRuntime{busy: true}
	m, err := NewModel(b)
	if err != nil {
		t.Fatal(err)
	}
	m.transitionTo(stateProcessing)
	enqueueText(m, "first")
	queueKey(m, tea.KeyTab, tea.ModShift)
	queueKey(m, tea.KeyEnter, 0)
	m.Update(tea.PasteMsg{Content: " discard"})
	queueKey(m, tea.KeyEsc, 0)
	queueKey(m, tea.KeyEsc, 0)
	m.Update(doneMsg{})
	if b.attempts != 1 || !m.queue.managing || len(m.queue.items) != 1 || m.queue.items[0].Value() != "first" {
		t.Fatal("failed admission lost queued draft")
	}
	m.loadSessionMessages()
	if len(m.queue.items) != 0 || m.queue.managing {
		t.Fatal("queued draft crossed sessions")
	}
}

type settlingQueueRuntime struct {
	tickFakeBot
	settled chan struct{}
}

func (r *settlingQueueRuntime) WaitRun(context.Context, controlruntime.RunID) error {
	<-r.settled
	return nil
}

func TestQueuedDispatchWaitsForForegroundCleanup(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	r := &settlingQueueRuntime{settled: make(chan struct{})}
	m, err := NewModel(r)
	if err != nil {
		t.Fatal(err)
	}
	m.transitionTo(stateProcessing)
	enqueueText(m, "next")
	cmd := m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventRunDone, RunID: "old"})
	m.dispatchQueued()
	if cmd == nil || m.queue.settling != "old" || len(r.submitted) > 0 {
		t.Fatal("submitted during foreground teardown")
	}
	close(r.settled)
	var runWait func(tea.Cmd)
	runWait = func(cmd tea.Cmd) {
		if cmd == nil {
			return
		}
		switch msg := cmd().(type) {
		case tea.BatchMsg:
			for _, sub := range msg {
				if len(r.submitted) == 0 {
					runWait(sub)
				}
			}
		case queueSettledMsg:
			m.Update(msg)
		}
	}
	// handleRuntimeEvent's batch contains the cleanup waiter without the event listener.
	runWait(cmd)
	if !reflect.DeepEqual(r.submitted, []string{"next"}) {
		t.Fatal(r.submitted)
	}
}

func TestMessageQueueRunsAsIndependentForegroundTurns(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	started := make(chan string, 3)
	finish := make(chan struct{})
	rt := controlruntime.New(controlruntime.RunnerFunc(func(ctx context.Context, text string, _ controlruntime.RunHost) (string, error) {
		started <- text
		select {
		case <-finish:
			return "done", nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}), controlruntime.ServiceGroups{})
	defer rt.Close()
	m, err := NewModel(rt)
	if err != nil {
		t.Fatal(err)
	}
	p := tea.NewProgram(m, tea.WithoutRenderer(), tea.WithInput(nil), tea.WithOutput(io.Discard))
	exited := make(chan error, 1)
	go func() { _, err := p.Run(); exited <- err }()
	defer func() { p.Quit(); <-exited }()
	send := func(s string, key rune) { p.Send(tea.PasteMsg{Content: s}); p.Send(tea.KeyPressMsg{Code: key}) }
	expect := func(s string) {
		t.Helper()
		select {
		case got := <-started:
			if got != s {
				t.Fatalf("got %q, want %q", got, s)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%q did not start", s)
		}
	}
	send("first", tea.KeyEnter)
	expect("first")
	send("second", tea.KeyTab)
	send("third", tea.KeyTab)
	finish <- struct{}{}
	expect("second")
	finish <- struct{}{}
	expect("third")
	finish <- struct{}{}
}

func TestQueueImageSurvivesUndoAndIsReleasedOnRemoval(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m, err := NewModel(&tickFakeBot{})
	if err != nil {
		t.Fatal(err)
	}
	m.transitionTo(stateProcessing)
	path, err := attachment.SaveImage("queue", []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"))
	if err != nil {
		t.Fatal(err)
	}
	if !m.Input.AddImage(path) {
		t.Fatal("image rejected")
	}
	queueKey(m, tea.KeyTab, 0)
	queueKey(m, tea.KeyTab, tea.ModShift)
	queueKey(m, tea.KeyDelete, 0)
	queueKey(m, 'z', tea.ModCtrl)
	if _, err := os.Stat(path); err != nil || len(m.queue.items[0].Images()) != 1 {
		t.Fatal("undo lost attachment")
	}
	queueKey(m, tea.KeyDelete, 0)
	queueKey(m, tea.KeyEsc, 0)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("discarded attachment: %v", err)
	}
}

func TestQueueRestoresUnacceptedMessageWithoutOverwritingEdit(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m, err := NewModel(&tickFakeBot{})
	if err != nil {
		t.Fatal(err)
	}
	m.transitionTo(stateProcessing)
	enqueueText(m, "B")
	enqueueText(m, "C")
	m.Update(doneMsg{})
	queueKey(m, tea.KeyTab, tea.ModShift)
	queueKey(m, tea.KeyEnter, 0)
	m.Update(tea.PasteMsg{Content: " edited"})
	m.Update(runtimeEventMsg{event: controlruntime.Event{Type: controlruntime.EventRunFailed}})
	queueKey(m, 's', tea.ModCtrl)
	if !m.queue.managing || len(m.queue.items) != 2 || m.queue.items[0].Value() != "B" || m.queue.items[1].Value() != "C edited" {
		t.Fatal("preparation failure lost input or changed editor target")
	}
}

func TestQueueHintTracksAvailableTabAfterLocalCommand(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m, err := NewModel(&localFakeBot{})
	if err != nil {
		t.Fatal(err)
	}
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.transitionTo(stateProcessing)
	m.Input.SetValue("/local")
	queueKey(m, tea.KeyEnter, 0)
	m.Input.SetValue("queued after command")
	visible := strings.Contains(ansi.Strip(m.View().Content), "Tab 排队")
	queueKey(m, tea.KeyTab, 0)
	if len(m.queue.items) != 1 || !visible {
		t.Fatalf("after local command: queued=%d, hint visible=%v", len(m.queue.items), visible)
	}
	queueKey(m, tea.KeyTab, tea.ModShift)
	if strings.Contains(ansi.Strip(m.View().Content), "Tab 排队") {
		t.Fatal("management has focus but composer advertises Tab")
	}
	queueKey(m, tea.KeyEsc, 0)
	m.queue.settling = "previous"
	m.Update(doneMsg{})
	if !strings.Contains(ansi.Strip(m.View().Content), "Tab 排队") {
		t.Fatal("waiting queue accepts Tab but hides the hint")
	}
}

func TestSingleQueuedDraftShowsManagementShortcut(t *testing.T) {
	m, err := NewModel(&tickFakeBot{})
	if err != nil {
		t.Fatal(err)
	}
	m.transitionTo(stateProcessing)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	enqueueText(m, "再次复核下")
	if !strings.Contains(ansi.Strip(m.View().Content), "待发送  再次复核下  Shift+Tab 管理") {
		t.Fatal("single draft should show the shortcut beside its preview")
	}
	m.Update(tea.WindowSizeMsg{Width: 24, Height: 14})
	if !strings.Contains(ansi.Strip(m.View().Content), queueManageHint) {
		t.Fatal("missing shortcut in the narrow view")
	}
	queueKey(m, tea.KeyTab, tea.ModShift)
	if !m.queue.managing {
		t.Fatal("single draft should be manageable")
	}
}

func TestQueueHeaderKeepsManagementHintOnNarrowScreens(t *testing.T) {
	m, err := NewModel(&tickFakeBot{})
	if err != nil {
		t.Fatal(err)
	}
	m.Update(tea.WindowSizeMsg{Width: 24, Height: 18})
	m.transitionTo(stateProcessing)
	enqueueText(m, "first")
	enqueueText(m, "second")
	for _, needsEdit := range []bool{false, true} {
		if needsEdit {
			m.queue.needsEdit = m.queue.items[0]
		}
		panel, _ := m.queueView()
		if !strings.Contains(ansi.Strip(panel), queueManageHint) {
			t.Errorf("needsEdit=%v: missing shortcut in %q", needsEdit, ansi.Strip(panel))
		}
		if len(strings.Split(panel, "\n")) != m.queueHeight() {
			t.Fatal("queue height differs from rendered rows")
		}
	}
}

func TestQueueEditorBlankDraftHelpMatchesSave(t *testing.T) {
	m, err := NewModel(&tickFakeBot{})
	if err != nil {
		t.Fatal(err)
	}
	m.transitionTo(stateProcessing)
	enqueueText(m, "original")
	queueKey(m, tea.KeyTab, tea.ModShift)
	queueKey(m, tea.KeyEnter, 0)
	m.queue.editor.SetValue(" \n ")
	if strings.Contains(strings.Join(m.queueHelp(), " "), "Ctrl+S 保存") {
		t.Fatal("blank draft advertises save")
	}
	queueKey(m, 's', tea.ModCtrl)
	if m.queue.editor == nil || m.queue.items[0].Value() != "original" {
		t.Fatal("blank edit replaced original draft")
	}
}

func TestQueueSpacingFitsViewport(t *testing.T) {
	m, err := NewModel(&tickFakeBot{})
	if err != nil {
		t.Fatal(err)
	}
	m.transitionTo(stateProcessing)
	enqueueText(m, "一条较长的消息，用于检查窄屏时的缩进和省略号")
	for _, size := range []tea.WindowSizeMsg{{Width: 80, Height: 30}, {Width: 24, Height: 14}} {
		m.Update(size)
		panel, _ := m.queueView()
		lines := strings.Split(ansi.Strip(panel), "\n")
		if len(lines) != m.queueHeight() {
			t.Fatalf("panel uses %d rows, reserved %d", len(lines), m.queueHeight())
		}
		if !strings.HasPrefix(lines[0], "  待发送  一条") || !strings.HasSuffix(strings.TrimSpace(strings.Join(lines, "\n")), queueManageHint) {
			t.Fatalf("unexpected alignment: %q", lines)
		}
		if (strings.Contains(lines[0], queueManageHint)) != (size.Width == 80) {
			t.Fatalf("unexpected shortcut placement: %q", lines)
		}
		if (lines[len(lines)-1] == "") != (size.Height == 30) {
			t.Fatalf("unexpected trailing space at height %d", size.Height)
		}
		for _, line := range lines {
			if ansi.StringWidth(line) > size.Width {
				t.Fatalf("row exceeds width: %q", line)
			}
		}
		if !strings.Contains(ansi.Strip(m.View().Content), "Tab 排队") {
			t.Fatalf("hint missing at width %d", size.Width)
		}
	}
}

func addQueueTestImage(t *testing.T, m *Model) string {
	t.Helper()
	path, err := attachment.SaveImage("queue", []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"))
	if err != nil {
		t.Fatal(err)
	}
	m.Input.SetCursorEnd()
	if !m.Input.AddImage(path) {
		t.Fatal("image not added")
	}
	return path
}

func TestQueueDeletionCannotReleaseSubmittedImage(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m, err := NewModel(&tickFakeBot{})
	if err != nil {
		t.Fatal(err)
	}
	path := addQueueTestImage(t, m)
	m.startChat(m.Input.Value(), inputImageAttachments(m.Input.ImageAttachments()))
	enqueueText(m, "next")
	queueKey(m, tea.KeyTab, tea.ModShift)
	queueKey(m, tea.KeyDelete, 0)
	queueKey(m, tea.KeyEsc, 0)
	if _, err := os.Stat(path); err != nil {
		t.Fatal("queue deleted the submitted image", err)
	}
}

type queuedSessionRuntime struct {
	backgroundFakeRuntime
	current    string
	afterCheck string
}

func (r *queuedSessionRuntime) CurrentSessionID() string {
	id := r.current
	if r.afterCheck != "" {
		r.current = r.afterCheck
		r.afterCheck = ""
	}
	return id
}
func TestQueuedLocalCommandPinsOwnerAndRetainsUnsentImages(t *testing.T) {
	for _, occupied := range []bool{false, true} {
		t.Setenv("HOME", t.TempDir())
		r := &queuedSessionRuntime{current: "owner"}
		m, err := NewModel(r)
		if err != nil {
			t.Fatal(err)
		}
		m.transitionTo(stateProcessing)
		enqueueText(m, "/tasks-stop")
		m.transitionTo(stateReady)
		r.afterCheck = "other"
		m.dispatchQueued()
		if r.session != "owner" || !r.allStopped {
			t.Fatalf("stop routed to %q", r.session)
		}
		m.transitionTo(stateProcessing)
		m.Input.SetValue("/task-stop missing ")
		path := addQueueTestImage(t, m)
		queueKey(m, tea.KeyTab, 0)
		if occupied {
			m.Input.SetValue("independent draft")
		}
		m.Update(doneMsg{})
		if _, err := os.Stat(path); err != nil {
			t.Fatal(err)
		}
		if occupied {
			if m.Input.Value() != "independent draft" || !m.queue.managing || len(m.queue.items) != 1 {
				t.Fatal("occupied composer or unsent image lost")
			}
			queueKey(m, tea.KeyEsc, 0)
			if len(m.queue.items) != 1 || m.queue.items[0] != m.queue.needsEdit {
				t.Fatal("command eligible for automatic replay")
			}
		} else if len(m.Input.ImageAttachments()) != 1 {
			t.Fatal("unsent image not restored")
		}
	}
}

func TestForegroundQueueKeepsFirstSessionAndRunsBehindTaskViewer(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var session string
	started := make(chan string, 2)
	release := make(chan struct{})
	rt := controlruntime.New(controlruntime.RunnerFunc(func(ctx context.Context, text string, _ controlruntime.RunHost) (string, error) {
		started <- text
		select {
		case <-release:
			return "ok", nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}), controlruntime.ServiceGroups{Sessions: controlruntime.SessionsServices{
		ListSessions: func() []controlruntime.SessionMeta { return nil }, SessionMessages: func() []controlruntime.DisplayMessage { return nil },
		CurrentSessionID:      func() string { return session },
		EnsureSessionIdentity: func() string { session = "first"; return session },
	}})
	defer rt.Close()
	m, err := NewModel(rt)
	if err != nil {
		t.Fatal(err)
	}
	m.startChat("first turn", nil)
	enqueueText(m, "second turn")
	if m.queue.session != "first" {
		t.Fatalf("queued before identity: %q", m.queue.session)
	}
	if got := <-started; got != "first turn" {
		t.Fatal(got)
	}
	run, _ := rt.CurrentRun()
	close(release)
	if err := rt.WaitRun(t.Context(), run.ID); err != nil {
		t.Fatal(err)
	}
	m.tasks = &taskOverlay{open: true}
	m.Update(doneMsg{})
	select {
	case got := <-started:
		if got != "second turn" {
			t.Fatal(got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("queue stalled behind task viewer")
	}
}

func TestQueueUsesRunSnapshotWhenAcceptanceEventWasMissed(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Setenv("HOME", t.TempDir())
		rt := controlruntime.New(controlruntime.RunnerFunc(func(context.Context, string, controlruntime.RunHost) (string, error) {
			if fail {
				return "", errors.New("after admission")
			}
			return "done", nil
		}), controlruntime.ServiceGroups{Models: controlruntime.ModelsServices{ImageAttachmentsEnabled: func() bool { return true }}})
		m, err := NewModel(rt)
		if err != nil {
			t.Fatal(err)
		}
		m.transitionTo(stateProcessing)
		path := addQueueTestImage(t, m)
		queueKey(m, tea.KeyTab, 0)
		m.Update(doneMsg{})
		id := m.queue.awaitingRun
		if id == "" {
			t.Fatal("queue did not start")
		}
		if err := rt.WaitRun(t.Context(), id); err != nil {
			t.Fatal(err)
		}
		typ := controlruntime.EventRunDone
		if fail {
			typ = controlruntime.EventRunFailed
		}
		// The reconnected subscription delivers only a synthetic terminal event.
		m.Update(runtimeEventMsg{event: controlruntime.Event{RunID: id, Type: typ, Source: controlruntime.SourceRef{Kind: "runtime"}}})
		if m.queue.awaiting != nil || len(m.queue.items) > 0 || m.Input.HasContent() {
			t.Fatal("accepted input offered for resend")
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatal("accepted image was deleted", err)
		}
		rt.Close()
	}
}

func TestQueuedChatRequiresSessionCheckedAdmission(t *testing.T) {
	b := &tickFakeBot{}
	// Expose just the base runtime contract, including unscoped StartRun.
	m, err := NewModel(struct{ RuntimeClient }{b})
	if err != nil {
		t.Fatal(err)
	}
	m.transitionTo(stateProcessing)
	enqueueText(m, "queued")
	m.Update(doneMsg{})
	if len(b.submitted) != 0 || !m.queue.managing || len(m.queue.items) != 1 || m.queue.items[0].Value() != "queued" {
		t.Fatalf("submitted=%v queue=%+v", b.submitted, m.queue)
	}
}

func TestQueueRejectsDraftFromAnotherSession(t *testing.T) {
	for _, awaiting := range []bool{false, true} {
		r := &queuedSessionRuntime{current: "A"}
		m, err := NewModel(r)
		if err != nil {
			t.Fatal(err)
		}
		m.transitionTo(stateProcessing)
		enqueueText(m, "for A")
		if awaiting {
			// The first draft has been admitted and is awaiting acknowledgement.
			m.queue.awaiting = m.queue.items[0]
			m.queue.items = nil
		}
		r.current = "B"
		enqueueText(m, "for B")
		r.current = "A"
		if m.Input.Value() != "for B" || m.queue.session != "A" {
			t.Fatalf("awaiting=%t owner=%q composer=%q", awaiting, m.queue.session, m.Input.Value())
		}
		if awaiting {
			if len(m.queue.items) != 0 || m.queue.awaiting.Value() != "for A" {
				t.Fatal("pending acknowledgement lost its original draft")
			}
		} else if len(m.queue.items) != 1 || m.queue.items[0].Value() != "for A" {
			t.Fatal("other session's draft entered the original queue")
		}
	}
}
