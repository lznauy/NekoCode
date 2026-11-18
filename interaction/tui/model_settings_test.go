package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	controlruntime "nekocode/runtime"
)

type settingsFakeRuntime struct {
	statusFakeBot
	failure  error
	switches int
	efforts  int
}

func (r *settingsFakeRuntime) SwitchModel(name string) (controlruntime.ModelSelection, error) {
	r.switches++
	if r.failure != nil {
		return r.selection, r.failure
	}
	r.selection.Provider = "test"
	r.selection.Model = name
	return r.selection, nil
}
func (r *settingsFakeRuntime) SetReasoningEffort(effort string) error {
	r.efforts++
	if r.failure != nil {
		return r.failure
	}
	r.selection.ReasoningEffort = effort
	return nil
}
func TestSettingChangesUpdateFooterWithoutMessages(t *testing.T) {
	runtime := &settingsFakeRuntime{statusFakeBot: statusFakeBot{selection: controlruntime.ModelSelection{Provider: "test", Model: "old", ReasoningEffort: "low"}}}
	m, err := NewModel(runtime)
	if err != nil {
		t.Fatal(err)
	}
	before := len(m.Messages.Items())
	for _, value := range []string{"/model next", "/effort high", "/effort auto"} {
		if handled, _ := m.tryLocalCommand(value); !handled {
			t.Fatalf("setting not handled: %s", value)
		}
		if len(m.Messages.Items()) != before {
			t.Fatalf("setting added a message: %s", value)
		}
	}
	footer := ansi.Strip(m.Input.View())
	if !strings.Contains(footer, "test/next") || runtime.selection.ReasoningEffort != "" || runtime.switches != 1 || runtime.efforts != 2 {
		t.Fatalf("footer or selection stale: %s %+v", footer, runtime.selection)
	}
}
func TestSettingFailureAndBusyStateRemainVisible(t *testing.T) {
	runtime := &settingsFakeRuntime{failure: errors.New("invalid setting"), statusFakeBot: statusFakeBot{selection: controlruntime.ModelSelection{Model: "old"}}}
	m, _ := NewModel(runtime)
	before := len(m.Messages.Items())
	m.tryLocalCommand("/model next")
	if len(m.Messages.Items()) != before+1 || runtime.selection.Model != "old" {
		t.Fatal("failure was hidden or changed selection")
	}
	m.transitionTo(stateProcessing)
	beforeBusy := len(m.Messages.Items())
	m.tryLocalCommand("/effort high")
	if runtime.efforts != 0 || len(m.Messages.Items()) != beforeBusy+1 {
		t.Fatal("busy mutation executed or error hidden")
	}
}
func TestSettingCommandPreservesImageDraftWithoutNotice(t *testing.T) {
	runtime := &settingsFakeRuntime{}
	m, _ := NewModel(runtime)
	m.Input.SetValue("/model next")
	if !m.Input.AddImage("/tmp/setting-draft.png") {
		t.Fatal("cannot add image")
	}
	draft := m.Input.Value()
	before := len(m.Messages.Items())
	m.startChat("/model next", nil)
	if m.Input.Value() != draft || len(m.Input.ImageAttachments()) != 1 || len(m.Messages.Items()) != before {
		t.Fatal("setting lost image draft or added notification")
	}
}

type silentPermissionRuntime struct{ settingsFakeRuntime }

func (*silentPermissionRuntime) ExecuteLocalCommand(_ context.Context, input string) (string, controlruntime.LocalCommandResult) {
	if input == "/permission full" {
		return "", controlruntime.LocalCommandExecuted
	}
	return "", controlruntime.LocalCommandRequiresIdle
}

func TestPermissionSwitchPreservesImageDraftWithoutNotice(t *testing.T) {
	for _, processing := range []bool{false, true} {
		t.Run(fmt.Sprint(processing), func(t *testing.T) {
			m, err := NewModel(&silentPermissionRuntime{})
			if err != nil {
				t.Fatal(err)
			}
			if processing {
				m.transitionTo(stateProcessing)
			}
			m.Input.SetValue("/permission full")
			if !m.Input.AddImage("/tmp/permission-draft.png") {
				t.Fatal("cannot add image")
			}
			draft, before := m.Input.Value(), len(m.Messages.Items())
			if processing {
				m.Suggestions.OpenMenu("Permission", "", []controlruntime.CommandMenuItem{{Value: "/permission full", Submit: true}})
				m.handleProcessingKey(tea.KeyPressMsg{Code: tea.KeyEnter})
			} else {
				m.startChat("/permission full", nil)
			}
			if m.Input.Value() != draft || len(m.Input.ImageAttachments()) != 1 || len(m.Messages.Items()) != before {
				t.Fatal("permission switch lost image draft or added notification")
			}
		})
	}
}

func TestSettingCommandIncludesAllFooterSettings(t *testing.T) {
	for _, input := range []string{"/model next", "/effort high", "/mode plan", "/permission full"} {
		if !settingCommand(input) {
			t.Errorf("setting not recognized: %q", input)
		}
	}
	for _, input := range []string{"/model", "/effort", "/mode", "/permission", "/local"} {
		if settingCommand(input) {
			t.Errorf("query classified as mutation: %q", input)
		}
	}
}
