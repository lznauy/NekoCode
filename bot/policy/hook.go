package policy

import (
	"context"
	"nekocode/bot/policy/contract"
	"time"
)

// HookPoint identifies one concrete place in the agent lifecycle.
type HookPoint = contract.Point

const (
	SessionStart  = contract.SessionStart
	SessionEnd    = contract.SessionEnd
	RunStart      = contract.RunStart
	RunEnd        = contract.RunEnd
	PreCompact    = contract.PreCompact
	PostCompact   = contract.PostCompact
	SubagentStart = contract.SubagentStart
	SubagentStop  = contract.SubagentStop
	UserSubmit    = contract.UserSubmit
	PreModel      = contract.PreModel
	PreToolUse    = contract.PreToolUse
	PostToolUse   = contract.PostToolUse
	PostToolBatch = contract.PostToolBatch
	Stop          = contract.Stop
)

type Hint = contract.Hint
type StopReason = contract.StopReason
type Result = contract.Result
type BlockTool = contract.BlockTool
type RequireTool = contract.RequireTool
type BlockFinal = contract.BlockFinal
type Decision = contract.Decision
type Action = contract.Action

const (
	StopFormatError   = contract.StopFormatError
	StopInterrupted   = contract.StopInterrupted
	StopCompleted     = contract.StopCompleted
	StopPolicyBlocked = contract.StopPolicyBlocked
	Continue          = contract.Continue
	Reject            = contract.Reject
	Terminate         = contract.Terminate
)

// Hook is the extension contract exposed by policy. Facts are read-only;
// Int/String store state private to this hook for deduplication and latches.
type Hook struct {
	// Guard failures reject the current action; observer failures only diagnose.
	Guard           bool
	Timeout         time.Duration
	SessionScoped   bool
	Once            bool
	Run             func(context.Context, State) (*Result, error)
	Name            string
	Point           HookPoint
	On              func(State) *Result
	DescribeTrigger func(State) string
}

type State interface {
	Facts() Facts
	Int(name string) int64
	SetInt(name string, value int64)
	String(name string) string
	SetString(name, value string)
}
