package policy

import "nekocode/bot/policy/contract"

// ToolRequest describes a tool before execution. TargetExists is supplied by
// the tool runtime because path resolution belongs to the tool layer.
type ToolRequest struct {
	Name         string
	Args         map[string]any
	TargetExists bool
}

// ToolResult is the single event recorded after a tool call.
type ToolResult = contract.ToolResult

type FinalIntent string

const (
	FinalIntentFinal       FinalIntent = "final"
	FinalIntentError       FinalIntent = "error"
	FinalIntentFormatError FinalIntent = "format_error"
	FinalIntentNonFinal    FinalIntent = "non_final"
)

type TurnResult struct {
	Intent  FinalIntent
	Garbled bool
	Text    string
}

// Facts is the immutable view evaluated by hooks.
type Facts struct {
	ActorID  string
	Batch    []ToolResult
	Tool     ToolFacts
	Response ResponseFacts
}

type ToolFacts struct {
	Name          string
	Args          map[string]any
	Error         bool
	Output        string
	ErrorMessage  string
	TargetPath    string
	TargetExists  bool
	TargetWasRead bool
}

type ResponseFacts struct {
	Text         string
	Intent       FinalIntent
	GarbledCount int
}
