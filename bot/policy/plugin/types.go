package plugin

import (
	"nekocode/bot/policy/contract"
	"time"
)

type Point = contract.Point

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

type Event = contract.Event
type Hint = contract.Hint
type Result = contract.Result
type StatePatchResult = contract.StatePatch

type Hook struct {
	Name          string
	Point         Point
	Once          bool
	SessionScoped bool
	Guard         bool
	Timeout       time.Duration
	On            func(Event) *Result
}
