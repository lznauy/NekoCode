package subagent

import (
	_ "embed"
	"encoding/json"

	"nekocode/bot/extension/agentprofile"
	"nekocode/bot/extension/tool/runtime/execution"
	"nekocode/bot/policy"
	"nekocode/bot/prompt"
	providertypes "nekocode/bot/provider/types"
	"nekocode/protocol"
)

//go:embed prompts/subagent.md
var builtinPrompt string

type Profile = agentprofile.Profile

type ToolCallEvent struct {
	Action    protocol.StepAction
	CallID    string
	ToolName  string
	ToolArgs  string
	ToolInput json.RawMessage
	Output    string
	IsError   bool
}

// RunConfig carries resolved dependencies; the engine does not discover files
// or consult mutable extension registries during a run.
type RunConfig struct {
	Prompt              string
	Profile             Profile
	SkillContents       []string
	ProjectInstructions string
	ContextWindow       int
	AutoCompactPercent  int
	OnText              func(string)
	OnReasoning         func(string)
	// OnMessage receives non-empty complete model text, not a per-step end marker.
	OnMessage      func(string)
	OnPhase        func(string)
	AddTokens      func(int, int)
	RecordLLMUsage func(providertypes.StreamUsage)
	SessionID      string
	ConfirmFn      protocol.ConfirmFunc
	FullAccess     func() bool
	Handoff        string // unverified prior-agent evidence, not instructions
	OnToolCall     func(ToolCallEvent)
	ToolState      *execution.ExecutionState
	// Evaluated per model call so newly approved roots become visible.
	Environment prompt.EnvironmentProvider
	// Audit only; authorization uses the actor-local guard created by Run.
	Policy *policy.Policy
	guard  *policy.Policy
}
