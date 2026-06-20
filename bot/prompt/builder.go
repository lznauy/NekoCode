package prompt

import (
	"nekocode/bot/prompt/planmode"
	systemprompt "nekocode/bot/prompt/system"
)

type Builder = systemprompt.Builder

func NewBuilder(cwd string) *Builder {
	return systemprompt.NewBuilder(cwd)
}

// AnalysisRules returns the analysis/review code-review rules. These should be
// injected contextually when the task is code review, bug hunting, or analysis,
// not forced into every turn's system prompt.
func AnalysisRules() string {
	return systemprompt.AnalysisRules()
}

func PlanModePrompt(task string) string {
	return planmode.Prompt(task)
}
