// Package core defines the protocol shared by tool implementations,
// executors, and agent runtimes.
package core

import (
	"context"
	"nekocode/protocol"
)

// ExecutionMode controls whether a tool can run concurrently.
type ExecutionMode int

const (
	ModeParallel ExecutionMode = iota
	ModeSequential
)

type ToolCallItem struct {
	ID   string
	Name string // model-facing registered tool name
	Args map[string]any

	// EffectiveName/EffectiveArgs identify a delegated target for governance,
	// audit, permission, and UI while Name/Args remain the executable proxy
	// call emitted by the model.
	EffectiveName string
	EffectiveArgs map[string]any
}

// Effective returns the governance identity of the call.
func (c ToolCallItem) Effective() (string, map[string]any) {
	if c.EffectiveName != "" {
		return c.EffectiveName, c.EffectiveArgs
	}
	return c.Name, c.Args
}

type ToolCallResult struct {
	Artifact protocol.ToolOutputArtifact
	ID       string
	Name     string
	Output   string
	Error    string
}

// EffectiveOutput returns Error if non-empty, otherwise Output.
func (r ToolCallResult) TextOutput() ToolOutput {
	text := r.Output
	if r.Error != "" {
		text = r.Error
	}
	return ToolOutput{Text: text, Artifact: r.Artifact}
}

func (r ToolCallResult) EffectiveOutput() string { return r.TextOutput().Render() }

// Tool is the interface all tools implement.
type Tool interface {
	Name() string
	Description() string
	Parameters() []Parameter
	ExecutionMode(args map[string]any) ExecutionMode
	Execute(ctx context.Context, args map[string]any) (ToolOutput, error)
}

type Parameter struct {
	Name               string
	Type               string
	Required           bool
	Description        string
	Enum               []string
	Items              *Schema
	Properties         map[string]Schema
	RequiredProperties []string
}

// Schema describes nested tool input using the JSON Schema subset supported
// by both built-in providers.
type Schema struct {
	Type        string
	Description string
	Enum        []string
	Items       *Schema
	Properties  map[string]Schema
	Required    []string
}

type Descriptor struct {
	Name        string
	Description string
	Parameters  []Parameter
}

// PermissionRequest describes a capability a tool needs beyond its default
// sandbox. The executor may satisfy it from a persisted grant or ask the user.
type PermissionRequest struct {
	Reason       string
	Capabilities []string
	Scope        string
	Details      map[string]any
}

const (
	// Sandbox-granularity capabilities — when authorized, the command still
	// runs inside the sandbox; only the corresponding restriction is relaxed.
	CapNetOutbound = "net.outbound"  // share host network namespace for outbound/local network
	CapFsWritePath = "fs.write.path" // bind writable_roots argument dirs as read-write

	// CapProcessHost: run entirely on the host without any sandbox isolation.
	// Special: this capability is NEVER persisted — every invocation prompts
	// the user regardless of prior grants. It is the escape hatch of last
	// resort (needs TTY, Docker socket, writes to /etc, ...), not a routine
	// escalation target.
	CapProcessHost = "process.host"
)

type RequiredPermissionError struct {
	Request PermissionRequest
}

func (e RequiredPermissionError) Error() string {
	if e.Request.Reason == "" {
		return "permission required"
	}
	return "permission required: " + e.Request.Reason
}

func (e RequiredPermissionError) PermissionRequest() PermissionRequest {
	return e.Request
}

// PermissionError is returned by tools when sandbox execution cannot proceed
// without an explicit grant.
type PermissionError interface {
	error
	PermissionRequest() PermissionRequest
}
