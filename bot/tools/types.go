package tools

import (
	"nekocode/bot/tools/core"
	"nekocode/llm/types"
)

type ExecutionMode = core.ExecutionMode
type ToolCallItem = core.ToolCallItem
type ToolCallResult = core.ToolCallResult
type Tool = core.Tool
type Parameter = core.Parameter
type Descriptor = core.Descriptor

const (
	ModeParallel   = core.ModeParallel
	ModeSequential = core.ModeSequential
)

func ToToolDefs(descs []Descriptor) []types.ToolDef {
	return core.ToToolDefs(descs)
}

func FormatArgs(args map[string]any) string {
	return core.FormatArgs(args)
}
