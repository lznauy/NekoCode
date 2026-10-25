package contract

import "slices"

// PointSpec defines a canonical event and the capabilities shared by adapters.
// Legacy post-tool results are handled separately by Normalize.
type PointSpec struct {
	Point         Point
	Name          string
	CanReject     bool
	CanStop       bool
	DefaultGuard  bool
	ToolMatcher   bool
	SessionScoped bool
	Once          bool
}

// Declaration order is also plugin registration order. Keep it stable.
var pointSpecs = []PointSpec{
	{Point: SessionStart, Name: "SessionStart", SessionScoped: true, Once: true},
	{Point: SessionEnd, Name: "SessionEnd", SessionScoped: true},
	{Point: RunStart, Name: "RunStart"},
	{Point: RunEnd, Name: "RunEnd"},
	{Point: UserSubmit, Name: "UserPromptSubmit", CanReject: true, CanStop: true},
	{Point: PreModel, Name: "PreModel", CanReject: true, CanStop: true},
	{Point: PreToolUse, Name: "PreToolUse", CanReject: true, CanStop: true, DefaultGuard: true, ToolMatcher: true},
	{Point: PostToolUse, Name: "PostToolUse", CanStop: true, ToolMatcher: true},
	{Point: PostToolBatch, Name: "PostToolBatch", CanStop: true},
	{Point: Stop, Name: "Stop", CanReject: true, CanStop: true, DefaultGuard: true},
	{Point: PreCompact, Name: "PreCompact"},
	{Point: PostCompact, Name: "PostCompact"},
	{Point: SubagentStart, Name: "SubagentStart"},
	{Point: SubagentStop, Name: "SubagentStop"},
}

// Points returns a detached list of canonical events, excluding filtered aliases.
func Points() []PointSpec { return slices.Clone(pointSpecs) }

func (p Point) Spec() (PointSpec, bool) {
	for _, spec := range pointSpecs {
		if spec.Point == p {
			return spec, true
		}
	}
	return PointSpec{}, false
}

func KnownPoint(p Point) bool { _, ok := p.Spec(); return ok }
