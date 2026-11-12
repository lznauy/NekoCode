// Package agentprofile loads declarative sub-agent profiles without depending
// on the execution engine. The extension manager owns their lifetime.
package agentprofile

import (
	"nekocode/mode"
	"slices"
)

const (
	DefaultMaxSteps = 150
	MaxSteps        = 200
)

// Profile is the resolved input to a run. Tools is an exact ceiling; Skills
// supplies workflow only. Zero MaxSteps inherits the engine default.
type Profile struct {
	RequiredRole mode.Role
	Name         string
	Description  string
	Model        string
	Isolation    string
	SystemPrompt string
	Tools        []string
	Skills       []string
	MaxSteps     int
}

func Builtins() []Profile {
	return []Profile{
		{Name: "coder", Description: "Workspace implementation and command execution", Tools: []string{"read", "write", "edit", "shell", "process", "grep", "glob", "list", "tree", "diff", "web_search", "web_fetch", "web_extract", "context_refs", "evidence", "index", "lsp_definition", "lsp_references", "lsp_hover", "lsp_diagnostics"}},
		{Name: "explore", Description: "Read-only exploration; no shell or workspace writes", Tools: []string{"read", "grep", "glob", "list", "tree", "diff", "web_search", "web_fetch", "web_extract", "context_refs", "evidence", "index", "lsp_definition", "lsp_references", "lsp_hover", "lsp_diagnostics"}},
	}
}

func Clone(p Profile) Profile {
	p.Tools = slices.Clone(p.Tools)
	p.Skills = slices.Clone(p.Skills)
	return p
}
