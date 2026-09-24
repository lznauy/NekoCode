// Package protocol defines the transport- and UI-neutral contract shared by
// the bot foundation and runtime adapters.
package protocol

import "encoding/json"

type PhaseFunc func(string)

const (
	PhaseReady     = "Ready"
	PhaseWaiting   = "Waiting"
	PhaseThinking  = "Thinking"
	PhaseReasoning = "Reasoning"
	PhaseRunning   = "Running"
)

type StepAction string

// ToolDecision is trusted permission metadata carried separately from a
// tool's display preview so tool-controlled text cannot forge a verdict.
type ToolDecision string

const (
	ToolDecisionJevSafe           ToolDecision = "jev_safe"
	ToolDecisionJevDangerous      ToolDecision = "jev_dangerous"
	ToolDecisionJevURLSafe        ToolDecision = "jev_url_safe"
	ToolDecisionJevURLUnavailable ToolDecision = "jev_url_unavailable"
	ToolDecisionJevURLRisky       ToolDecision = "jev_url_risky"
)

const (
	StepActionChat            StepAction = "chat"
	StepActionRunSummary      StepAction = "run_summary"
	StepActionSubAgentText    StepAction = "subagent_text"
	StepActionSubAgentReason  StepAction = "subagent_reasoning"
	StepActionSubAgentMessage StepAction = "subagent_message"
	StepActionThink           StepAction = "think"
	StepActionToolStart       StepAction = "tool_start"
	StepActionToolBlocked     StepAction = "tool_blocked"
	StepActionToolPreview     StepAction = "tool_preview"
	StepActionExecuteTool     StepAction = "execute_tool"
	StepActionSubAgentStart   StepAction = "sub_agent_start"
	StepActionSubAgentEnd     StepAction = "sub_agent_end"
)

type RunSummary struct {
	StepCount  int    `json:"step_count"`
	StopReason string `json:"stop_reason"`
}

type SubAgentOutput struct {
	ID   string `json:"subagent_id"`
	Kind string `json:"kind"`
	Text string `json:"text"`
}

type StepEvent struct {
	Summary         *RunSummary
	Compaction      *CompactionEvent
	Action          StepAction
	CallID          string
	ToolName        string
	ToolArgs        string
	ToolInput       json.RawMessage
	Output          string
	Decision        ToolDecision
	IsError         bool
	SubAgentID      string
	SubAgentType    string
	SubAgentProfile string
	SubAgentSkills  []string
	SubAgentColor   int
}

// CompactionEvent is a display-only projection of one summary operation.
// Terminal events include full content to recover any missed deltas.
type CompactionEvent struct {
	ID             string            `json:"id"`
	Status         CompactionStatus  `json:"status"`
	Trigger        CompactionTrigger `json:"trigger"`
	Delta          string            `json:"delta,omitempty"`
	Summary        string            `json:"summary,omitempty"`
	Error          string            `json:"error,omitempty"`
	Warning        string            `json:"warning,omitempty"`
	BeforeTokens   int               `json:"beforeTokens"`
	AfterTokens    int               `json:"afterTokens"`
	BeforeMessages int               `json:"beforeMessages"`
	AfterMessages  int               `json:"afterMessages"`
	ElapsedMs      int64             `json:"elapsedMs"`
}

type CompactionStatus string

const (
	CompactionStarted   CompactionStatus = "started"
	CompactionDelta     CompactionStatus = "delta"
	CompactionCompleted CompactionStatus = "completed"
	CompactionFailed    CompactionStatus = "failed"
)

func (s CompactionStatus) Valid() bool {
	return s == CompactionStarted || s == CompactionDelta || s == CompactionCompleted || s == CompactionFailed
}

type CompactionTrigger string

const (
	CompactionAuto   CompactionTrigger = "auto"
	CompactionManual CompactionTrigger = "manual"
)

func (t CompactionTrigger) Valid() bool {
	return t == CompactionAuto || t == CompactionManual
}

const StepActionCompaction StepAction = "compaction"

type CommandAction string

const (
	CommandIgnored  CommandAction = "ignored"
	CommandHandled  CommandAction = "handled"
	CommandContinue CommandAction = "continue"
)

type CommandResult struct {
	Action     CommandAction
	Output     string
	AgentInput string
}

// LocalCommandResult classifies the outcome of ExecuteLocalCommand: commands
// that neither read nor mutate conversation context run immediately, even
// while a task is in progress; everything else goes through the run path.
type LocalCommandResult int

const (
	// LocalCommandNotCommand: input is not a registered command.
	LocalCommandNotCommand LocalCommandResult = iota
	// LocalCommandExecuted: a during-task-safe command ran immediately.
	LocalCommandExecuted
	// LocalCommandRequiresIdle: input is a command but needs the run path,
	// and therefore an idle runtime.
	LocalCommandRequiresIdle
)

// CommandMenu is a transport-neutral, dynamically generated command picker.
// Each item contains the complete input to place in the command line.
type CommandMenu struct {
	Title string            `json:"title"`
	Empty string            `json:"empty,omitempty"`
	Items []CommandMenuItem `json:"items"`
}

type CommandMenuItem struct {
	Key         string `json:"key,omitempty"`
	Value       string `json:"value"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	Submit      bool   `json:"submit,omitempty"`
	// Current marks the item as already active, so UIs can highlight it and
	// refuse to re-select it.
	Current bool `json:"current,omitempty"`
}

type TodoItem struct {
	Content string `json:"content"`
	Status  string `json:"status"`
}

type TodoFunc func([]TodoItem)

func CountCompleted(items []TodoItem) int {
	n := 0
	for _, item := range items {
		if item.Status == "completed" {
			n++
		}
	}
	return n
}

type ConfirmKind string

const (
	ConfirmKindPermission ConfirmKind = "permission"
	ConfirmKindInstall    ConfirmKind = "install"
)

type ApprovalScope string

const (
	ApprovalScopeOnce    ApprovalScope = "once"
	ApprovalScopeProject ApprovalScope = "project"
)

// ApprovalContext contains the security facts behind a confirmation. Keeping
// these facts separate from tool Args gives policy enforcement and every UI a
// single typed contract; Args remains the original tool invocation only.
type ApprovalContext struct {
	Risk         string        `json:"risk,omitempty"`
	Reason       string        `json:"reason,omitempty"`
	Structures   []string      `json:"structures,omitempty"`
	Capabilities []string      `json:"capabilities,omitempty"`
	Scope        ApprovalScope `json:"scope,omitempty"`
	Workspace    string        `json:"workspace,omitempty"`
	Sandbox      string        `json:"sandbox,omitempty"`
	WritePaths   []string      `json:"write_paths,omitempty"`
	Combined     bool          `json:"combined,omitempty"`
}

func (c *ApprovalContext) CanRemember() bool {
	return c == nil || c.Scope != ApprovalScopeOnce
}

func (c *ApprovalContext) Clone() *ApprovalContext {
	if c == nil {
		return nil
	}
	clone := *c
	clone.Structures = append([]string(nil), c.Structures...)
	clone.Capabilities = append([]string(nil), c.Capabilities...)
	clone.WritePaths = append([]string(nil), c.WritePaths...)
	return &clone
}

type ConfirmRequest struct {
	ToolName string
	Args     map[string]any
	Kind     ConfirmKind
	Approval *ApprovalContext
	// CallID identifies the tool call this request belongs to, when known.
	// It lets interaction surfaces attach the approval to the right tool.
	CallID     string
	SubAgentID string
	// Deprecated: capabilities live in Approval and are approved atomically.
	CanEscalatePermission bool
}

type ConfirmReply struct {
	Allowed  bool
	Remember bool
	// Deprecated: an allowed unified request already covers displayed
	// capabilities. This field is accepted for source compatibility and ignored.
	AllowWithPermission bool
}

func AllowOnce() ConfirmReply       { return ConfirmReply{Allowed: true} }
func AllowRemembered() ConfirmReply { return ConfirmReply{Allowed: true, Remember: true} }
func Deny() ConfirmReply            { return ConfirmReply{} }

func NewConfirmRequest(toolName string, args map[string]any, kind ConfirmKind) ConfirmRequest {
	return ConfirmRequest{
		ToolName: toolName,
		Args:     args,
		Kind:     kind,
	}
}

func NewApprovalRequest(toolName string, args map[string]any, kind ConfirmKind, approval *ApprovalContext) ConfirmRequest {
	request := NewConfirmRequest(toolName, args, kind)
	request.Approval = approval.Clone()
	return request
}

type ConfirmFunc func(ConfirmRequest) ConfirmReply

type QuestionOption struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

type QuestionItem struct {
	Header   string           `json:"header,omitempty"`
	Question string           `json:"question"`
	Options  []QuestionOption `json:"options"`
	Multiple bool             `json:"multiple,omitempty"`
	Custom   bool             `json:"custom,omitempty"`
}

type QuestionReply struct {
	Answers  [][]string `json:"answers,omitempty"`
	Rejected bool       `json:"rejected,omitempty"`
}

type QuestionRequest struct {
	Questions []QuestionItem
}

func NewQuestionRequest(questions []QuestionItem) QuestionRequest {
	return QuestionRequest{
		Questions: questions,
	}
}

type QuestionFunc func(QuestionRequest) QuestionReply

// Metrics is the bot's operational measurement snapshot.
type Metrics struct {
	PromptTokens      int    `json:"promptTokens"`
	CompletionTokens  int    `json:"completionTokens"`
	TurnPrompt        int    `json:"turnPrompt"`
	TurnCompletion    int    `json:"turnCompletion"`
	TurnTotal         int    `json:"turnTotal"`
	TurnInput         int    `json:"turnInput"`
	TurnCached        int    `json:"turnCached"`
	TurnNew           int    `json:"turnNew"`
	TurnOutput        int    `json:"turnOutput"`
	TurnReasoning     int    `json:"turnReasoning,omitempty"`
	TurnCacheReported bool   `json:"turnCacheReported"`
	ContextTokens     int    `json:"contextTokens"`
	ContextBudget     int    `json:"contextBudget"`
	CompactCount      int    `json:"compactCount"`
	Duration          string `json:"duration"`
}

// HasTurnUsage reports whether the snapshot contains an actual LLM request.
// Command-only runs intentionally carry no turn usage.
func (m Metrics) HasTurnUsage() bool { return m.TurnInput > 0 || m.TurnOutput > 0 }

// WorkspaceChanges is a privacy-safe summary of the current Git worktree.
type WorkspaceChanges struct {
	Added     int  `json:"added"`
	Deleted   int  `json:"deleted"`
	Untracked int  `json:"untracked"`
	Available bool `json:"available"`
}

// CheckpointInfo is a transport-neutral summary of a rewind point.
type CheckpointInfo struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description"`
}
