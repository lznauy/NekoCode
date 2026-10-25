// Package contract defines the versioned wire contract shared by hook adapters.
package contract

import "context"

const Version = 1

type Point string

const (
	SessionStart  Point = "session_start"
	SessionEnd    Point = "session_end"
	RunStart      Point = "run_start"
	RunEnd        Point = "run_end"
	UserSubmit    Point = "user_submit"
	PreModel      Point = "pre_model"
	PreToolUse    Point = "pre_tool_use"
	PostToolUse   Point = "post_tool_use"
	PostToolBatch Point = "post_tool_batch"
	Stop          Point = "stop"
	PreCompact    Point = "pre_compact"
	PostCompact   Point = "post_compact"
	SubagentStart Point = "subagent_start"
	SubagentStop  Point = "subagent_stop"
)

type Action string

const (
	Continue  Action = "continue"
	Reject    Action = "reject"
	Terminate Action = "stop"
)

type Decision struct {
	Action     Action     `json:"action"`
	Reason     string     `json:"reason,omitempty"`
	StopReason StopReason `json:"stop_reason,omitempty"`
}

type StatePatch struct {
	Ints    map[string]int64  `json:"ints"`
	Strings map[string]string `json:"strings"`
}

type Event struct {
	Context      context.Context `json:"-"`
	Version      int             `json:"version"`
	Point        string          `json:"event"`
	HookID       string          `json:"hook_id"`
	ActionIndex  int             `json:"action_index"`
	EvaluationID uint64          `json:"evaluation_id"`
	SessionID    string          `json:"session_id"`
	RunID        string          `json:"run_id"`
	ActorID      string          `json:"actor_id"`
	Tool         string          `json:"tool"`
	Error        bool            `json:"error"`
	Args         map[string]any  `json:"args"`
	Output       string          `json:"output,omitempty"`
	ErrorMessage string          `json:"error_message,omitempty"`
	Batch        []ToolResult    `json:"batch,omitempty"`
	Input        string          `json:"input,omitempty"`
	Intent       string          `json:"intent"`
	Text         string          `json:"text,omitempty"`
	PluginRoot   string          `json:"pluginRoot"`
	State        StatePatch      `json:"state"`
}

type ToolResult struct {
	Name        string         `json:"name"`
	Args        map[string]any `json:"args"`
	Output      string         `json:"output"`
	Error       string         `json:"error"`
	Blocked     bool           `json:"blocked"`
	BlockReason string         `json:"block_reason,omitempty"`
}

type Hint struct {
	Type     string `json:"type"`
	Severity string `json:"severity,omitempty"`
	Content  string `json:"content"`
}

type StopReason string

const (
	StopFormatError   StopReason = "format_error"
	StopInterrupted   StopReason = "interrupted"
	StopCompleted     StopReason = "completed"
	StopPolicyBlocked StopReason = "policy_blocked"
)

func (s StopReason) String() string { return string(s) }

type Result struct {
	Decision    *Decision    `json:"decision,omitempty"`
	Hints       []Hint       `json:"hints,omitempty"`
	StatePatch  *StatePatch  `json:"state_patch,omitempty"`
	Err         error        `json:"-"`
	Hint        *Hint        `json:"hint,omitempty"`
	Stop        *StopReason  `json:"stop,omitempty"`
	BlockTool   *BlockTool   `json:"block_tool,omitempty"`
	RequireTool *RequireTool `json:"require_tool,omitempty"`
	BlockFinal  *BlockFinal  `json:"block_final,omitempty"`
}

type BlockTool struct {
	Tool   string `json:"tool,omitempty"`
	Reason string `json:"reason"`
}

type RequireTool struct {
	Tool   string `json:"tool,omitempty"`
	Reason string `json:"reason"`
}

type BlockFinal struct {
	Reason string `json:"reason"`
}
