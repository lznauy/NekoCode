package subagent

import (
	_ "embed"
)

//go:embed prompts/shared_rules.md
var sharedRules string

//go:embed prompts/output_format.md
var outputFormat string

//go:embed prompts/executor.md
var executorPrompt string

//go:embed prompts/verify.md
var verifyPrompt string

//go:embed prompts/explore.md
var explorePrompt string

//go:embed prompts/plan.md
var planPrompt string

//go:embed prompts/decompose.md
var decomposePrompt string

func init() {
	register(AgentType{
		Name:         "executor",
		SystemPrompt: executorPrompt + "\n" + outputFormat + "\n" + sharedRules,
		Tools:        []string{"read", "write", "edit", "bash", "grep", "glob", "list"},
	})

	register(AgentType{
		Name:         "verify",
		SystemPrompt: verifyPrompt + "\n" + outputFormat + "\n" + sharedRules,
		Tools:        []string{"read", "grep", "glob", "list", "bash"},
	})

	register(AgentType{
		Name:               "explore",
		SystemPrompt:       explorePrompt + "\n" + outputFormat + "\n" + sharedRules,
		Tools:              []string{"read", "grep", "glob", "list", "web_search", "web_fetch"},
		OmitProjectContext: true,
		ReadOnly:           true,
	})

	register(AgentType{
		Name:               "plan",
		SystemPrompt:       planPrompt + "\n" + outputFormat + "\n" + sharedRules,
		Tools:              []string{"read", "grep", "glob", "list", "web_search", "web_fetch"},
		OmitProjectContext: true,
		ReadOnly:           true,
	})

	register(AgentType{
		Name:               "decompose",
		SystemPrompt:       decomposePrompt + "\n" + outputFormat + "\n" + sharedRules,
		Tools:              []string{"read", "grep", "glob", "list"},
		OmitProjectContext: true,
		ReadOnly:           true,
	})
}
