// types.go — shared types for the tools package.
package tools

import (
	"context"

	"nekocode/common"
)

// ExecutionMode controls whether a tool can run concurrently.
type ExecutionMode int

const (
	ModeParallel ExecutionMode = iota
	ModeSequential
)

type ToolCallItem struct {
	ID   string
	Name string
	Args map[string]any
}

type ToolCallResult struct {
	ID     string
	Name   string
	Output string
	Error  string
}

// Tool is the interface all tools implement.
type Tool interface {
	Name() string
	Description() string
	Parameters() []Parameter
	ExecutionMode(args map[string]any) ExecutionMode
	DangerLevel(args map[string]any) common.DangerLevel
	Execute(ctx context.Context, args map[string]any) (string, error)
}

type Parameter struct {
	Name        string
	Type        string
	Required    bool
	Description string
}

type Descriptor struct {
	Name        string
	Description string
	Parameters  []Parameter
}
