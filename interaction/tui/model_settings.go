package tui

import (
	"fmt"
	"strings"

	"nekocode/interaction/tui/components/message"
	controlruntime "nekocode/runtime"
)

type modelSwitcher interface {
	SwitchModel(string) (controlruntime.ModelSelection, error)
}
type effortSwitcher interface{ SetReasoningEffort(string) error }

// settingCommand identifies mutations whose successful result is already
// visible in the input footer. Queries and usage feedback keep their normal UI.
func settingCommand(value string) bool {
	fields := strings.Fields(value)
	return len(fields) > 1 && (fields[0] == "/model" || fields[0] == "/effort" || fields[0] == "/mode" || fields[0] == "/permission")
}

func (m *Model) changeModelSetting(value string) bool {
	fields := strings.Fields(value)
	if !settingCommand(value) {
		return false
	}
	var change func() error
	switch fields[0] {
	case "/model":
		source, ok := m.Runtime.(modelSwitcher)
		if !ok {
			return false
		}
		change = func() error { _, err := source.SwitchModel(strings.Join(fields[1:], " ")); return err }
	case "/effort":
		source, ok := m.Runtime.(effortSwitcher)
		if !ok {
			return false
		}
		change = func() error {
			if len(fields) != 2 {
				return fmt.Errorf("Usage: /effort <value>")
			}
			effort := strings.ToLower(fields[1])
			if effort == "auto" {
				effort = ""
			}
			return source.SetReasoningEffort(effort)
		}
	default:
		return false
	}
	if m.state == stateProcessing {
		m.Messages.AddMessage(message.ChatMessage{Role: "error", Content: "命令 " + value + " 需在任务结束后执行"})
		return true
	}
	if err := change(); err != nil {
		m.Messages.AddMessage(message.ChatMessage{Role: "error", Content: err.Error()})
	}
	m.refreshRuntimeStatus()
	m.resizeMessages()
	return true
}
