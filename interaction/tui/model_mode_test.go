package tui

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"nekocode/mode"
	controlruntime "nekocode/runtime"
)

type modeFakeRuntime struct {
	statusFakeBot
	selected mode.ID
	failure  error
}

func (r *modeFakeRuntime) InteractionMode() controlruntime.InteractionMode {
	return controlruntime.InteractionMode{Selected: r.selected, Options: mode.Definitions()}
}
func (r *modeFakeRuntime) SetInteractionMode(id mode.ID) error {
	if r.failure != nil {
		return r.failure
	}
	r.selected = id
	return nil
}

func TestModePickerPreservesDraftAndAttachments(t *testing.T) {
	r := &modeFakeRuntime{selected: mode.Standard}
	m, err := NewModel(r)
	if err != nil {
		t.Fatal(err)
	}
	m.Input.SetValue("unfinished draft")
	if !m.Input.AddImage("/tmp/mode-draft.png") {
		t.Fatal("cannot add image")
	}
	draft := m.Input.Value()
	m.openModePicker()
	if !m.modePicker {
		t.Fatal("picker did not open")
	}
	m.handleModeKey(tea.KeyPressMsg{Code: tea.KeyDown})
	cmd := m.handleModeKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("selection produced no mutation")
	}
	m.Update(cmd())
	if r.selected != mode.Plan || m.modeChanging {
		t.Fatal("selection did not settle")
	}
	if m.Input.Value() != draft || len(m.Input.ImageAttachments()) != 1 {
		t.Fatal("mode selection destroyed draft")
	}
	clean := ansi.Strip(m.Input.View())
	if !strings.Contains(clean, "Plan") || strings.Index(clean, "Plan") > strings.Index(clean, "F:") && strings.Contains(clean, "F:") {
		t.Fatalf("mode is not before Follow: %s", clean)
	}
	r.failure = errors.New("busy")
	m.openModePicker()
	cmd = m.handleModeKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("standard selection produced no mutation")
	}
	m.Update(cmd())
	if r.selected != mode.Plan || !strings.Contains(ansi.Strip(m.Input.View()), "Plan") || m.Input.Value() != draft {
		t.Fatal("failed mutation changed displayed mode or draft")
	}
}

func TestModePickerClickAndBusyLock(t *testing.T) {
	r := &modeFakeRuntime{selected: mode.Standard}
	m, _ := NewModel(r)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 35})
	view := strings.Split(ansi.Strip(m.View().Content), "\n")
	actual := -1
	for n, line := range view {
		if strings.Contains(line, "Mode:") {
			actual = n
		}
	}
	if actual < 0 || actual != m.modeControlY {
		t.Fatalf("hit row = %d, rendered = %d", m.modeControlY, actual)
	}
	m.Update(tea.MouseClickMsg{X: 3, Y: actual, Button: tea.MouseLeft})
	if !m.modePicker {
		t.Fatal("click did not open picker")
	}
	m.handleModeKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	m.transitionTo(stateProcessing)
	m.openModePicker()
	if m.modePicker {
		t.Fatal("busy picker opened")
	}
}

func TestOneShotPlanDisplaySurvivesStatusAndResetsOnCompletion(t *testing.T) {
	r := &modeFakeRuntime{selected: mode.Standard}
	m, _ := NewModel(r)
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventRunStarted})
	plan, _ := mode.Resolve(mode.Plan)
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventInputAccepted, Payload: controlruntime.MessagePayload{Content: "/plan inspect", InteractionMode: plan.Snapshot}})
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventSystemMessage})
	if !strings.Contains(ansi.Strip(m.Input.View()), "Plan*") {
		t.Fatal("status refresh lost active policy")
	}
	// Steering has no new policy and must not replace the captured one.
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventInputAccepted, Payload: controlruntime.MessagePayload{Content: "more context"}})
	if !strings.Contains(ansi.Strip(m.Input.View()), "Plan*") {
		t.Fatal("steering replaced active policy")
	}
	m.handleRuntimeEvent(controlruntime.Event{Type: controlruntime.EventRunDone})
	if strings.Contains(ansi.Strip(m.Input.View()), "Plan") {
		t.Fatal("one-shot mode stuck after completion")
	}
}

func TestPastingWhileModePickerOpenRestoresInputFocus(t *testing.T) {
	m, _ := NewModel(&modeFakeRuntime{selected: mode.Standard})
	m.openModePicker()
	m.Update(tea.PasteMsg{Content: "pasted draft"})
	if m.modePicker {
		t.Fatal("paste left focus in hidden mode picker")
	}
	before := m.Input.Value()
	m.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	if m.Input.Value() == before {
		t.Fatal("typing remained trapped after paste")
	}
}

func TestLateModeReplyDoesNotOverwriteNewSessionMode(t *testing.T) {
	r := &modeFakeRuntime{selected: mode.Standard}
	m, _ := NewModel(r)
	m.openModePicker()
	m.handleModeKey(tea.KeyPressMsg{Code: tea.KeyDown})
	cmd := m.handleModeKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	reply := cmd()             // Mutation completed in session A; reply is still queued.
	r.selected = mode.Standard // Another client activates session B.
	m.sessionLoadGeneration++
	m.Input.SetValue("session B draft")
	m.refreshRuntimeStatus()
	m.Update(reply)
	if strings.Contains(ansi.Strip(m.Input.View()), "Plan") {
		t.Fatal("late session A reply overwrote session B mode")
	}
	if m.Input.Value() != "session B draft" || m.modeChanging {
		t.Fatal("late reply lost draft or left input locked")
	}
}
