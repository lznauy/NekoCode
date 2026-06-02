package subagent

import _ "embed"

//go:embed prompts/executor.md
var executorPrompt string

//go:embed prompts/verify.md
var verifyPrompt string

//go:embed prompts/researcher.md
var researcherPrompt string

func init() {
	register(AgentType{
		Name: "executor", SystemPrompt: executorPrompt,
		Tools: []string{"read", "write", "edit", "bash", "grep", "glob", "list"},
	})
	register(AgentType{
		Name: "verify", SystemPrompt: verifyPrompt,
		Tools: []string{"read", "grep", "glob", "list", "bash"},
	})
	register(AgentType{
		Name: "researcher", SystemPrompt: researcherPrompt,
		Tools: []string{"read", "grep", "glob", "list", "web_search", "web_fetch"},
		OmitProjectContext: true,
	})
}
