package subagent

import (
	"sync"

	"nekocode/common"
)

type AgentType struct {
	Name               string
	SystemPrompt       string
	Tools              []string
	OmitProjectContext bool
}

type RunConfig struct {
	Prompt          string
	AgentType       AgentType
	Cwd             string
	ProjectContext  string
	Thoroughness    string
	ContextWindow     int
	OnPhase         func(phase string)
	AddTokens       func(prompt, compl int)
	DisableThinking bool
	ConfirmFn       common.ConfirmFunc
	Handoff         string // injected into system prompt for cross-agent context
}

var builtins = map[string]AgentType{}
var (
	pluginMu sync.RWMutex
	plugins  = map[string]AgentType{}
)

func register(a AgentType) { builtins[a.Name] = a }

// RegisterPlugin registers a plugin-provided agent type.
func RegisterPlugin(a AgentType) {
	pluginMu.Lock()
	plugins[a.Name] = a
	pluginMu.Unlock()
}

// UnregisterPlugin removes a plugin-provided agent type by name.
func UnregisterPlugin(name string) {
	pluginMu.Lock()
	delete(plugins, name)
	pluginMu.Unlock()
}

func Get(name string) (AgentType, bool) {
	if a, ok := builtins[name]; ok {
		return a, ok
	}
	pluginMu.RLock()
	a, ok := plugins[name]
	pluginMu.RUnlock()
	return a, ok
}

