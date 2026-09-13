package subagent

import (
	_ "embed"
	"fmt"
	"os"

	goyaml "gopkg.in/yaml.v3"

	"nekocode/bot/extension/tool/runtime/execution"
	"nekocode/bot/policy"
	"nekocode/bot/prompt"
	providertypes "nekocode/bot/provider/types"
	"nekocode/protocol"
	"nekocode/util/registry"
	"nekocode/util/yaml"
)

//go:embed prompts/subagent.md
var builtinPrompt string

// Profile defines a sub-agent's prompt and deterministic tool ceiling. Work
// methods such as investigation, planning, or review are supplied by skills.
type Profile struct {
	Name         string
	SystemPrompt string
	Tools        []string
}

// ToolCallEvent is fired for each tool executed inside a sub-agent.
type ToolCallEvent struct {
	Action   protocol.StepAction
	CallID   string
	ToolName string
	ToolArgs string
	Output   string
	IsError  bool
}

type RunConfig struct {
	Prompt             string
	Profile            Profile
	SkillContents      []string
	ContextWindow      int
	AutoCompactPercent int
	OnPhase            func(phase string)
	AddTokens          func(prompt, compl int)
	RecordLLMUsage     func(usage providertypes.StreamUsage)
	SessionID          string
	ConfirmFn          protocol.ConfirmFunc
	// FullAccess, when non-nil and returning true, puts the sub-agent's tool
	// executor into full-takeover mode (no approval prompts), mirroring the
	// main agent's permission mode.
	FullAccess func() bool
	Handoff    string                 // unverified prior-agent evidence prepended to the delegated task
	OnToolCall func(ev ToolCallEvent) // sub-agent tool execution callback
	ToolState  *execution.ExecutionState
	// Environment, when non-nil, is evaluated for every model call so roots
	// approved while the parent run is active become visible immediately.
	Environment prompt.EnvironmentProvider
	// Policy, when non-nil, receives audit-only outcomes for the main run. Its
	// hooks and authorization state are not evaluated inside this actor.
	Policy *policy.Policy
	// guard is created by Engine.Run for every invocation. Callers cannot omit
	// or share actor-local write authorization state.
	guard *policy.Policy
}

var (
	builtins = registry.New[Profile](func(a Profile) string { return a.Name })
	plugins  = registry.New[Profile](func(a Profile) string { return a.Name })
)

func register(a Profile) { builtins.Register(a) }

func init() {
	register(Profile{
		Name: "coder", SystemPrompt: builtinPrompt,
		Tools: []string{"read", "write", "edit", "shell", "process", "grep", "glob", "list", "web_search", "web_fetch", "web_extract"},
	})
	register(Profile{
		Name: "explore", SystemPrompt: builtinPrompt,
		Tools: []string{"read", "grep", "glob", "list", "web_search", "web_fetch", "web_extract"},
	})
}

// RegisterPlugin registers a plugin-provided profile.
func RegisterPlugin(a Profile) { plugins.Register(a) }

// UnregisterPlugin removes a plugin-provided agent type by name.
func UnregisterPlugin(name string) { plugins.Unregister(name) }

// GetProfile looks up a profile by name, checking builtins first, then plugins.
func GetProfile(name string) (Profile, bool) {
	if a, ok := builtins.Get(name); ok {
		return a, ok
	}
	return plugins.Get(name)
}

// AgentDef is parsed from an agents/*.md file (Claude Code format).
type AgentDef struct {
	Name         string   `yaml:"name"`
	Tools        []string `yaml:"tools"`
	SystemPrompt string   // markdown body (after frontmatter)
}

// ParseAgentMD parses a single agents/*.md file.
func ParseAgentMD(path string) (*AgentDef, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read agent file: %w", err)
	}
	yamlBytes, body, err := yaml.ParseYAMLFrontmatter(string(data))
	if err != nil {
		return nil, err
	}
	var def AgentDef
	if err := goyaml.Unmarshal(yamlBytes, &def); err != nil {
		return nil, fmt.Errorf("invalid frontmatter: %w", err)
	}
	if def.Name == "" {
		return nil, fmt.Errorf("missing required field: name")
	}
	def.SystemPrompt = body
	return &def, nil
}

// ToProfile converts an AgentDef to a Profile for the subagent engine.
func (d *AgentDef) ToProfile() Profile {
	return Profile{
		Name:         d.Name,
		SystemPrompt: d.SystemPrompt,
		Tools:        d.Tools,
	}
}
