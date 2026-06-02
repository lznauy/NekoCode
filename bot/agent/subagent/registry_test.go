package subagent

import (
	"testing"
)

func TestRegisterPluginAgent(t *testing.T) {
	def := AgentDef{
		Name:         "plugin-agent",
		SystemPrompt: "You are a plugin agent.",
		Tools:        []string{"Read", "Grep"},
	}

	at := def.ToAgentType()
	RegisterPlugin(at)

	got, ok := Get("plugin-agent")
	if !ok {
		t.Fatal("plugin agent not found in registry")
	}
	if got.Name != "plugin-agent" {
		t.Errorf("name = %q", got.Name)
	}

	UnregisterPlugin("plugin-agent")
	if _, ok := Get("plugin-agent"); ok {
		t.Error("plugin agent should be gone after unregister")
	}
}
