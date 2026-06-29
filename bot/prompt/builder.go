package prompt

import (
	"nekocode/bot/prompt/planmode"
	systemprompt "nekocode/bot/prompt/system"
)

type Builder = systemprompt.Builder

func NewBuilder(cwd string) *Builder {
	return systemprompt.NewBuilder(cwd)
}

func PlanModePrompt(task string) string {
	return planmode.Prompt(task)
}
