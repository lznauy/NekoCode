package tui

import (
	"errors"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	tea "charm.land/bubbletea/v2"
	controlruntime "nekocode/runtime"
)

type renameFakeRuntime struct {
	commandFakeBot
	renamedID, name string
	err             error
	calls           int
}

func (r *renameFakeRuntime) RenameSession(id, name string) error {
	r.calls++
	if r.err != nil {
		return r.err
	}
	r.renamedID, r.name = id, name
	for key, menu := range r.menus {
		for i := range menu.Items {
			if menu.Items[i].Key == id {
				menu.Items[i].Tag = name
			}
		}
		r.menus[key] = menu
	}
	return nil
}
func newRenameModel(t *testing.T, input string) (*Model, *renameFakeRuntime) {
	t.Helper()
	r := &renameFakeRuntime{commandFakeBot: commandFakeBot{menus: map[string]controlruntime.CommandMenu{input: {Title: "Sessions", Items: []controlruntime.CommandMenuItem{
		{Key: "first", Label: "first", Value: "/sessions first", Submit: true},
		{Key: "second", Label: "second", Tag: "已有名称", Value: "/sessions second", Submit: true},
	}}}}}
	m, err := NewModel(r)
	if err != nil {
		t.Fatal(err)
	}
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.Input.SetValue(input)
	if !m.Input.AddImage("/tmp/session-draft.png") {
		t.Fatal("cannot attach image")
	}
	// Menu selection preserves the underlying draft and its attachments.
	m.openCommandMenu(input)
	m.Suggestions.Cycle(1)
	return m, r
}
func TestSessionMenuRenameSelectedItemAndPreserveDraft(t *testing.T) {
	for _, processing := range []bool{false, true} {
		t.Run(map[bool]string{false: "idle", true: "running"}[processing], func(t *testing.T) {
			m, r := newRenameModel(t, "/sessions")
			if processing {
				m.transitionTo(stateProcessing)
			}
			draft, before := m.Input.Value(), m.Messages.Len()
			m.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
			if m.sessionName == nil || m.sessionName.input.Value() != "已有名称" {
				t.Fatal("editor did not open with existing name")
			}
			view := ansi.Strip(m.View().Content)
			if !m.Suggestions.IsMenu() || !strings.Contains(view, "已有名称") || strings.Index(view, "second") > strings.Index(view, "tag ›") {
				t.Fatalf("editor replaced list or appeared before selected session:\n%s", view)
			}
			if strings.Contains(view, "enter select") || strings.Contains(view, "会话名称") {
				t.Fatal("inline editor kept misleading navigation hints or dialog chrome")
			}
			if !strings.Contains(m.View().Content, "Enter 保存") {
				t.Fatal("editor missing from view")
			}
			m.sessionName.input.SetValue("")
			m.Update(tea.PasteMsg{Content: "修复登录 #auth"})
			m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			selected, ok := m.Suggestions.Selected()
			if !ok || selected.Key != "second" || selected.Label != "second" || selected.Tag != "修复登录 #auth" || r.renamedID != "second" {
				t.Fatalf("rename affected wrong item: %+v %+v", selected, r)
			}
			if m.sessionName != nil || m.Input.Value() != draft || len(m.Input.ImageAttachments()) != 1 || m.Messages.Len() != before {
				t.Fatal("rename changed draft or emitted message")
			}
			m.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
			m.sessionName.input.SetValue("")
			m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			selected, _ = m.Suggestions.Selected()
			if processing && m.state != stateProcessing {
				t.Fatal("tag edit interrupted active run")
			}
			if selected.Label != "second" || r.name != "" {
				t.Fatal("clear name failed")
			}
		})
	}
}
func TestSessionRenameCancelAndFailure(t *testing.T) {
	m, r := newRenameModel(t, "/sessions")
	m.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.sessionName != nil || r.calls != 0 || !m.Suggestions.IsMenu() {
		t.Fatal("cancel saved name or failed to restore list")
	}
	m.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	r.err = errors.New("write failed")
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.sessionName == nil || !strings.Contains(m.View().Content, "write failed") {
		t.Fatal("failure was hidden or closed editor")
	}
	m.Update(tea.PasteMsg{Content: "修订"})
	if strings.Contains(m.View().Content, "write failed") || !strings.Contains(m.View().Content, "Enter 保存") {
		t.Fatal("editing after a failed save did not restore the key hints")
	}
	m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if m.sessionName != nil {
		t.Fatal("ctrl+c did not cancel")
	}
}
func TestSessionRenameEventDoesNotReloadConversation(t *testing.T) {
	m, r := newRenameModel(t, "/sessions")
	if err := r.RenameSession("second", "updated externally"); err != nil {
		t.Fatal(err)
	}
	if cmd := m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventSessionChanged, Payload: controlruntime.SessionPayload{Reason: "rename", ChangedID: "second"}}); cmd != nil || m.sessionLoading {
		t.Fatal("metadata event reloaded conversation")
	}
	if selected, ok := m.Suggestions.Selected(); !ok || selected.Key != "second" || selected.Tag != "updated externally" {
		t.Fatalf("metadata event did not refresh selected row: %+v", selected)
	}
}

func TestSessionNameEditorClosesWhenAnotherRunStarts(t *testing.T) {
	m, _ := newRenameModel(t, "/sessions")
	m.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventRunStarted})
	if m.sessionName != nil {
		t.Fatal("editor kept focus after run started")
	}
}

func TestSessionRenameDoesNotInterceptOtherMenus(t *testing.T) {
	m, _ := newRenameModel(t, "/sessions")
	m.Input.SetValue("/other")
	m.refreshSuggestions()
	m.Suggestions.OpenMenu("Other", "", []controlruntime.CommandMenuItem{{Key: "other", Label: "other"}})
	m.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	if m.sessionName != nil {
		t.Fatal("stale session context intercepted another menu")
	}
}

func TestSessionNameEditorSuspendsWhileQuestionOwnsFocus(t *testing.T) {
	m, _ := newRenameModel(t, "/sessions")
	m.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	m.sessionName.input.SetValue("未保存名称")
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventQuestionRequested, Payload: controlruntime.QuestionView{ID: "q", Questions: []controlruntime.QuestionItem{{Question: "请选择", Options: []controlruntime.QuestionOption{{Label: "是"}}}}}})
	if m.state != stateQuestioning || strings.Contains(m.View().Content, "Enter 保存") {
		t.Fatal("name editor stole question focus")
	}
	if m.sessionName == nil || m.sessionName.input.Value() != "未保存名称" {
		t.Fatal("question lost pending name")
	}
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventQuestionResolved, Payload: controlruntime.QuestionView{ID: "q"}})
	if m.state != stateReady || !strings.Contains(m.View().Content, "Enter 保存") {
		t.Fatal("editor did not resume after question")
	}
}

func TestSessionNamePanelFitsTerminalAndKeepsErrorHeightStable(t *testing.T) {
	m, _ := newRenameModel(t, "/sessions")
	m.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	editor := m.sessionName
	editor.id = "20261118T150405-123456789"
	editor.input.SetValue(strings.Repeat("中文名称", 20))
	for _, width := range []int{10, 16, 32, 80, 140} {
		m.Update(tea.WindowSizeMsg{Width: width, Height: 30})
		for _, failure := range []string{"", strings.Repeat("保存失败：只读目录", 20) + "\n请重试"} {
			editor.err = failure
			panel := editor.view(width)
			if lipgloss.Height(panel) != sessionNameHeight {
				t.Fatalf("panel height changed at width %d", width)
			}
			for _, line := range strings.Split(panel, "\n") {
				if lipgloss.Width(line) > width {
					t.Fatalf("panel overflows width %d: %q", width, ansi.Strip(line))
				}
			}
		}
	}
	editor.err = ""
	editor.input.SetValue("修复登录 #auth")
	editor.resize(80)
	t.Log("\n" + ansi.Strip(editor.view(80)))
}

func TestUnrelatedSessionDeletionPreservesPendingName(t *testing.T) {
	m, _ := newRenameModel(t, "/sessions")
	m.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	m.sessionName.input.SetValue("未保存名称")
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventSessionChanged, Payload: controlruntime.SessionPayload{
		Reason: "delete", ChangedID: "first",
	}})
	if m.sessionName == nil || m.sessionName.input.Value() != "未保存名称" {
		t.Fatal("deleting another session discarded the pending name")
	}
}

func TestDelayedDraftPasteDoesNotCloseNameEditorMenu(t *testing.T) {
	m, _ := newRenameModel(t, "/sessions")
	draft := m.Input.Value()
	paste := clipboardImageMsg{scope: m.clipboardScope(), text: "delayed draft paste"}
	m.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	m.Update(paste)
	if m.Input.Value() != draft || m.sessionName == nil || !m.Suggestions.IsMenu() {
		t.Fatalf("stale paste changed draft from %q to %q (editor=%v menu=%v)", draft, m.Input.Value(), m.sessionName != nil, m.Suggestions.IsMenu())
	}
}

func TestModeEventKeepsSessionNameEditorVisible(t *testing.T) {
	m, _ := newRenameModel(t, "/sessions")
	m.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventInteractionModeChanged})
	if m.sessionName == nil || !strings.Contains(m.View().Content, "Enter 保存") {
		t.Fatal("mode event hid the active session name editor")
	}
}
