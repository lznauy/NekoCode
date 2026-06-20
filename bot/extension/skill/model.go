// Package skill provides a file-based skill system.
package skill

// Skill represents a loaded skill definition.
type Skill struct {
	Name        string
	Description string
	WhenToUse   string
	Content     string
	Dir         string
	Files       []string

	Context                string
	AgentType              string
	AllowedTools           []string
	MaxSteps               int
	ContextWindow          int
	DisableModelInvocation bool
}
