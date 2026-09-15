// Package agentprofile loads declarative sub-agent profiles without depending
// on the execution engine. The extension manager owns their lifetime.
package agentprofile

import "slices"

const MaxSteps = 50

// Profile is the resolved input to a run. Tools is an exact ceiling; Skills
// supplies workflow only. Zero MaxSteps inherits the engine default.
type Profile struct {
	Name         string
	Description  string
	SystemPrompt string
	Tools        []string
	Skills       []string
	MaxSteps     int
}

func Builtins() []Profile {
	return []Profile{
		{Name: "coder", Description: "Workspace implementation and command execution", Tools: []string{"read", "write", "edit", "shell", "process", "grep", "glob", "list", "web_search", "web_fetch", "web_extract"}},
		{Name: "explore", Description: "Read-only exploration; no shell or workspace writes", Tools: []string{"read", "grep", "glob", "list", "web_search", "web_fetch", "web_extract"}},
	}
}

func Clone(p Profile) Profile {
	p.Tools = slices.Clone(p.Tools)
	p.Skills = slices.Clone(p.Skills)
	return p
}
