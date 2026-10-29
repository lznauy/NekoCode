package subagent

import (
	_ "embed"
	"encoding/json"
	ctxmgr "nekocode/bot/contextmgr"

	"nekocode/bot/extension/agentprofile"
	"nekocode/bot/extension/tool/runtime/execution"
	"nekocode/bot/extension/tool/runtime/runner"
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
	Decision  protocol.ToolDecision
	IsError   bool
}

type AttemptUsage struct {
	TotalTokens     int
	CacheHitTokens  int
	CacheMissTokens int
	ToolUseCount    int
	DurationMs      int64
	ContextTokens   int
	ContextWindow   int
}

// OutputEvent identifies the pending transcript record so observers can replace
// a live preview with its committed message without duplicating it.
type OutputEvent struct {
	Action          protocol.StepAction
	Text, Reasoning string
	MessageSeq      int
}

// RunConfig carries resolved dependencies; the engine does not discover files
// or consult mutable extension registries during a run.
type RunConfig struct {
	// ResumeContext restores a task; persistence remains owned by the assembly layer.
	ResumeContext     *ctxmgr.ManagerSnapshot
	IsolatedWorkspace bool
	SubagentID        string
	SaveContext       func(ctxmgr.ManagerSnapshot, string, string, AttemptUsage) error

	Prompt              string
	Profile             Profile
	SkillContents       []string
	ProjectInstructions string
	ContextWindow       int
	CompactionWindow    int
	AutoCompactPercent  int
	OnStarted           func()
	OnText              func(string)
	OnReasoning         func(string)
	OnOutput            func(OutputEvent)
	// OnMessage receives non-empty complete model text, not a per-step end marker.
	OnMessage            func(string)
	OnPhase              func(string)
	AddTokens            func(int, int)
	RecordLLMUsage       func(providertypes.StreamUsage)
	SessionID            string
	ConfirmFn            protocol.ConfirmFunc
	FullAccess           func() bool
	ConfigurePermissions func(*runner.Executor)
	Handoff              string // unverified prior-agent evidence, not instructions
	OnToolCall           func(ToolCallEvent)
	ToolState            *execution.ExecutionState
	// Evaluated per model call so newly approved roots become visible.
	Environment prompt.EnvironmentProvider
	// Audit only; authorization uses the actor-local guard created by Run.
	Policy *policy.Policy
	guard  *policy.Policy
}
