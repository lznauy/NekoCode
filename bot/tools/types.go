// types.go — shared types for the tools package.
package tools

import (
	"context"

	"nekocode/common"
	"nekocode/llm/types"
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

func ToToolDefs(descs []Descriptor) []types.ToolDef {
	defs := make([]types.ToolDef, len(descs))
	for i, d := range descs {
		props := make(map[string]types.Property)
		var required []string
		for _, p := range d.Parameters {
			props[p.Name] = types.Property{Type: p.Type, Description: p.Description}
			if p.Required {
				required = append(required, p.Name)
			}
		}
		defs[i] = types.ToolDef{
			Type: "function",
			Function: types.FunctionDef{
				Name: d.Name, Description: d.Description,
				Parameters: types.Parameters{Type: "object", Properties: props, Required: required},
			},
		}
	}
	return defs
}
