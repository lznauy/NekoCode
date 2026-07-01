package subagent

import (
	_ "embed"

	"nekocode/bot/tools"
	"nekocode/common"
)

//go:embed prompts/executor.md
var executorPrompt string

//go:embed prompts/verify.md
var verifyPrompt string

//go:embed prompts/researcher.md
var researcherPrompt string

type AgentType struct {
	Name               string
	SystemPrompt       string
	Tools              []string
	OmitProjectContext bool
}

// ToolCallEvent is fired for each tool executed inside a sub-agent.
type ToolCallEvent struct {
	Action   string // "tool_start" | "execute_tool"
	ToolName string
	ToolArgs string
	Output   string
}

type RunConfig struct {
	Prompt         string
	AgentType      AgentType
	Cwd            string
	ProjectContext string
	Thoroughness   string
	ContextWindow  int
	OnPhase        func(phase string)
	AddTokens      func(prompt, compl int)
	ConfirmFn      common.ConfirmFunc
	Handoff        string                 // injected into system prompt for cross-agent context
	OnToolCall     func(ev ToolCallEvent) // sub-agent tool execution callback
	ToolState      *tools.ExecutionState
}

var (
	builtins = common.NewRegistry[AgentType](func(a AgentType) string { return a.Name })
	plugins  = common.NewRegistry[AgentType](func(a AgentType) string { return a.Name })
)

func register(a AgentType) { builtins.Register(a) }

func init() {
	register(AgentType{
		Name: "executor", SystemPrompt: executorPrompt,
		Tools: []string{"read", "write", "edit", "bash", "grep", "glob", "list"},
	})
	register(AgentType{
		Name: "verify", SystemPrompt: verifyPrompt,
		Tools: []string{"read", "grep", "glob", "list", "bash"},
	})
	register(AgentType{
		Name: "researcher", SystemPrompt: researcherPrompt,
		Tools:              []string{"read", "grep", "glob", "list", "web_search", "web_fetch"},
		OmitProjectContext: true,
	})
}

// RegisterPlugin registers a plugin-provided agent type.
func RegisterPlugin(a AgentType) { plugins.Register(a) }

// UnregisterPlugin removes a plugin-provided agent type by name.
func UnregisterPlugin(name string) { plugins.Unregister(name) }

// Get looks up an agent type by name, checking builtins first, then plugins.
func Get(name string) (AgentType, bool) {
	if a, ok := builtins.Get(name); ok {
		return a, ok
	}
	return plugins.Get(name)
}
