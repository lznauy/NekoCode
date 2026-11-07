package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"nekocode/interaction/tui/components/message"
	"nekocode/mode"
	controlruntime "nekocode/runtime"
)

type modeRuntime interface {
	InteractionMode() controlruntime.InteractionMode
	SetInteractionMode(mode.ID) error
}

type modeChangedMsg struct {
	status controlruntime.InteractionMode
	err    error
}

func modeLabel(state controlruntime.InteractionMode) string {
	for _, d := range state.Options {
		if d.ID == state.Selected {
			return d.Label
		}
	}
	if state.Selected != "" && state.Selected != mode.Standard {
		return string(state.Selected) + "?"
	}
	return "Standard"
}

func (m *Model) applyModeStatus(state controlruntime.InteractionMode) {
	if m.activeMode.ID != "" {
		label := string(m.activeMode.ID)
		if d, err := mode.Resolve(m.activeMode.ID); err == nil {
			label = d.Label
		}
		m.Input.SetInteractionMode(label, m.activeModeOneShot, len(state.Options) > 0)
		return
	}
	m.Input.SetInteractionMode(modeLabel(state), false, len(state.Options) > 0)
}

func (m *Model) openModePicker() tea.Cmd {
	source, ok := m.Runtime.(modeRuntime)
	if !ok || m.state != stateReady || m.sessionLoading || m.modeChanging {
		return nil
	}
	state := source.InteractionMode()
	if len(state.Options) == 0 {
		return nil
	}
	items := make([]controlruntime.CommandMenuItem, 0, len(state.Options))
	for _, d := range state.Options {
		items = append(items, controlruntime.CommandMenuItem{Key: string(d.ID), Label: d.Label, Description: d.Description, Current: d.ID == state.Selected})
	}
	m.modePicker = true
	m.commandMenuBack = nil
	m.Suggestions.OpenMenu("Work mode · Ctrl+O", "No modes available", items)
	m.resizeMessages()
	return nil
}

func (m *Model) handleModeKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc", "ctrl+o":
		m.modePicker = false
		m.Suggestions.Hide()
		m.refreshSuggestions()
	case "up", "shift+tab":
		m.Suggestions.Cycle(-1)
	case "down", "tab":
		m.Suggestions.Cycle(1)
	case "enter":
		item, ok := m.Suggestions.Accept()
		if !ok {
			return nil
		}
		source, ok := m.Runtime.(modeRuntime)
		if !ok {
			return nil
		}
		m.modePicker, m.modeChanging = false, true
		m.resizeMessages()
		return func() tea.Msg {
			err := source.SetInteractionMode(mode.ID(item.Key))
			return modeChangedMsg{status: source.InteractionMode(), err: err}
		}
	}
	m.resizeMessages()
	return nil
}

func (m *Model) applyModeChange(msg modeChangedMsg) tea.Cmd {
	m.modeChanging = false
	m.applyModeStatus(msg.status)
	if msg.err != nil {
		m.Messages.AddMessage(message.ChatMessage{Role: "error", Content: "Mode unchanged: " + msg.err.Error()})
	}
	// Restoring suggestions reads the draft without replacing text or attachments.
	m.refreshSuggestions()
	return nil
}

func (m *Model) showRunMode(p controlruntime.MessagePayload) {
	if p.InteractionMode.ID == "" {
		return
	}
	m.activeMode, m.activeModeOneShot = p.InteractionMode, mode.OneShot(strings.TrimSpace(p.Content))
	label := string(p.InteractionMode.ID)
	if d, err := mode.Resolve(p.InteractionMode.ID); err == nil {
		label = d.Label
	}
	m.Input.SetInteractionMode(label, m.activeModeOneShot, true)
}
